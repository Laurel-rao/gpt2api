package ecommerce

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/432539/gpt2api/internal/channel"
	imgpkg "github.com/432539/gpt2api/internal/image"
	modelpkg "github.com/432539/gpt2api/internal/model"
	"github.com/432539/gpt2api/internal/scheduler"
	"github.com/432539/gpt2api/internal/upstream/adapter"
	"github.com/432539/gpt2api/internal/upstream/chatgpt"
	"github.com/432539/gpt2api/pkg/logger"
)

const (
	maxReferenceImages     = 4
	maxReferenceImageBytes = 20 * 1024 * 1024
)

type AccountSecretResolver interface {
	DecryptCookies(ctx context.Context, accountID uint64) (string, error)
}

type ImageAccountResolver interface {
	AuthToken(ctx context.Context, accountID uint64) (at, deviceID, cookies string, err error)
	ProxyURL(ctx context.Context, accountID uint64) string
}

type Runner struct {
	dao              *DAO
	models           *modelpkg.Registry
	scheduler        *scheduler.Scheduler
	acc              AccountSecretResolver
	imageAcc         ImageAccountResolver
	channels         *channel.Router
	imageDAO         *imgpkg.DAO
	imageRun         *imgpkg.Runner
	imageConcurrency int
	imageSem         chan struct{}
	activeMu         sync.Mutex
	activeCancels    map[string]context.CancelFunc
}

func NewRunner(dao *DAO, models *modelpkg.Registry, sched *scheduler.Scheduler, acc AccountSecretResolver, channels *channel.Router, imageDAO *imgpkg.DAO, imageRun *imgpkg.Runner, imageConcurrency int) *Runner {
	if imageConcurrency <= 0 {
		imageConcurrency = 1
	}
	return &Runner{
		dao:              dao,
		models:           models,
		scheduler:        sched,
		acc:              acc,
		channels:         channels,
		imageDAO:         imageDAO,
		imageRun:         imageRun,
		imageConcurrency: imageConcurrency,
		imageSem:         make(chan struct{}, imageConcurrency),
		activeCancels:    map[string]context.CancelFunc{},
	}
}

func (r *Runner) SetImageAccountResolver(resolver ImageAccountResolver) {
	r.imageAcc = resolver
}

func (r *Runner) Enqueue(taskID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		ctx, cancel = r.trackTaskContext(taskID, ctx, cancel)
		defer r.untrackTaskContext(taskID, cancel)
		if err := r.Run(ctx, taskID); err != nil {
			if r.isCanceled(context.Background(), taskID) {
				return
			}
			logger.L().Warn("ecommerce task failed", zap.String("task_id", taskID), zap.Error(err))
			_ = r.dao.MarkTaskFailed(context.Background(), taskID, err.Error())
		}
	}()
}

func (r *Runner) EnqueueAssetRetry(taskID string, assetID uint64, extraPrompt string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := r.RetryAsset(ctx, taskID, assetID, extraPrompt); err != nil {
			logger.L().Warn("ecommerce asset retry failed",
				zap.String("task_id", taskID), zap.Uint64("asset_id", assetID), zap.Error(err))
		}
	}()
}

func (r *Runner) CancelTask(ctx context.Context, taskID string) error {
	task, err := r.dao.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if task.Status != StatusQueued && task.Status != StatusRunning {
		return errors.New("任务已结束，不能中断")
	}
	if err := r.dao.MarkTaskCanceled(ctx, taskID); err != nil {
		return err
	}
	_ = r.dao.MarkTaskAssetsCanceled(ctx, taskID)
	r.activeMu.Lock()
	cancel := r.activeCancels[taskID]
	r.activeMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (r *Runner) trackTaskContext(taskID string, parent context.Context, parentCancel context.CancelFunc) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	r.activeMu.Lock()
	r.activeCancels[taskID] = func() {
		cancel()
		parentCancel()
	}
	r.activeMu.Unlock()
	return ctx, func() {
		cancel()
		parentCancel()
	}
}

func (r *Runner) untrackTaskContext(taskID string, cancel context.CancelFunc) {
	cancel()
	r.activeMu.Lock()
	delete(r.activeCancels, taskID)
	r.activeMu.Unlock()
}

func (r *Runner) isCanceled(ctx context.Context, taskID string) bool {
	task, err := r.dao.GetTask(ctx, taskID)
	return err == nil && task.Status == StatusCanceled
}

