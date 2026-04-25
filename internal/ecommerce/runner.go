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

type Runner struct {
	dao       *DAO
	models    *modelpkg.Registry
	scheduler *scheduler.Scheduler
	acc       AccountSecretResolver
	channels  *channel.Router
	imageDAO  *imgpkg.DAO
	imageRun  *imgpkg.Runner
}

func NewRunner(dao *DAO, models *modelpkg.Registry, sched *scheduler.Scheduler, acc AccountSecretResolver, channels *channel.Router, imageDAO *imgpkg.DAO, imageRun *imgpkg.Runner) *Runner {
	return &Runner{dao: dao, models: models, scheduler: sched, acc: acc, channels: channels, imageDAO: imageDAO, imageRun: imageRun}
}

func (r *Runner) Enqueue(taskID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		if err := r.Run(ctx, taskID); err != nil {
			logger.L().Warn("ecommerce task failed", zap.String("task_id", taskID), zap.Error(err))
			_ = r.dao.MarkTaskFailed(context.Background(), taskID, err.Error())
		}
	}()
}

func (r *Runner) EnqueueAssetRetry(taskID string, assetID uint64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := r.RetryAsset(ctx, taskID, assetID); err != nil {
			logger.L().Warn("ecommerce asset retry failed",
				zap.String("task_id", taskID), zap.Uint64("asset_id", assetID), zap.Error(err))
		}
	}()
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
	outBytes, _ := json.Marshal(out)
	_ = r.dao.UpdateTaskProgress(ctx, taskID, 35)

	refs, err := decodeReferenceInputs(ctx, task.ReferenceImages.RawMessage())
	if err != nil {
		return err
	}
	imageModel, err := r.firstModel(ctx, modelpkg.TypeImage)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var completed int
	assetErrors := make([]string, 0)
	for _, assetType := range assetTypes {
		assetPrompt := r.buildImagePrompt(*platform, *prompt, *style, out, task.Requirement, assetType)
		spec := out.ImageSpecs[assetType]
		imgTaskID := imgpkg.GenerateTaskID()
		asset := &Asset{TaskID: taskID, AssetType: assetType, ImageTaskID: imgTaskID, Prompt: assetPrompt, Status: StatusRunning}
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
		wg.Add(1)
		go func(assetID uint64, assetType, imgTaskID, assetPrompt string) {
			defer wg.Done()
			res := r.imageRun.Run(ctx, imgpkg.RunOptions{
				TaskID:        imgTaskID,
				UserID:        task.UserID,
				ModelID:       imageModel.ID,
				UpstreamModel: imageModel.UpstreamModelSlug,
				Prompt:        assetPrompt,
				N:             1,
				MaxAttempts:   1,
				References:    refs,
			})
			if res.Status != imgpkg.StatusSuccess {
				errCode := res.ErrorCode
				if errCode == "" {
					errCode = res.ErrorMessage
				}
				if errCode == "" {
					errCode = "unknown"
				}
				_ = r.dao.UpdateAssetResult(context.Background(), assetID, StatusFailed, imgTaskID, "", "", errCode)
				mu.Lock()
				assetErrors = append(assetErrors, fmt.Sprintf("%s:%s", assetType, errCode))
				completed++
				progress := 35 + completed*10
				mu.Unlock()
				_ = r.dao.UpdateTaskProgress(context.Background(), taskID, progress)
				return
			}
			url := ""
			fileID := ""
			if len(res.SignedURLs) > 0 {
				url = imgpkg.BuildProxyURL(imgTaskID, 0, 24*time.Hour)
			}
			if len(res.FileIDs) > 0 {
				fileID = strings.TrimPrefix(res.FileIDs[0], "sed:")
			}
			_ = r.dao.UpdateAssetResult(context.Background(), assetID, StatusSuccess, imgTaskID, url, fileID, "")
			mu.Lock()
			completed++
			progress := 35 + completed*10
			mu.Unlock()
			_ = r.dao.UpdateTaskProgress(context.Background(), taskID, progress)
		}(asset.ID, assetType, imgTaskID, assetPrompt)
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

func (r *Runner) RetryAsset(ctx context.Context, taskID string, assetID uint64) error {
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
	assetPrompt := r.buildImagePrompt(*platform, *prompt, *style, out, task.Requirement, asset.AssetType)
	spec := out.ImageSpecs[asset.AssetType]
	imgTaskID := imgpkg.GenerateTaskID()
	if err := r.dao.MarkTaskRetrying(ctx, taskID); err != nil {
		return err
	}
	if err := r.dao.MarkAssetRetrying(ctx, assetID, imgTaskID, assetPrompt); err != nil {
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
	res := r.imageRun.Run(ctx, imgpkg.RunOptions{
		TaskID:        imgTaskID,
		UserID:        task.UserID,
		ModelID:       imageModel.ID,
		UpstreamModel: imageModel.UpstreamModelSlug,
		Prompt:        assetPrompt,
		N:             1,
		MaxAttempts:   1,
		References:    refs,
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
	return []chatgpt.ChatMessage{
		{Role: "system", Content: "你是资深电商详情页策划。只输出 JSON，不要输出 Markdown。"},
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
内容策略：{{.Prompt.ContentPrompt}}
视觉风格：{{.Style.StylePrompt}}

请生成可直接用于电商详情页的结构化内容，返回严格 JSON：
{
  "shop_title": "店铺标题",
  "product_title": "商品标题",
  "description": "商品描述",
  "price_copy": "价格/促销文案",
  "marketing_copy": ["营销短句1", "营销短句2", "营销短句3"],
  "detail_sections": [{"title": "模块标题", "body": "模块正文"}],
  "platform_fields": {"title": "平台标题", "description": "平台描述"},
  "image_specs": {
    "title_image": {"size": "1024x1024", "aspect_ratio": "1:1", "clarity": "high"},
    "main_image": {"size": "1024x1024", "aspect_ratio": "1:1", "clarity": "high"},
    "white_image": {"size": "1024x1024", "aspect_ratio": "1:1", "clarity": "high"},
    "detail_image": {"size": "1024x1536", "aspect_ratio": "2:3", "clarity": "high"},
    "price_image": {"size": "1024x1024", "aspect_ratio": "1:1", "clarity": "high"}
  }
}`
	return renderTemplate(tpl, renderData{Requirement: requirement, Platform: platform, Prompt: prompt, Style: style})
}

func (r *Runner) buildImagePrompt(platform Platform, prompt PromptTemplate, style StyleTemplate, out Output, requirement, assetType string) string {
	assetText := map[string]string{
		AssetTitle:  "店标题图，突出商品名称和核心价值",
		AssetMain:   "电商主图，商品主体清晰，适合列表和首屏",
		AssetWhite:  "商品白底图，只展示商品本体",
		AssetDetail: "详情页长图模块，展示卖点、场景和参数",
		AssetPrice:  "价格促销图，突出优惠和行动号召",
	}[assetType]
	spec := out.ImageSpecs[assetType]
	if assetType == AssetWhite {
		src := `平台：{{.Platform.Name}}
图片类型：白底图
目标：干净的商品白底图，只展示商品本体，商品完整居中，边缘清晰，适合电商平台商品主图审核。
商品标题：{{.Output.ProductTitle}}
描述：{{.Output.Description}}
原始需求：{{.Requirement}}
图片参数：尺寸 ` + spec.Size + `，长宽比 ` + spec.AspectRatio + `，清晰度 ` + spec.Clarity + `
严格要求：纯白或接近纯白背景；无标题、无卖点文字、无价格、无促销标签、无图标、无贴纸、无边框、无水印、无品牌标志、无场景道具、无人物手部；商品真实、清晰、完整、单独呈现。`
		return renderTemplate(src, renderData{
			Requirement: requirement,
			Platform:    platform,
			Output:      out,
		})
	}
	src := `{{.Prompt.ImagePrompt}}
{{.Style.StylePrompt}}
平台：{{.Platform.Name}}
图片类型：{{.AssetType}}
目标：` + assetText + `
商品标题：{{.Output.ProductTitle}}
描述：{{.Output.Description}}
价格文案：{{.Output.PriceCopy}}
原始需求：{{.Requirement}}
图片参数：尺寸 ` + spec.Size + `，长宽比 ` + spec.AspectRatio + `，清晰度 ` + spec.Clarity + `
要求：中文文字清晰可读，商业电商设计，避免虚假品牌标志。`
	return renderTemplate(src, renderData{
		Requirement: requirement,
		Platform:    platform,
		Prompt:      prompt,
		Style:       style,
		Output:      out,
		AssetType:   assetType,
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