func (r *Runner) Run(ctx context.Context, taskID string) error {
	task, err := r.dao.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if err := r.dao.MarkTaskRunning(ctx, taskID); err != nil {
		return err
	}
	platform, err := r.dao.GetPlatform(ctx, task.PlatformID)
	if err != nil {
		return err
	}
	prompt, err := r.dao.GetPromptTemplate(ctx, task.PromptTemplateID)
	if err != nil {
		return err
	}
	style, err := r.dao.GetStyleTemplate(ctx, task.StyleTemplateID)
	if err != nil {
		return err
	}

	rawText, err := r.generateText(ctx, *platform, *prompt, *style, task.Requirement)
	var out Output
	if err != nil {
		if !canFallbackText(err) {
			return err
		}
		logger.L().Warn("ecommerce text upstream failed, using local draft",
			zap.String("task_id", taskID), zap.Error(err))
		out = localDraftOutput(*platform, *style, task.Requirement)
	} else {
		out = parseOutput(rawText, task.Requirement)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	normalizeOutput(&out, task.Requirement)
	outBytes, _ := json.Marshal(out)
	_ = r.dao.UpdateTaskDraft(ctx, taskID, 35, outBytes, buildHTML(out, nil))

	refs, err := decodeReferenceInputs(ctx, task.ReferenceImages.RawMessage())
	if err != nil {
		return err
	}
	imageModel, err := r.firstModel(ctx, modelpkg.TypeImage)
	if err != nil {
		return err
	}
	type assetJob struct {
		id        uint64
		assetTyp  string
		imgTaskID string
		prompt    string
		spec      ImageSpec
	}
	jobs := make([]assetJob, 0, len(assetTypes))
	var whiteJob assetJob
	for _, assetType := range assetTypes {
		if err := ctx.Err(); err != nil {
			return err
		}
		assetPrompt := r.buildImagePrompt(*platform, *prompt, *style, out, task.Requirement, assetType)
		spec := out.ImageSpecs[assetType]
		imgTaskID := imgpkg.GenerateTaskID()
		asset := &Asset{TaskID: taskID, AssetType: assetType, ImageTaskID: imgTaskID, Prompt: assetPrompt, Status: StatusQueued}
		if err := r.dao.CreateAsset(ctx, asset); err != nil {
			return err
		}
		if r.imageDAO != nil {
			_ = r.imageDAO.Create(ctx, &imgpkg.Task{
				TaskID:  imgTaskID,
				UserID:  task.UserID,
				ModelID: imageModel.ID,
				Prompt:  assetPrompt,
				N:       1,
				Size:    spec.Size,
				Status:  imgpkg.StatusDispatched,
			})
		}
		job := assetJob{id: asset.ID, assetTyp: assetType, imgTaskID: imgTaskID, prompt: assetPrompt, spec: spec}
		jobs = append(jobs, job)
		if assetType == AssetWhite {
			whiteJob = job
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var completed int
	assetErrors := make([]string, 0)
	runAsset := func(job assetJob, refImages []imgpkg.ReferenceImage) *imgpkg.RunResult {
		release, err := r.acquireImageSlot(ctx)
		if err != nil {
			errCode := err.Error()
			_ = r.dao.UpdateAssetResult(context.Background(), job.id, StatusFailed, job.imgTaskID, "", "", errCode)
			mu.Lock()
			assetErrors = append(assetErrors, fmt.Sprintf("%s:%s", job.assetTyp, errCode))
			completed++
			progress := 35 + completed*10
			mu.Unlock()
			_ = r.dao.UpdateTaskProgress(context.Background(), taskID, progress)
			return &imgpkg.RunResult{Status: imgpkg.StatusFailed, ErrorCode: errCode}
		}
		defer release()
		_ = r.dao.UpdateAssetResult(context.Background(), job.id, StatusRunning, job.imgTaskID, "", "", "")
		res := r.imageRun.Run(ctx, imgpkg.RunOptions{
			TaskID:           job.imgTaskID,
			UserID:           task.UserID,
			ModelID:          imageModel.ID,
			UpstreamModel:    imageModel.UpstreamModelSlug,
			Prompt:           job.prompt,
			N:                1,
			Size:             job.spec.Size,
			MaxAttempts:      1,
			References:       refImages,
			ReturnImageBytes: job.assetTyp == AssetWhite,
		})
		if res.Status != imgpkg.StatusSuccess {
			errCode := res.ErrorCode
			if errCode == "" {
				errCode = res.ErrorMessage
			}
			if errCode == "" {
				errCode = "unknown"
			}
			_ = r.dao.UpdateAssetResult(context.Background(), job.id, StatusFailed, job.imgTaskID, "", "", errCode)
			mu.Lock()
			assetErrors = append(assetErrors, fmt.Sprintf("%s:%s", job.assetTyp, errCode))
			completed++
			progress := 35 + completed*10
			mu.Unlock()
			_ = r.dao.UpdateTaskProgress(context.Background(), taskID, progress)
			return res
		}
		url := ""
		fileID := ""
		if len(res.SignedURLs) > 0 {
			url = imgpkg.BuildProxyURL(job.imgTaskID, 0, 24*time.Hour)
		}
		if len(res.FileIDs) > 0 {
			fileID = strings.TrimPrefix(res.FileIDs[0], "sed:")
		}
		_ = r.dao.UpdateAssetResult(context.Background(), job.id, StatusSuccess, job.imgTaskID, url, fileID, "")
		mu.Lock()
		completed++
		progress := 35 + completed*10
		mu.Unlock()
		_ = r.dao.UpdateTaskProgress(context.Background(), taskID, progress)
		return res
	}

	whiteRes := runAsset(whiteJob, refs)
	whiteRefs, whiteRefErr := whiteReferenceFromResult(ctx, whiteRes)
	if whiteRes.Status != imgpkg.StatusSuccess || whiteRefErr != nil {
		errCode := "white_reference_failed"
		if whiteRefErr != nil {
			errCode = whiteRefErr.Error()
		}
		if whiteRes.Status == imgpkg.StatusSuccess {
			assetErrors = append(assetErrors, "white_reference:"+errCode)
		}
		for _, job := range jobs {
			if job.assetTyp == AssetWhite {
				continue
			}
			_ = r.dao.UpdateAssetResult(context.Background(), job.id, StatusFailed, job.imgTaskID, "", "", errCode)
			completed++
			_ = r.dao.UpdateTaskProgress(context.Background(), taskID, 35+completed*10)
		}
	} else {
		for _, job := range jobs {
			if job.assetTyp == AssetWhite {
				continue
			}
			wg.Add(1)
			go func(job assetJob) {
				defer wg.Done()
				_ = runAsset(job, whiteRefs)
			}(job)
		}
	}
	wg.Wait()
	dbCtx := context.Background()
	html, err := r.rebuildTaskHTML(dbCtx, taskID, out)
	if err != nil {
		return err
	}
	if len(assetErrors) > 0 {
		return r.dao.MarkTaskFailedWithOutput(dbCtx, taskID, "图片生成失败: "+strings.Join(assetErrors, "; "), outBytes, html)
	}
	if err := r.dao.MarkTaskSuccess(dbCtx, taskID, outBytes, html); err != nil {
		return err
	}
	return nil
}

func (r *Runner) RetryAsset(ctx context.Context, taskID string, assetID uint64, extraPrompt string) error {
	task, err := r.dao.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	asset, err := r.dao.GetAsset(ctx, assetID)
	if err != nil {
		return err
	}
	if asset.TaskID != taskID {
		return ErrNotFound
	}
	if asset.Status == StatusRunning || asset.Status == StatusQueued {
		return errors.New("图片正在生成中")
	}
	platform, err := r.dao.GetPlatform(ctx, task.PlatformID)
	if err != nil {
		return err
	}
	prompt, err := r.dao.GetPromptTemplate(ctx, task.PromptTemplateID)
	if err != nil {
		return err
	}
	style, err := r.dao.GetStyleTemplate(ctx, task.StyleTemplateID)
	if err != nil {
		return err
	}
	out := outputFromTask(task)
	if out.ProductTitle == "" {
		out = localDraftOutput(*platform, *style, task.Requirement)
	}
	normalizeOutput(&out, task.Requirement)
	imageModel, err := r.firstModel(ctx, modelpkg.TypeImage)
	if err != nil {
		return err
	}
	refs, err := decodeReferenceInputs(ctx, task.ReferenceImages.RawMessage())
	if err != nil {
		return err
	}
	assetPrompt := r.buildRetryImagePrompt(*platform, *prompt, *style, out, task.Requirement, asset.AssetType, extraPrompt)
	spec := out.ImageSpecs[asset.AssetType]
	imgTaskID := imgpkg.GenerateTaskID()
	if err := r.dao.MarkTaskRetrying(ctx, taskID); err != nil {
		return err
	}
	if r.imageDAO != nil {
		_ = r.imageDAO.Create(ctx, &imgpkg.Task{
			TaskID:  imgTaskID,
			UserID:  task.UserID,
			ModelID: imageModel.ID,
			Prompt:  assetPrompt,
			N:       1,
			Size:    spec.Size,
			Status:  imgpkg.StatusDispatched,
		})
	}
	release, err := r.acquireImageSlot(ctx)
	if err != nil {
		errCode := err.Error()
		_ = r.dao.UpdateAssetResult(context.Background(), assetID, StatusFailed, imgTaskID, "", "", errCode)
		return r.finalizeTaskAfterRetry(context.Background(), taskID, out, "图片重试失败: "+asset.AssetType+":"+errCode)
	}
	defer release()
	if err := r.dao.MarkAssetRetrying(ctx, assetID, imgTaskID, assetPrompt); err != nil {
		return err
	}
	refImages := r.retryReferences(ctx, taskID, *asset, refs)
	res := r.imageRun.Run(ctx, imgpkg.RunOptions{
		TaskID:        imgTaskID,
		UserID:        task.UserID,
		ModelID:       imageModel.ID,
		UpstreamModel: imageModel.UpstreamModelSlug,
		Prompt:        assetPrompt,
		N:             1,
		Size:          spec.Size,
		MaxAttempts:   1,
		References:    refImages,
	})
	if res.Status != imgpkg.StatusSuccess {
		errCode := imageErrorCode(res)
		_ = r.dao.UpdateAssetResult(context.Background(), assetID, StatusFailed, imgTaskID, "", "", errCode)
		return r.finalizeTaskAfterRetry(context.Background(), taskID, out, "图片重试失败: "+asset.AssetType+":"+errCode)
	}
	url := ""
	fileID := ""
	if len(res.SignedURLs) > 0 {
		url = imgpkg.BuildProxyURL(imgTaskID, 0, 24*time.Hour)
	}
	if len(res.FileIDs) > 0 {
		fileID = strings.TrimPrefix(res.FileIDs[0], "sed:")
	}
	if err := r.dao.UpdateAssetResult(context.Background(), assetID, StatusSuccess, imgTaskID, url, fileID, ""); err != nil {
		return err
	}
	return r.finalizeTaskAfterRetry(context.Background(), taskID, out, "")
}

func (r *Runner) acquireImageSlot(ctx context.Context) (func(), error) {
	if r.imageSem == nil {
		return func() {}, nil
	}
	select {
	case r.imageSem <- struct{}{}:
		return func() { <-r.imageSem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func outputFromTask(task *TaskRow) Output {
	var out Output
	if len(task.OutputJSON) > 0 {
		_ = json.Unmarshal(task.OutputJSON.RawMessage(), &out)
	}
	return out
}

func imageErrorCode(res *imgpkg.RunResult) string {
	if res.ErrorCode != "" {
		return res.ErrorCode
	}
	if res.ErrorMessage != "" {
		return res.ErrorMessage
	}
	return "unknown"
}

func (r *Runner) rebuildTaskHTML(ctx context.Context, taskID string, out Output) (string, error) {
	assets, err := r.dao.ListAssets(ctx, taskID)
	if err != nil {
		return "", err
	}
	return buildHTML(out, assets), nil
}

func (r *Runner) finalizeTaskAfterRetry(ctx context.Context, taskID string, out Output, errMsg string) error {
	assets, err := r.dao.ListAssets(ctx, taskID)
	if err != nil {
		return err
	}
	running := false
	failed := make([]string, 0)
	for _, a := range assets {
		switch a.Status {
		case StatusQueued, StatusRunning:
			running = true
		case StatusFailed:
			if a.Error != "" {
				failed = append(failed, a.AssetType+":"+a.Error)
			} else {
				failed = append(failed, a.AssetType)
			}
		}
	}
	if running {
		return nil
	}
	outBytes, _ := json.Marshal(out)
	html := buildHTML(out, assets)
	if errMsg != "" {
		failed = append(failed, errMsg)
	}
	if len(failed) > 0 {
		return r.dao.MarkTaskFailedWithOutput(ctx, taskID, strings.Join(failed, "; "), outBytes, html)
	}
	return r.dao.MarkTaskSuccess(ctx, taskID, outBytes, html)
}

func (r *Runner) generateText(ctx context.Context, platform Platform, prompt PromptTemplate, style StyleTemplate, requirement string) (string, error) {
	chatModel, err := r.firstModel(ctx, modelpkg.TypeChat)
	if err != nil {
		return "", err
	}
	msgs := r.buildContentMessages(platform, prompt, style, requirement)
	if r.channels != nil {
		text, err := r.generateTextByChannel(ctx, chatModel, msgs)
		if err == nil {
			return text, nil
		}
		if !errors.Is(err, channel.ErrNoRoute) {
			return "", err
		}
	}
	lease, err := r.scheduler.Dispatch(ctx, modelpkg.TypeChat)
	if err != nil {
		return "", err
	}
	defer func() { _ = lease.Release(context.Background()) }()
	cookies, _ := r.acc.DecryptCookies(ctx, lease.Account.ID)
	cli, err := chatgpt.New(chatgpt.Options{
		AuthToken: lease.AuthToken,
		DeviceID:  lease.DeviceID,
		SessionID: lease.SessionID,
		ProxyURL:  lease.ProxyURL,
		Cookies:   cookies,
		Timeout:   90 * time.Second,
	})
	if err != nil {
		return "", err
	}
	bootCtx, cancelBoot := context.WithTimeout(ctx, 15*time.Second)
	_ = cli.Bootstrap(bootCtx)
	cancelBoot()
	reqCtx, cancelReq := context.WithTimeout(ctx, 30*time.Second)
	cr, err := cli.ChatRequirementsV2(reqCtx)
	cancelReq()
	if err != nil {
		return "", err
	}
	var proof string
	if cr.Proofofwork.Required {
		proof = cr.SolveProof("")
		if proof == "" {
			r.scheduler.MarkWarned(ctx, lease.Account.ID)
			return "", errors.New("上游 PoW 校验失败")
		}
	}
	upstream := chatModel.UpstreamModelSlug
	if upstream == "" {
		upstream = "auto"
	}
	if cr.IsFreeAccount() {
		upstream = "auto"
	}
	opt := chatgpt.FChatOpts{UpstreamModel: upstream, Messages: msgs, ChatToken: cr.Token, ProofToken: proof}
	prepCtx, cancelPrep := context.WithTimeout(ctx, 30*time.Second)
	conduit, _ := cli.PrepareFChat(prepCtx, opt)
	cancelPrep()
	opt.ConduitToken = conduit
	stream, err := cli.StreamFChat(ctx, opt)
	if err != nil {
		return "", err
	}
	var ex deltaExtractor
	var b strings.Builder
	for ev := range stream {
		if ev.Err != nil {
			return "", ev.Err
		}
		delta, final := ex.Extract(ev.Data)
		b.WriteString(delta)
		if final {
			break
		}
	}
	if strings.TrimSpace(b.String()) == "" {
		return "", errors.New("上游未返回电商文案")
	}
	return b.String(), nil
}

func (r *Runner) generateTextByChannel(ctx context.Context, chatModel *modelpkg.Model, msgs []chatgpt.ChatMessage) (string, error) {
	routes, err := r.channels.Resolve(ctx, chatModel.Slug, channel.ModalityText)
	if err != nil {
		return "", err
	}
	req := &adapter.ChatRequest{
		Model:     chatModel.Slug,
		Messages:  msgs,
		Stream:    true,
		MaxTokens: 1200,
	}
	var lastErr error
	for _, rt := range routes {
		upstreamModel := strings.TrimSpace(rt.UpstreamModel)
		if upstreamModel == "" {
			upstreamModel = strings.TrimSpace(chatModel.UpstreamModelSlug)
		}
		if upstreamModel == "" {
			upstreamModel = chatModel.Slug
		}
		stream, err := rt.Adapter.Chat(ctx, upstreamModel, req)
		if err != nil {
			lastErr = err
			_ = r.channels.Svc().MarkHealth(context.Background(), rt.Channel, false, err.Error())
			logger.L().Warn("ecommerce channel chat failed",
				zap.Uint64("channel_id", rt.Channel.ID),
				zap.String("channel_name", rt.Channel.Name),
				zap.String("upstream_model", upstreamModel),
				zap.Error(err))
			continue
		}
		var b strings.Builder
		for ch := range stream {
			if ch.Err != nil {
				lastErr = ch.Err
				break
			}
			b.WriteString(ch.Delta)
		}
		if s := strings.TrimSpace(b.String()); s != "" {
			_ = r.channels.Svc().MarkHealth(context.Background(), rt.Channel, true, "")
			return s, nil
		}
		lastErr = errors.New("渠道未返回电商文案")
		_ = r.channels.Svc().MarkHealth(context.Background(), rt.Channel, false, lastErr.Error())
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", channel.ErrNoRoute
}

func (r *Runner) buildContentMessages(platform Platform, prompt PromptTemplate, style StyleTemplate, requirement string) []chatgpt.ChatMessage {
	langRule := platformLanguageRule(platformLanguageCode(platform))
	return []chatgpt.ChatMessage{
		{Role: "system", Content: "你是资深电商详情页策划。先解析统一商品信息和价格信息，再生成页面文案与图片文字计划。只输出 JSON，不要输出 Markdown。 " + langRule},
		{Role: "user", Content: r.buildContentPrompt(platform, prompt, style, requirement)},
	}
}

func canFallbackText(err error) bool {
	var ue *chatgpt.UpstreamError
	if errors.As(err, &ue) {
		return ue.Status == http.StatusUnauthorized || ue.Status == http.StatusForbidden
	}
	msg := err.Error()
	return strings.Contains(msg, "chat-requirements") || strings.Contains(msg, "上游未返回电商文案")
}

func localDraftOutput(platform Platform, style StyleTemplate, requirement string) Output {
	name := compactRequirement(requirement, 34)
	if name == "" {
		name = "精选商品"
	}
	if strings.HasPrefix(strings.ToLower(platformLanguageCode(platform)), "en") {
		if name == "精选商品" {
			name = "Selected Product"
		}
		desc := compactRequirement(requirement, 120)
		if desc == "" {
			desc = "An ecommerce detail-page draft focused on product benefits, use cases and purchase reasons."
		}
		return Output{
			ShopTitle:    name + " Store",
			ProductTitle: name,
			Description:  desc,
			PriceCopy:    "Limited-time offer, better value today",
			ProductInfo: ProductInfo{
				CanonicalTitle: name,
				ShortTitle:     name,
				CoreValue:      desc,
			},
			PriceInfo: PriceInfo{
				Currency:      "USD",
				PromotionText: "Limited-time offer",
				CTA:           "Shop Now",
			},
			MarketingCopy: []string{"Clear core benefits", "Ready for multiple ecommerce platforms", "Reduce purchase hesitation"},
			DetailSections: []DetailSection{
				{Title: "Key Benefits", Body: "Highlight the product advantages customers care about most."},
				{Title: "Use Cases", Body: "Connect the product with everyday use, gifting and repeat-purchase scenarios."},
				{Title: "Specifications", Body: "Organize size, material, function and audience details to reduce pre-sale questions."},
				{Title: "Why Buy Now", Body: "Use concise copy to connect price, quality and service assurance."},
			},
			PlatformFields: map[string]string{
				"platform": platform.Name,
				"title":    name,
				"style":    style.Name,
			},
			ImageSpecs: defaultImageSpecs(),
		}
	}
	desc := compactRequirement(requirement, 120)
	if desc == "" {
		desc = "围绕商品卖点、使用场景和购买理由生成的电商详情页草稿。"
	}
	return Output{
		ShopTitle:     name + "优选馆",
		ProductTitle:  name,
		Description:   desc,
		PriceCopy:     "限时优惠，到手价更划算",
		MarketingCopy: []string{"核心卖点清晰呈现", "适配多平台详情页", "减少用户决策成本"},
		DetailSections: []DetailSection{
			{Title: "核心卖点", Body: "提炼商品关键优势，突出用户最关心的价值点。"},
			{Title: "使用场景", Body: "结合日常使用、送礼和复购场景，增强购买代入感。"},
			{Title: "规格信息", Body: "整理尺寸、材质、功能、适用人群等信息，降低咨询成本。"},
			{Title: "购买理由", Body: "用简洁文案承接价格、品质和服务保障，推动下单。"},
		},
		PlatformFields: map[string]string{
			"platform": platform.Name,
			"title":    name,
			"style":    style.Name,
		},
		ImageSpecs: defaultImageSpecs(),
	}
}

func compactRequirement(s string, limit int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if limit <= 0 || len([]rune(s)) <= limit {
		return s
	}
	rs := []rune(s)
	return string(rs[:limit])
}

func (r *Runner) buildContentPrompt(platform Platform, prompt PromptTemplate, style StyleTemplate, requirement string) string {
	tpl := `商品需求：
{{.Requirement}}

目标平台：{{.Platform.Name}}
生成语言：{{.LanguageName}}（{{.LanguageCode}}）
语言要求：{{.LanguageRule}}
内容策略：{{.Prompt.ContentPrompt}}
视觉风格：{{.Style.StylePrompt}}

请生成可直接用于电商详情页的结构化内容，返回严格 JSON：
{
  "shop_title": "店铺标题",
  "product_title": "商品标题",
  "description": "商品描述",
  "price_copy": "价格/促销文案",
  "product_info": {
    "category": "商品品类",
    "canonical_title": "统一商品全称，所有图片必须一致",
    "short_title": "统一短标题，适合图片展示",
    "core_value": "一句话核心价值",
    "key_specs": ["规格/材质/型号/容量等事实信息"],
    "selling_points": ["统一卖点1", "统一卖点2", "统一卖点3"],
    "target_audience": "目标用户"
  },
  "price_info": {
    "currency": "CNY/USD 等币种",
    "sale_price": "用户明确提供的成交价，未提供则留空",
    "original_price": "用户明确提供的原价，未提供则留空",
    "price_text": "图片和页面统一展示的价格文字；未提供明确数字时不得编造数字价格",
    "promotion_text": "统一促销权益",
    "cta": "统一行动号召"
  },
  "marketing_copy": ["营销短句1", "营销短句2", "营销短句3"],
  "detail_sections": [{"title": "模块标题", "body": "模块正文"}],
  "platform_fields": {"title": "平台标题", "description": "平台描述"},
  "image_specs": {
    "title_image": {"size": "1792x1024", "aspect_ratio": "7:4", "clarity": "high"},
    "main_image": {"size": "1024x1024", "aspect_ratio": "1:1", "clarity": "high"},
    "white_image": {"size": "1024x1024", "aspect_ratio": "1:1", "clarity": "high"},
    "detail_image": {"size": "1024x1792", "aspect_ratio": "4:7", "clarity": "high"},
    "price_image": {"size": "1024x1792", "aspect_ratio": "4:7", "clarity": "high"}
  },
  "image_text_plans": {
    "title_image": {"title": "只使用统一标题", "subtitle": "只使用统一核心价值", "price_text": "只使用统一价格文字", "promotion_text": "只使用统一促销文字", "cta": "只使用统一行动号召", "badges": ["标签"], "selling_points": ["卖点"], "specs": ["规格"], "notes": ["图片文字约束"]},
    "main_image": {"title": "只使用统一标题", "subtitle": "只使用统一核心价值", "price_text": "只使用统一价格文字", "promotion_text": "只使用统一促销文字", "cta": "只使用统一行动号召", "badges": ["标签"], "selling_points": ["卖点"], "specs": ["规格"], "notes": ["图片文字约束"]},
    "white_image": {"title": "", "subtitle": "", "price_text": "", "promotion_text": "", "cta": "", "badges": [], "selling_points": [], "specs": [], "notes": ["白底图不放任何文字、价格、促销标签或图标"]},
    "detail_image": {"title": "只使用统一标题", "subtitle": "只使用统一核心价值", "price_text": "只使用统一价格文字", "promotion_text": "只使用统一促销文字", "cta": "只使用统一行动号召", "badges": ["标签"], "selling_points": ["卖点"], "specs": ["规格"], "notes": ["图片文字约束"]},
    "price_image": {"title": "只使用统一标题", "subtitle": "只使用统一核心价值", "price_text": "只使用统一价格文字", "promotion_text": "只使用统一促销文字", "cta": "只使用统一行动号召", "badges": ["标签"], "selling_points": ["卖点"], "specs": ["规格"], "notes": ["图片文字约束"]}
  }
}

一致性规则：
1. 先从商品需求中解析商品品类、标题、规格、卖点、价格、促销和行动号召，再生成详情页文案。
2. 用户提供了价格、折扣、型号、规格时，必须逐字保留；用户没有提供明确数字价格时，sale_price/original_price 留空，price_text 使用非数字促销文案。
3. 所有图片的 image_text_plans 必须复用同一份 product_info 和 price_info，不得为不同图片编造不同价格、标题、型号或规格。
4. 白底图 image_text_plans 必须为空文字，只保留无文字备注。
5. 商品标题和价格在 JSON 内只允许出现一个统一版本。
6. image_specs 只能使用 1024x1024、1792x1024、1024x1792 三种尺寸；店标题图优先横版 1792x1024，电商大图和白底图优先方图 1024x1024，详情图和价格图优先竖版 1024x1792；不得把所有图片都设置成同一尺寸。`
	return renderTemplate(tpl, newRenderData(requirement, platform, prompt, style, Output{}))
}

func (r *Runner) buildImagePrompt(platform Platform, prompt PromptTemplate, style StyleTemplate, out Output, requirement, assetType string) string {
	langCode := platformLanguageCode(platform)
	if strings.HasPrefix(strings.ToLower(langCode), "en") {
		return r.buildEnglishImagePrompt(platform, prompt, style, out, requirement, assetType, langCode)
	}
	assetText := map[string]string{
		AssetTitle:  "店标题图，突出商品名称和核心价值",
		AssetMain:   "电商主图，商品主体清晰，适合列表和首屏",
		AssetWhite:  "商品白底图，只展示商品本体",
		AssetDetail: "详情页长图模块，展示卖点、场景和参数",
		AssetPrice:  "价格促销图，突出优惠和行动号召",
	}[assetType]
	composition := assetCompositionCN(assetType)
	spec := out.ImageSpecs[assetType]
	if assetType == AssetWhite {
		src := `平台：{{.Platform.Name}}
图片类型：白底图
目标：干净的商品白底图，只展示商品本体，商品完整居中，边缘清晰，适合电商平台商品主图审核。
商品标题：{{.Output.ProductTitle}}
描述：{{.Output.Description}}
原始需求：{{.Requirement}}
统一商品信息：
{{.UnifiedInfo}}
图片参数：尺寸 ` + spec.Size + `，长宽比 ` + spec.AspectRatio + `，清晰度 ` + spec.Clarity + `
构图要求：` + composition + `
形态保真：必须严格保持原始需求和参考图中的商品品类、结构、可折叠/可收纳/便携等关键形态；如有参考图，以参考图商品外观、比例、颜色、部件和结构为最高优先级；不得改成同类普通款、传统款或不可收纳形态；例如可折叠/可收纳钢琴必须是便携折叠电子琴/键盘类商品，不得生成传统立式钢琴、三角钢琴、柜式电钢琴或固定琴架款。
严格要求：纯白或接近纯白背景；无标题、无卖点文字、无价格、无促销标签、无图标、无贴纸、无边框、无水印、无品牌标志、无场景道具、无人物手部；商品真实、清晰、完整、单独呈现。`
		return renderTemplate(src, renderData{
			Requirement:  requirement,
			Platform:     platform,
			Output:       out,
			UnifiedInfo:  formatUnifiedInfoForLanguage(out, langCode),
			LanguageCode: langCode,
			LanguageName: platformLanguageName(langCode),
			LanguageRule: platformLanguageRule(langCode),
		})
	}
	src := `以下内容仅作为视觉方向，不要把模板原文直接做成画面文字：
{{.Prompt.ImagePrompt}}
{{.Style.StylePrompt}}
平台：{{.Platform.Name}}
图片类型：{{.AssetType}}
目标：` + assetText + `
原始需求：{{.Requirement}}
统一商品信息：
{{.CompactUnifiedInfo}}
本图允许出现的文字：
{{.CompactImageTextPlan}}
图片参数：尺寸 ` + spec.Size + `，长宽比 ` + spec.AspectRatio + `，清晰度 ` + spec.Clarity + `
构图要求：` + composition + `
硬约束：
- 必须保持参考图中的商品主体一致。
- 不得照搬参考图或白底图的纯白背景、角度、裁切和光影。
- 当前资产必须与白底图明显不同。
- 非白底图不得退化成白底商品图。
- 原始需求中关于场景、人物、代言氛围、品牌氛围、活动主题的要求优先级更高。
- 不得新增、替换、改写任何数字价格、折扣、标题、规格、型号。`
	return renderTemplate(src, renderData{
		Requirement:          requirement,
		Platform:             platform,
		Prompt:               prompt,
		Style:                style,
		Output:               out,
		AssetType:            assetType,
		UnifiedInfo:          formatUnifiedInfoForLanguage(out, langCode),
		ImageTextPlan:        formatImageTextPlanForLanguage(out.ImageTextPlans[assetType], langCode),
		CompactUnifiedInfo:   formatCompactUnifiedInfoForLanguage(out, langCode),
		CompactImageTextPlan: formatCompactImageTextPlanForLanguage(out.ImageTextPlans[assetType], assetType, langCode),
		LanguageCode:         langCode,
		LanguageName:         platformLanguageName(langCode),
		LanguageRule:         platformLanguageRule(langCode),
	})
}

func (r *Runner) buildEnglishImagePrompt(platform Platform, prompt PromptTemplate, style StyleTemplate, out Output, requirement, assetType, langCode string) string {
	assetText := map[string]string{
		AssetTitle:  "store title image that highlights the product name and core value",
		AssetMain:   "main ecommerce image with a clear product hero, suitable for listings and above-the-fold display",
		AssetWhite:  "white-background product image showing only the product itself",
		AssetDetail: "detail-page module image showing selling points, scenes and specs",
		AssetPrice:  "price promotion image highlighting the offer and call to action",
	}[assetType]
	composition := assetCompositionEN(assetType)
	spec := out.ImageSpecs[assetType]
	data := newRenderData(requirement, platform, prompt, style, out)
	data.AssetType = assetType
	data.UnifiedInfo = formatUnifiedInfoForLanguage(out, langCode)
	data.ImageTextPlan = formatImageTextPlanForLanguage(out.ImageTextPlans[assetType], langCode)
	data.CompactUnifiedInfo = formatCompactUnifiedInfoForLanguage(out, langCode)
	data.CompactImageTextPlan = formatCompactImageTextPlanForLanguage(out.ImageTextPlans[assetType], assetType, langCode)
	if assetType == AssetWhite {
		src := `Platform: {{.Platform.Name}}
Image type: white-background image
Goal: a clean product-only white-background ecommerce image; the product is complete, centered, sharp-edged and suitable for platform review.
Product title: {{.Output.ProductTitle}}
Description: {{.Output.Description}}
Original requirement: {{.Requirement}}
Unified product information:
{{.UnifiedInfo}}
Image parameters: size ` + spec.Size + `, aspect ratio ` + spec.AspectRatio + `, clarity ` + spec.Clarity + `
Composition requirement: ` + composition + `
Language requirement: {{.LanguageRule}}
Shape fidelity: strictly preserve the product category, structure, foldable/portable/storage features and other key forms from the original requirement and reference images. If reference images are provided, their product appearance, proportions, colors, parts and structure have highest priority.
Strict requirements: pure white or near-white background; no title, no selling-point text, no price, no promotion tag, no icon, no sticker, no border, no watermark, no brand logo, no scene props, no hands or people; realistic, clear, complete, standalone product.`
		return renderTemplate(src, data)
	}
	src := `Use the following as visual direction, not as literal overlay text:
{{.Prompt.ImagePrompt}}
{{.Style.StylePrompt}}
Platform: {{.Platform.Name}}
Image type: {{.AssetType}}
Goal: ` + assetText + `
Original requirement: {{.Requirement}}
Core product facts:
{{.CompactUnifiedInfo}}
Visible text pool for this image:
{{.CompactImageTextPlan}}
Image parameters: size ` + spec.Size + `, aspect ratio ` + spec.AspectRatio + `, clarity ` + spec.Clarity + `
Composition: ` + composition + `
Hard constraints:
- Keep the same product identity as the reference images.
- Do not copy the white background, angle, crop or lighting from the references.
- This asset must look clearly different from the white-background image.
- For non-white assets, do not output a plain centered product cutout on a white background.
- Follow user requests about scenes, spokesperson mood, campaign mood or brand atmosphere when present.
- {{.LanguageRule}}
- Do not invent prices, discounts, models or specs.`
	return renderTemplate(src, data)
}

func (r *Runner) buildRetryImagePrompt(platform Platform, prompt PromptTemplate, style StyleTemplate, out Output, requirement, assetType, extraPrompt string) string {
	extraPrompt = strings.TrimSpace(extraPrompt)
	if extraPrompt == "" {
		return r.buildImagePrompt(platform, prompt, style, out, requirement, assetType)
	}
	langCode := platformLanguageCode(platform)
	if strings.HasPrefix(strings.ToLower(langCode), "en") {
		return r.buildRetryEnglishImagePrompt(platform, out, requirement, assetType, extraPrompt, langCode)
	}
	return r.buildRetryChineseImagePrompt(platform, out, requirement, assetType, extraPrompt, langCode)
}

func (r *Runner) buildRetryChineseImagePrompt(platform Platform, out Output, requirement, assetType, extraPrompt, langCode string) string {
	assetText := map[string]string{
		AssetTitle:  "店标题图，突出商品名称和核心价值",
		AssetMain:   "电商主图，商品主体清晰，适合列表和首屏",
		AssetWhite:  "商品白底图，只展示商品本体",
		AssetDetail: "详情页长图模块，展示卖点、场景和参数",
		AssetPrice:  "价格促销图，突出优惠和行动号召",
	}[assetType]
	spec := out.ImageSpecs[assetType]
	src := `平台：{{.Platform.Name}}
图片类型：{{.AssetType}}
目标：` + assetText + `
` + `{{.RetryExtra}}
原始需求：{{.Requirement}}
统一商品信息：
{{.CompactUnifiedInfo}}
本图允许出现的文字：
{{.CompactImageTextPlan}}
图片参数：尺寸 ` + spec.Size + `，长宽比 ` + spec.AspectRatio + `，清晰度 ` + spec.Clarity + `
构图要求：` + assetCompositionCN(assetType) + `
硬约束：
` + assetRetryRulesCN(assetType) + `
- 没有价格数字时不得编造价格数字，不得新增、替换、改写标题、规格、型号。`
	return renderTemplate(src, renderData{
		Requirement:          requirement,
		Platform:             platform,
		Output:               out,
		AssetType:            assetType,
		CompactUnifiedInfo:   formatCompactUnifiedInfoForLanguage(out, langCode),
		CompactImageTextPlan: formatCompactImageTextPlanForLanguage(out.ImageTextPlans[assetType], assetType, langCode),
		RetryExtra:           extraPrompt,
		LanguageCode:         langCode,
		LanguageName:         platformLanguageName(langCode),
		LanguageRule:         platformLanguageRule(langCode),
	})
}

func (r *Runner) buildRetryEnglishImagePrompt(platform Platform, out Output, requirement, assetType, extraPrompt, langCode string) string {
	assetText := map[string]string{
		AssetTitle:  "store title image that highlights the product name and core value",
		AssetMain:   "main ecommerce image with a clear product hero, suitable for listings and above-the-fold display",
		AssetWhite:  "white-background product image showing only the product itself",
		AssetDetail: "detail-page module image showing selling points, scenes and specs",
		AssetPrice:  "price promotion image highlighting the offer and call to action",
	}[assetType]
	spec := out.ImageSpecs[assetType]
	src := `Platform: {{.Platform.Name}}
Image type: {{.AssetType}}
Goal: ` + assetText + `
{{.RetryExtra}}
Original requirement: {{.Requirement}}
Core product facts:
{{.CompactUnifiedInfo}}
Visible text pool for this image:
{{.CompactImageTextPlan}}
Image parameters: size ` + spec.Size + `, aspect ratio ` + spec.AspectRatio + `, clarity ` + spec.Clarity + `
Composition: ` + assetCompositionEN(assetType) + `
Hard constraints:
- ` + assetRetryRulesEN(assetType) + `
- Do not invent prices, discounts, models or specs.`
	return renderTemplate(src, renderData{
		Requirement:          requirement,
		Platform:             platform,
		Output:               out,
		AssetType:            assetType,
		CompactUnifiedInfo:   formatCompactUnifiedInfoForLanguage(out, langCode),
		CompactImageTextPlan: formatCompactImageTextPlanForLanguage(out.ImageTextPlans[assetType], assetType, langCode),
		RetryExtra:           extraPrompt,
		LanguageCode:         langCode,
		LanguageName:         platformLanguageName(langCode),
		LanguageRule:         platformLanguageRule(langCode),
	})
}

func (r *Runner) firstModel(ctx context.Context, modelType string) (*modelpkg.Model, error) {
	list, err := r.models.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range list {
		if m.Type == modelType {
			return m, nil
		}
	}
	return nil, fmt.Errorf("未配置可用的 %s 模型", modelType)
}

func assetCompositionCN(assetType string) string {
	switch assetType {
	case AssetTitle:
		return "横版品牌海报，商品放在画面右侧或左侧三分之一，留出大标题和核心卖点区，必须有明确营销版式。"
	case AssetMain:
		return "商品主图，单一主视觉，使用干净渐变或平台友好背景，展示商品使用氛围，但不要与白底图同角度同裁切。"
	case AssetWhite:
		return "标准白底商品图，居中、完整、无文字，只保留商品本体。"
	case AssetDetail:
		return "竖版详情模块，分区展示卖点、参数和场景，可使用信息卡片、局部特写和图文排版。"
	case AssetPrice:
		return "竖版促销图，突出价格、优惠、CTA 和购买理由，使用强视觉层级，不做纯产品白底图。"
	default:
		return "按当前图片类型设计独立构图，避免与其他资产重复。"
	}
}

func assetCompositionEN(assetType string) string {
	switch assetType {
	case AssetTitle:
		return "horizontal brand poster; place the product on the left or right third, reserve space for a large headline and key value, with a clear marketing layout."
	case AssetMain:
		return "main product visual with a clean gradient or platform-safe background and usage atmosphere; do not reuse the white-background angle or crop."
	case AssetWhite:
		return "standard white-background product image, centered, complete, no text, product only."
	case AssetDetail:
		return "vertical detail module with sections for selling points, specs and scenes; use info cards, close-ups and editorial layout."
	case AssetPrice:
		return "vertical promotion image emphasizing price, discount, CTA and purchase reasons with strong visual hierarchy; not a plain product cutout."
	default:
		return "design an independent composition for this asset type and avoid repeating other assets."
	}
}

func assetRetryRulesCN(assetType string) string {
	if assetType == AssetWhite {
		return "- 必须保持参考图中的商品主体一致。\n- 保持干净白底，只展示商品本体，不要加入场景、文字或装饰元素。"
	}
	return "- 必须保持参考图中的商品主体一致。\n- 不得复用白底图的纯白背景、居中孤立摆放、角度、裁切和光影。\n- 当前资产必须与白底图明显不同。\n- 非白底图不得退化成白底商品图。"
}

func assetRetryRulesEN(assetType string) string {
	if assetType == AssetWhite {
		return "Keep the same product identity as the reference images.\n- Keep a clean white background with the product only; no scene, text or decorative elements."
	}
	return "Keep the same product identity as the reference images.\n- Do not reuse the white-background look, centered cutout, angle, crop or lighting from the white image.\n- This asset must look clearly different from the white-background image.\n- For non-white assets, do not output a plain centered product cutout on a white background."
}

func referencesForAsset(assetType string, refs []imgpkg.ReferenceImage) []imgpkg.ReferenceImage {
	// 所有图片都使用参考图锁定商品外观，差异化交给每类 asset 的构图规则控制。
	return refs
}

func (r *Runner) retryReferences(ctx context.Context, taskID string, asset Asset, fallback []imgpkg.ReferenceImage) []imgpkg.ReferenceImage {
	if asset.Status == StatusSuccess && asset.ImageTaskID != "" {
		if refs, err := r.referenceFromImageTask(ctx, asset.ImageTaskID, asset.AssetType+"-retry-reference.png"); err == nil {
			return refs
		} else {
			logger.L().Warn("ecommerce retry current image reference failed",
				zap.String("task_id", taskID), zap.Uint64("asset_id", asset.ID), zap.Error(err))
		}
	}
	if asset.AssetType != AssetWhite {
		if assets, err := r.dao.ListAssets(ctx, taskID); err == nil {
			for _, item := range assets {
				if item.AssetType != AssetWhite || item.Status != StatusSuccess || item.ImageTaskID == "" {
					continue
				}
				if refs, err := r.referenceFromImageTask(ctx, item.ImageTaskID, "white-retry-reference.png"); err == nil {
					return refs
				} else {
					logger.L().Warn("ecommerce retry white image reference failed",
						zap.String("task_id", taskID), zap.Uint64("asset_id", asset.ID), zap.Error(err))
				}
				break
			}
		}
	}
	return referencesForAsset(asset.AssetType, fallback)
}

func (r *Runner) referenceFromImageTask(ctx context.Context, taskID, fileName string) ([]imgpkg.ReferenceImage, error) {
	if r.imageDAO == nil || r.imageAcc == nil {
		return nil, errors.New("image reference resolver not ready")
	}
	imgTask, err := r.imageDAO.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if imgTask.Status != imgpkg.StatusSuccess {
		return nil, fmt.Errorf("image task status %s", imgTask.Status)
	}
	refs := imgTask.DecodeFileIDs()
	if len(refs) == 0 || imgTask.ConversationID == "" || imgTask.AccountID == 0 {
		return nil, errors.New("image task missing download metadata")
	}
	at, deviceID, cookies, err := r.imageAcc.AuthToken(ctx, imgTask.AccountID)
	if err != nil {
		return nil, err
	}
	cli, err := chatgpt.New(chatgpt.Options{
		AuthToken: at,
		DeviceID:  deviceID,
		ProxyURL:  r.imageAcc.ProxyURL(ctx, imgTask.AccountID),
		Cookies:   cookies,
		Timeout:   90 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	signedURL, err := cli.ImageDownloadURL(ctx, imgTask.ConversationID, refs[0])
	if err != nil {
		return nil, err
	}
	data, _, err := cli.FetchImage(ctx, signedURL, maxReferenceImageBytes)
	if err != nil {
		return nil, err
	}
	if len(data) > maxReferenceImageBytes {
		return nil, fmt.Errorf("参考图超过 %dMB", maxReferenceImageBytes/1024/1024)
	}
	return []imgpkg.ReferenceImage{{Data: data, FileName: fileName}}, nil
}

func whiteReferenceFromResult(ctx context.Context, res *imgpkg.RunResult) ([]imgpkg.ReferenceImage, error) {
	if res == nil || res.Status != imgpkg.StatusSuccess {
		return nil, errors.New("白底图未生成成功")
	}
	if len(res.ImageBytes) > 0 && len(res.ImageBytes[0]) > 0 {
		return []imgpkg.ReferenceImage{{Data: res.ImageBytes[0], FileName: "white-reference.png"}}, nil
	}
	if len(res.SignedURLs) == 0 || strings.TrimSpace(res.SignedURLs[0]) == "" {
		return nil, errors.New("白底图缺少下载地址")
	}
	data, name, err := fetchReferenceBytes(ctx, res.SignedURLs[0])
	if err != nil {
		return nil, fmt.Errorf("读取白底图参考失败:%w", err)
	}
	if strings.TrimSpace(name) == "" {
		name = "white-reference.png"
	}
	return []imgpkg.ReferenceImage{{Data: data, FileName: name}}, nil
}

func decodeReferenceInputs(ctx context.Context, raw json.RawMessage) ([]imgpkg.ReferenceImage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var inputs []string
	if err := json.Unmarshal(raw, &inputs); err != nil {
		return nil, fmt.Errorf("参考图格式错误:%w", err)
	}
	if len(inputs) > maxReferenceImages {
		return nil, fmt.Errorf("最多支持 %d 张参考图", maxReferenceImages)
	}
	out := make([]imgpkg.ReferenceImage, 0, len(inputs))
	for i, s := range inputs {
		data, name, err := fetchReferenceBytes(ctx, strings.TrimSpace(s))
		if err != nil {
			return nil, fmt.Errorf("第 %d 张参考图:%w", i+1, err)
		}
		if len(data) > maxReferenceImageBytes {
			return nil, fmt.Errorf("第 %d 张参考图超过 20MB", i+1)
		}
		out = append(out, imgpkg.ReferenceImage{Data: data, FileName: name})
	}
	return out, nil
}

func fetchReferenceBytes(ctx context.Context, s string) ([]byte, string, error) {
	if s == "" {
		return nil, "", errors.New("内容为空")
	}
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "data:") {
		comma := strings.IndexByte(s, ',')
		if comma < 0 {
			return nil, "", errors.New("无效 data URL")
		}
		payload := s[comma+1:]
		b, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, "", err
		}
		return b, "", nil
	}
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s, nil)
		if err != nil {
			return nil, "", err
		}
		res, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
		if err != nil {
			return nil, "", err
		}
		defer res.Body.Close()
		if res.StatusCode >= 400 {
			return nil, "", fmt.Errorf("下载失败 HTTP %d", res.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(res.Body, int64(maxReferenceImageBytes)+1))
		if err != nil {
			return nil, "", err
		}
		return b, filepath.Base(req.URL.Path), nil
	}
	b, err := base64.StdEncoding.DecodeString(s)
	return b, "", err
}
