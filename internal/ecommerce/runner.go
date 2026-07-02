package ecommerce

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
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
	"github.com/432539/gpt2api/internal/textgen"
	"github.com/432539/gpt2api/internal/upstream/adapter"
	"github.com/432539/gpt2api/internal/upstream/chatgpt"
	"github.com/432539/gpt2api/internal/videogen"
	"github.com/432539/gpt2api/pkg/logger"
)

const (
	maxReferenceImages     = 4
	maxReferenceImageBytes = 20 * 1024 * 1024
	videoProductAnchorName = "product_anchor"
)

type AccountSecretResolver interface {
	DecryptCookies(ctx context.Context, accountID uint64) (string, error)
}

type ImageAccountResolver interface {
	AuthToken(ctx context.Context, accountID uint64) (at, deviceID, sessionID, cookies string, err error)
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
	textGen          *textgen.Client
	videoGen         *videogen.Client
	appBaseURL       string
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

func (r *Runner) SetTextGenClient(client *textgen.Client) {
	r.textGen = client
}

func (r *Runner) SetVideoGenClient(client *videogen.Client) {
	r.videoGen = client
}

func (r *Runner) SetAppBaseURL(baseURL string) {
	r.appBaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

func (r *Runner) VideoEnabled() bool {
	return r.videoGen != nil && r.videoGen.Enabled()
}

func (r *Runner) SyncVideoAsset(ctx context.Context, asset Asset) error {
	if r.videoGen == nil || !r.videoGen.Enabled() || asset.AssetType != AssetVideo || !isUUID(asset.ImageTaskID) {
		return nil
	}
	result, err := r.videoGen.GetTask(ctx, asset.ImageTaskID)
	if err != nil {
		return err
	}
	status := strings.ToLower(strings.TrimSpace(result.Status))
	switch status {
	case "completed":
		if strings.TrimSpace(result.ResultURL) == "" {
			return nil
		}
		fileID := "videogen:" + firstNonEmpty(result.TaskID, asset.ImageTaskID)
		return r.dao.UpdateAssetResult(ctx, asset.ID, StatusSuccess, firstNonEmpty(result.TaskID, asset.ImageTaskID), strings.TrimSpace(result.ResultURL), fileID, "")
	case "failed":
		msg := strings.TrimSpace(result.ErrorMessage)
		if msg == "" {
			msg = "videogen task failed"
		}
		return r.dao.UpdateAssetResult(ctx, asset.ID, StatusFailed, firstNonEmpty(result.TaskID, asset.ImageTaskID), "", "", msg)
	default:
		progress := result.Progress
		if progress >= 100 {
			progress = 99
		}
		return r.dao.UpdateAssetVideoProgress(ctx, asset.ID, result.Status, firstNonEmpty(result.TaskID, asset.ImageTaskID), progress)
	}
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
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		if err := r.RetryAsset(ctx, taskID, assetID, extraPrompt); err != nil {
			logger.L().Warn("ecommerce asset retry failed",
				zap.String("task_id", taskID), zap.Uint64("asset_id", assetID), zap.Error(err))
		}
	}()
}

func (r *Runner) EnqueueVideo(taskID string, extraPrompt string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		if err := r.GenerateVideo(ctx, taskID, extraPrompt); err != nil {
			logger.L().Warn("ecommerce video generation failed", zap.String("task_id", taskID), zap.Error(err))
		}
	}()
}

func (r *Runner) RetryTask(ctx context.Context, taskID string) error {
	task, err := r.dao.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	switch task.Status {
	case StatusQueued, StatusRunning:
		return errors.New("任务正在生成中")
	case StatusSuccess:
		return errors.New("任务已完成，无需整体重试")
	case StatusFailed, StatusCanceled:
		if err := r.dao.ResetTaskForRetry(ctx, taskID); err != nil {
			return err
		}
		r.Enqueue(taskID)
		return nil
	default:
		return errors.New("当前任务状态不支持整体重试")
	}
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

	platformForTask := platformWithLanguage(*platform, task.Language)
	rawText, err := r.generateText(ctx, platformForTask, *prompt, *style, task.Requirement)
	var out Output
	if err != nil {
		if !canFallbackText(err) {
			return err
		}
		logger.L().Warn("ecommerce text upstream failed, using local draft",
			zap.String("task_id", taskID), zap.Error(err))
		out = localDraftOutput(platformForTask, *style, task.Requirement)
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
		assetPrompt := r.buildImagePrompt(platformForTask, *prompt, *style, out, task.Requirement, assetType)
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

	whiteRes := runAsset(whiteJob, referencesForAsset(whiteJob.assetTyp, refs, nil))
	whiteRefs := imageResultReferences(whiteRes, AssetWhite+"-product-anchor.png")
	if len(whiteRefs) == 0 {
		logger.L().Warn("ecommerce white image anchor unavailable, fallback to original references",
			zap.String("task_id", taskID),
			zap.String("status", whiteRes.Status),
			zap.String("error_code", whiteRes.ErrorCode),
			zap.String("error_message", whiteRes.ErrorMessage))
	} else {
		logger.L().Info("ecommerce white image anchor ready",
			zap.String("task_id", taskID),
			zap.Int("refs", len(whiteRefs)))
	}
	for _, job := range jobs {
		if job.assetTyp == AssetWhite {
			continue
		}
		wg.Add(1)
		go func(job assetJob) {
			defer wg.Done()
			_ = runAsset(job, referencesForAsset(job.assetTyp, refs, whiteRefs))
		}(job)
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
	if err := r.generateVideoAsset(ctx, taskID, platformForTask, *prompt, *style, out, task.Requirement, whiteRefs); err != nil {
		html, _ = r.rebuildTaskHTML(dbCtx, taskID, out)
		return r.dao.MarkTaskFailedWithOutput(dbCtx, taskID, "视频生成失败: "+err.Error(), outBytes, html)
	}
	if rebuilt, err := r.rebuildTaskHTML(dbCtx, taskID, out); err == nil {
		html = rebuilt
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
		return errors.New("资产正在生成中")
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
	platformForTask := platformWithLanguage(*platform, task.Language)
	out := outputFromTask(task)
	if out.ProductTitle == "" {
		out = localDraftOutput(platformForTask, *style, task.Requirement)
	}
	normalizeOutput(&out, task.Requirement)
	if asset.AssetType == AssetVideo {
		assetPrompt := r.buildRetryVideoPrompt(platformForTask, *prompt, *style, out, task.Requirement, extraPrompt)
		vidTaskID := videogen.GenerateTaskID()
		videoRefs := r.videoReferences(ctx, taskID, nil)
		if err := r.dao.MarkTaskRetrying(ctx, taskID); err != nil {
			return err
		}
		if err := r.dao.MarkAssetRetrying(ctx, assetID, vidTaskID, assetPrompt); err != nil {
			return err
		}
		if err := r.runVideoAsset(ctx, taskID, assetID, vidTaskID, assetPrompt, videoRefs); err != nil {
			_ = r.dao.UpdateAssetResult(context.Background(), assetID, StatusFailed, vidTaskID, "", "", err.Error())
			return r.finalizeTaskAfterRetry(context.Background(), taskID, out, "视频重试失败: "+err.Error())
		}
		return r.finalizeTaskAfterRetry(context.Background(), taskID, out, "")
	}
	imageModel, err := r.firstModel(ctx, modelpkg.TypeImage)
	if err != nil {
		return err
	}
	refs, err := decodeReferenceInputs(ctx, task.ReferenceImages.RawMessage())
	if err != nil {
		return err
	}
	assetPrompt := r.buildRetryImagePrompt(platformForTask, *prompt, *style, out, task.Requirement, asset.AssetType, extraPrompt)
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
	if err := r.dao.MarkAssetRetrying(ctx, assetID, imgTaskID, assetPrompt); err != nil {
		return err
	}
	release, err := r.acquireImageSlot(ctx)
	if err != nil {
		errCode := err.Error()
		_ = r.dao.UpdateAssetResult(context.Background(), assetID, StatusFailed, imgTaskID, "", "", errCode)
		return r.finalizeTaskAfterRetry(context.Background(), taskID, out, "图片重试失败: "+asset.AssetType+":"+errCode)
	}
	defer release()
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

func (r *Runner) GenerateVideo(ctx context.Context, taskID string, extraPrompt string) error {
	task, err := r.dao.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if task.Status == StatusQueued || task.Status == StatusRunning {
		return errors.New("任务主流程正在生成中，请稍后再生成视频")
	}
	if task.Status == StatusCanceled {
		return errors.New("任务已中断，不能生成视频")
	}
	if r.videoGen == nil || !r.videoGen.Enabled() {
		return errors.New("视频网关未启用或未配置密钥")
	}
	latest, err := r.dao.GetLatestAssetByType(ctx, taskID, AssetVideo)
	if err == nil && (latest.Status == StatusQueued || latest.Status == StatusRunning) {
		return errors.New("视频正在生成中")
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
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
	platformForTask := platformWithLanguage(*platform, task.Language)
	out := outputFromTask(task)
	if out.ProductTitle == "" {
		out = localDraftOutput(platformForTask, *style, task.Requirement)
	}
	normalizeOutput(&out, task.Requirement)
	assetPrompt := r.buildRetryVideoPrompt(platformForTask, *prompt, *style, out, task.Requirement, extraPrompt)
	vidTaskID := videogen.GenerateTaskID()
	videoRefs := r.videoReferences(ctx, taskID, nil)
	asset := &Asset{
		TaskID:      taskID,
		AssetType:   AssetVideo,
		ImageTaskID: vidTaskID,
		Prompt:      assetPrompt,
		Status:      StatusQueued,
	}
	if err := r.dao.CreateAsset(ctx, asset); err != nil {
		return err
	}
	if err := r.dao.MarkTaskRetrying(ctx, taskID); err != nil {
		return err
	}
	if err := r.runVideoAsset(ctx, taskID, asset.ID, vidTaskID, assetPrompt, videoRefs); err != nil {
		_ = r.dao.UpdateAssetResult(context.Background(), asset.ID, StatusFailed, vidTaskID, "", "", err.Error())
		return r.finalizeTaskAfterRetry(context.Background(), taskID, out, "视频生成失败: "+err.Error())
	}
	return r.finalizeTaskAfterRetry(context.Background(), taskID, out, "")
}

func (r *Runner) generateVideoAsset(ctx context.Context, taskID string, platform Platform, prompt PromptTemplate, style StyleTemplate, out Output, requirement string, whiteRefs []imgpkg.ReferenceImage) error {
	if r.videoGen == nil || !r.videoGen.Enabled() {
		return nil
	}
	vidTaskID := videogen.GenerateTaskID()
	assetPrompt := r.buildVideoPrompt(platform, prompt, style, out, requirement)
	asset := &Asset{
		TaskID:      taskID,
		AssetType:   AssetVideo,
		ImageTaskID: vidTaskID,
		Prompt:      assetPrompt,
		Status:      StatusQueued,
	}
	if err := r.dao.CreateAsset(ctx, asset); err != nil {
		return err
	}
	if err := r.runVideoAsset(ctx, taskID, asset.ID, vidTaskID, assetPrompt, r.videoReferences(ctx, taskID, whiteRefs)); err != nil {
		_ = r.dao.UpdateAssetResult(context.Background(), asset.ID, StatusFailed, vidTaskID, "", "", err.Error())
		return err
	}
	return nil
}

func (r *Runner) runVideoAsset(ctx context.Context, taskID string, assetID uint64, vidTaskID, prompt string, refs []imgpkg.ReferenceImage) error {
	if r.videoGen == nil {
		return errors.New("视频网关未初始化")
	}
	images, err := videoReferenceImages(refs)
	if err != nil {
		return err
	}
	if len(images) == 0 {
		logger.L().Warn("ecommerce video reference unavailable, fallback to text-to-video",
			zap.String("task_id", taskID),
			zap.Uint64("asset_id", assetID))
	} else {
		logger.L().Info("ecommerce video reference attached",
			zap.String("task_id", taskID),
			zap.Uint64("asset_id", assetID),
			zap.Int("refs", len(images)))
		prompt = withVideoReferencePrompt(prompt)
	}
	res, err := r.videoGen.Generate(ctx, videogen.Options{
		Images: images,
		Prompt: prompt,
		OnProgress: func(result videogen.Result) {
			progress := result.Progress
			if progress >= 100 {
				progress = 99
			}
			upstreamTaskID := firstNonEmpty(result.TaskID, vidTaskID)
			_ = r.dao.UpdateAssetVideoProgress(context.Background(), assetID, result.Status, upstreamTaskID, progress)
		},
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(res.ResultURL) == "" {
		return errors.New("视频结果为空")
	}
	upstreamTaskID := firstNonEmpty(res.TaskID, vidTaskID)
	fileID := "videogen:" + upstreamTaskID
	if err := r.dao.UpdateAssetResult(context.Background(), assetID, StatusSuccess, upstreamTaskID, strings.TrimSpace(res.ResultURL), fileID, ""); err != nil {
		return err
	}
	_ = r.dao.UpdateTaskProgress(context.Background(), taskID, 95)
	return nil
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
	assets = latestAssetsByType(assets)
	return buildHTML(out, assets), nil
}

func (r *Runner) finalizeTaskAfterRetry(ctx context.Context, taskID string, out Output, errMsg string) error {
	assets, err := r.dao.ListAssets(ctx, taskID)
	if err != nil {
		return err
	}
	assets = latestAssetsByType(assets)
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
		case StatusCanceled:
			if a.Error != "" {
				failed = append(failed, a.AssetType+":"+a.Error)
			} else {
				failed = append(failed, a.AssetType+":已中断")
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
	if r.textGen != nil && r.textGen.Enabled() {
		text, err := r.generateTextByTextGen(ctx, chatModel, msgs)
		if err == nil {
			return text, nil
		}
		logger.L().Warn("ecommerce textgen failed, fallback",
			zap.String("model", chatModel.Slug),
			zap.Error(err))
	}
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

func (r *Runner) generateTextByTextGen(ctx context.Context, chatModel *modelpkg.Model, msgs []chatgpt.ChatMessage) (string, error) {
	model := strings.TrimSpace(chatModel.UpstreamModelSlug)
	if model == "" || model == "auto" {
		model = strings.TrimSpace(chatModel.Slug)
	}
	stream, err := r.textGen.Chat(ctx, textgen.Options{
		Model:     model,
		Messages:  msgs,
		MaxTokens: 1200,
	})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for ch := range stream {
		if ch.Err != nil {
			return "", ch.Err
		}
		b.WriteString(ch.Delta)
	}
	if s := strings.TrimSpace(b.String()); s != "" {
		return s, nil
	}
	return "", errors.New("文本网关未返回电商文案")
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
	return strings.Contains(msg, "chat-requirements") ||
		strings.Contains(msg, "上游未返回电商文案") ||
		strings.Contains(msg, "渠道未返回电商文案")
}

func localDraftOutput(platform Platform, style StyleTemplate, requirement string) Output {
	name := compactRequirement(requirement, 34)
	if name == "" {
		name = "精选商品"
	}
	if normalizeLanguageCode(platformLanguageCode(platform)) != "zh-CN" {
		if name == "精选商品" {
			name = platformLanguageName(platformLanguageCode(platform)) + " Product"
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
	if normalizeLanguageCode(langCode) != "zh-CN" {
		return r.buildEnglishImagePrompt(platform, prompt, style, out, requirement, assetType, "", langCode)
	}
	return r.buildChineseImagePrompt(platform, prompt, style, out, requirement, assetType, "", langCode)
}

func (r *Runner) buildChineseImagePrompt(platform Platform, prompt PromptTemplate, style StyleTemplate, out Output, requirement, assetType, extraPrompt, langCode string) string {
	spec := out.ImageSpecs[assetType]
	return joinPromptLayers(
		subjectFidelityLayerCN(assetType),
		currentImageTaskLayerCN(platform, out, requirement, assetType, extraPrompt, spec, langCode),
		stylePromptLayerCN(prompt, style, assetType),
		negativePromptLayerCN(assetType, compactLanguageRule(langCode), extractCreativeRequirement(requirement)),
	)
}

func (r *Runner) buildEnglishImagePrompt(platform Platform, prompt PromptTemplate, style StyleTemplate, out Output, requirement, assetType, extraPrompt, langCode string) string {
	spec := out.ImageSpecs[assetType]
	return joinPromptLayers(
		subjectFidelityLayerEN(assetType),
		currentImageTaskLayerEN(platform, out, requirement, assetType, extraPrompt, spec, langCode),
		stylePromptLayerEN(prompt, style, assetType),
		negativePromptLayerEN(assetType, compactLanguageRule(langCode), extractCreativeRequirement(requirement)),
	)
}

func (r *Runner) buildRetryImagePrompt(platform Platform, prompt PromptTemplate, style StyleTemplate, out Output, requirement, assetType, extraPrompt string) string {
	extraPrompt = strings.TrimSpace(extraPrompt)
	if extraPrompt == "" {
		return r.buildImagePrompt(platform, prompt, style, out, requirement, assetType)
	}
	langCode := platformLanguageCode(platform)
	if normalizeLanguageCode(langCode) != "zh-CN" {
		return r.buildEnglishImagePrompt(platform, prompt, style, out, requirement, assetType, extraPrompt, langCode)
	}
	return r.buildChineseImagePrompt(platform, prompt, style, out, requirement, assetType, extraPrompt, langCode)
}

func (r *Runner) buildVideoPrompt(platform Platform, prompt PromptTemplate, style StyleTemplate, out Output, requirement string) string {
	return r.buildRetryVideoPrompt(platform, prompt, style, out, requirement, "")
}

func (r *Runner) buildRetryVideoPrompt(platform Platform, prompt PromptTemplate, style StyleTemplate, out Output, requirement, extraPrompt string) string {
	if tpl := strings.TrimSpace(prompt.VideoPrompt); tpl != "" {
		langCode := platformLanguageCode(platform)
		data := newRenderData(requirement, platform, prompt, style, out)
		data.RetryExtra = strings.TrimSpace(extraPrompt)
		if normalizeLanguageCode(langCode) != "zh-CN" {
			data.RetryExtraLine = extraVideoPromptLineEN(extraPrompt)
		} else {
			data.RetryExtraLine = extraVideoPromptLineCN(extraPrompt)
		}
		rendered := strings.TrimSpace(renderTemplate(tpl, data))
		if rendered == "" {
			return r.buildDefaultVideoPrompt(platform, prompt, style, out, requirement, extraPrompt)
		}
		if data.RetryExtra != "" && !strings.Contains(rendered, data.RetryExtra) {
			rendered = joinPromptLayers(rendered, data.RetryExtraLine)
		}
		return rendered
	}
	return r.buildDefaultVideoPrompt(platform, prompt, style, out, requirement, extraPrompt)
}

func (r *Runner) buildDefaultVideoPrompt(platform Platform, prompt PromptTemplate, style StyleTemplate, out Output, requirement, extraPrompt string) string {
	langCode := platformLanguageCode(platform)
	if normalizeLanguageCode(langCode) != "zh-CN" {
		return strings.Join(nonEmptyLines([]string{
			"Create a polished 5-second ecommerce product short video.",
			"Target platform: " + platform.Name,
			"Generation language: " + platformLanguageName(langCode) + " (" + langCode + ")",
			"Visible text language: " + compactLanguageRule(langCode),
			"Product title: " + firstNonEmpty(out.ProductInfo.CanonicalTitle, out.ProductTitle),
			"Core value: " + firstNonEmpty(out.ProductInfo.CoreValue, out.Description),
			"Selling points: " + strings.Join(out.ProductInfo.SellingPoints, " / "),
			"Key specs: " + strings.Join(out.ProductInfo.KeySpecs, " / "),
			"Marketing direction: " + strings.Join(out.MarketingCopy, " / "),
			"Price/promotion: " + firstNonEmpty(out.PriceInfo.PriceText, out.PriceCopy, out.PriceInfo.PromotionText),
			"Visual style: " + combineVisualDirection(prompt.ImagePrompt, style.StylePrompt),
			"Second-by-second script: each second must specify camera, lighting, background, sound and detail description.",
			"0-1s: Camera: hero product reveal, centered or rule-of-thirds composition, slow push-in; Lighting: soft key light with rim light; Background: clean ecommerce set or brand color blocks; Sound: gentle opening hit or low ambient bed; Details: emphasize product silhouette, material, color and true scale.",
			"1-2s: Camera: close-up of the core selling point or slight orbit; Lighting: strengthen material highlights and reflections; Background: keep the same visual world with subtle depth; Sound: delicate transition or material feedback sound; Details: show key structure, texture, interface, package or usage method.",
			"2-3s: Camera: natural use scene or functional demonstration; Lighting: bright, credible and product-friendly; Background: match the target platform and buyer scenario; Sound: rhythm lifts slightly; Details: make the selling point understandable through action, not invented claims.",
			"3-4s: Camera: detail close-up plus very short readable text; Lighting: focus on the important product area; Background: clean, layered and not cluttered; Sound: clear cue or light beat; Details: do not invent prices, specs, logos, certifications or brand promises.",
			"4-5s: Camera: ecommerce poster-like final frame with product and core benefit clear; Lighting: stable premium finish; Background: unified with the earlier shots; Sound: natural resolve; Details: keep product identity, proportion, color, material and requirement consistent.",
			"Visual restrictions: no wrong text, misspellings, watermarks, QR codes, low-resolution noise, exaggerated deformation or infringing brand elements.",
			"Keep the product identity consistent with the brief: " + compactRequirement(requirement, 220),
			extraVideoPromptLineEN(extraPrompt),
		}), "\n")
	}
	return strings.Join(nonEmptyLines([]string{
		"生成一支 5 秒精致电商商品短视频。",
		"目标平台：" + platform.Name,
		"生成语言：" + platformLanguageName(langCode) + "（" + langCode + "）",
		"可见文字语言：" + compactLanguageRule(langCode),
		"商品标题：" + firstNonEmpty(out.ProductInfo.CanonicalTitle, out.ProductTitle),
		"核心价值：" + firstNonEmpty(out.ProductInfo.CoreValue, out.Description),
		"卖点：" + strings.Join(out.ProductInfo.SellingPoints, " / "),
		"规格：" + strings.Join(out.ProductInfo.KeySpecs, " / "),
		"营销方向：" + strings.Join(out.MarketingCopy, " / "),
		"价格/促销：" + firstNonEmpty(out.PriceInfo.PriceText, out.PriceCopy, out.PriceInfo.PromotionText),
		"视觉风格：" + combineVisualDirection(prompt.ImagePrompt, style.StylePrompt),
		"分秒脚本：每一秒都必须同时交代镜头、灯光、背景、声音和细节描述。",
		"0-1 秒：镜头：商品主视觉开场，居中或三分构图，缓慢推近；灯光：柔和主光加边缘轮廓光；背景：干净电商场景或品牌色块；声音：轻柔开场音效或低音氛围铺底；细节描述：突出商品轮廓、材质、颜色和真实比例。",
		"1-2 秒：镜头：切到核心卖点近景或轻微环绕；灯光：增强材质高光和反射层次；背景：保持同一视觉世界并加入少量空间层次；声音：加入细腻转场声或材质反馈音；细节描述：展示关键结构、纹理、接口、包装或使用方式。",
		"2-3 秒：镜头：展示自然使用场景或功能演示；灯光：保持明亮可信，避免棚拍假感；背景：贴合目标平台和目标消费场景；声音：节奏轻微抬升；细节描述：用动作说明卖点如何解决需求，不编造额外功效。",
		"3-4 秒：镜头：切到细节特写和短促可读的信息展示；灯光：聚焦商品重点部位；背景：干净有层次，避免杂乱；声音：使用清晰提示音或轻节拍；细节描述：不得编造价格、规格、Logo、认证或品牌承诺。",
		"4-5 秒：镜头：形成电商海报式结束定帧，商品与核心利益点清晰；灯光：稳定高级，完成质感收束；背景：与前面镜头统一完整；声音：自然收束；细节描述：保持商品身份、比例、颜色、材质与需求一致。",
		"画面限制：不要错误文字、错别字、水印、二维码、低清噪点、夸张变形或侵权品牌元素。",
		"必须保持商品身份与需求一致：" + compactRequirement(requirement, 220),
		extraVideoPromptLineCN(extraPrompt),
	}), "\n")
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

func joinPromptLayers(layers ...string) string {
	out := make([]string, 0, len(layers))
	for _, layer := range layers {
		layer = strings.TrimSpace(layer)
		if layer != "" {
			out = append(out, layer)
		}
	}
	return strings.Join(out, "\n\n")
}

func extraVideoPromptLineCN(extraPrompt string) string {
	extraPrompt = strings.TrimSpace(extraPrompt)
	if extraPrompt == "" {
		return ""
	}
	return "本次重试额外要求：" + extraPrompt
}

func extraVideoPromptLineEN(extraPrompt string) string {
	extraPrompt = strings.TrimSpace(extraPrompt)
	if extraPrompt == "" {
		return ""
	}
	return "Extra retry requirement: " + extraPrompt
}

func subjectFidelityLayerCN(assetType string) string {
	lines := []string{
		"【1. 主体保真层】",
		"严格对照参考图主体，参考图中的商品是最高优先级。",
		"必须保留商品品类、轮廓、比例、材质、颜色、正面/侧面结构、按键、接口、屏幕、纹理、边缘细节和关键物品特征。",
		"商品本体上的品牌 Logo、商标、包装文字、标签、图案、封口、瓶盖、盒型和版面位置都属于商品特征，必须照参考图保留，不得抹掉或替换。",
		"不得把商品改成同类但不同外观的款式，不得改成普通款、传统款、不可收纳/不可折叠款或其它相似产品。",
	}
	if assetType != AssetWhite {
		lines = append(lines, "非白底图也必须沿用同一商品外观、比例和角度，不能为了海报效果重绘成另一款商品。")
	}
	return strings.Join(lines, "\n")
}

func subjectFidelityLayerEN(assetType string) string {
	lines := []string{
		"[1. Subject Fidelity Layer]",
		"Strictly follow the reference product subject; the product in the reference images has highest priority.",
		"Preserve product category, silhouette, proportions, material, color, front/side structure, buttons, ports, screen, texture, edge details and defining object features.",
		"Brand logos, trademarks, package text, labels, graphics, seals, caps, box shape and layout positions printed on the product itself are product features and must be preserved from the reference, not removed or replaced.",
		"Do not convert the product into a similar but different model, generic version, traditional version, non-foldable/non-portable version or another similar product.",
	}
	if assetType != AssetWhite {
		lines = append(lines, "Non-white assets must also keep the same product appearance, proportions and angle; do not redraw it as a different product for poster effects.")
	}
	return strings.Join(lines, "\n")
}

func currentImageTaskLayerCN(platform Platform, out Output, requirement, assetType, extraPrompt string, spec ImageSpec, langCode string) string {
	lines := []string{
		"【2. 当前图片任务层】",
		"平台：" + platform.Name,
		"图片类型：" + assetType,
		"目标：" + assetGoalCN(assetType),
	}
	if strings.TrimSpace(extraPrompt) != "" {
		lines = append(lines, "本次重试额外要求："+strings.TrimSpace(extraPrompt))
	}
	lines = append(lines,
		"原始需求："+requirement,
		"统一商品信息：\n"+formatCompactUnifiedInfoForAssetLanguage(out, assetType, langCode),
		"图片参数：尺寸 "+spec.Size+"，长宽比 "+spec.AspectRatio+"，清晰度 "+spec.Clarity,
		"构图要求："+assetCompositionCN(assetType),
	)
	if persona := publicFigureMoodCN(requirement); persona != "" && assetType != AssetWhite {
		lines = append(lines, "人物/代言氛围处理："+persona)
	}
	if assetType == AssetWhite {
		lines = append(lines, "本图允许出现的额外营销文字：无。白底图不得额外添加标题、卖点、价格、促销标签或图标；但商品包装/瓶身/盒身原有品牌 Logo、商标、标签和包装文字必须保留。")
	} else {
		lines = append(lines, "本图允许出现的文字：\n"+formatCompactImageTextPlanForLanguage(out.ImageTextPlans[assetType], assetType, langCode))
	}
	return strings.Join(nonEmptyLines(lines), "\n")
}

func currentImageTaskLayerEN(platform Platform, out Output, requirement, assetType, extraPrompt string, spec ImageSpec, langCode string) string {
	lines := []string{
		"[2. Current Image Task Layer]",
		"Platform: " + platform.Name,
		"Image type: " + assetType,
		"Goal: " + assetGoalEN(assetType),
	}
	if strings.TrimSpace(extraPrompt) != "" {
		lines = append(lines, "Retry extra requirement: "+strings.TrimSpace(extraPrompt))
	}
	lines = append(lines,
		"Original requirement: "+requirement,
		"Core product facts:\n"+formatCompactUnifiedInfoForAssetLanguage(out, assetType, langCode),
		"Image parameters: size "+spec.Size+", aspect ratio "+spec.AspectRatio+", clarity "+spec.Clarity,
		"Composition: "+assetCompositionEN(assetType),
	)
	if persona := publicFigureMoodEN(requirement); persona != "" && assetType != AssetWhite {
		lines = append(lines, "Person / spokesperson mood handling: "+persona)
	}
	if assetType == AssetWhite {
		lines = append(lines, "Extra marketing text pool: none. White-background image must not add title, selling points, price, promotion tags or icons; however brand logos, trademarks, labels and package text already printed on the product/package must be preserved.")
	} else {
		lines = append(lines, "Visible text pool for this image:\n"+formatCompactImageTextPlanForLanguage(out.ImageTextPlans[assetType], assetType, langCode))
	}
	return strings.Join(nonEmptyLines(lines), "\n")
}

func stylePromptLayerCN(prompt PromptTemplate, style StyleTemplate, assetType string) string {
	lines := []string{
		"【3. 风格提示词层】",
		"视觉方向：" + combineVisualDirection(prompt.ImagePrompt, style.StylePrompt),
		"风格规则：" + assetBackgroundRuleCN(assetType),
	}
	if assetType != AssetWhite {
		lines = append(lines,
			"资产差异规则："+assetSeriesRuleCN(assetType),
			"整组非白底图只需要保持商品身份、品牌调性和基础质感一致；主图、场景/详情图、价格图必须有不同构图、不同信息层级和不同使用场景。",
			"不得把其它资产简单裁切成同一张图；必须按当前图片类型重新设计背景、留白、文字区和视觉重点。",
			assetNonWhiteHardRuleCN(assetType),
		)
	}
	return strings.Join(nonEmptyLines(lines), "\n")
}

func stylePromptLayerEN(prompt PromptTemplate, style StyleTemplate, assetType string) string {
	lines := []string{
		"[3. Style Prompt Layer]",
		"Visual direction: " + combineVisualDirection(prompt.ImagePrompt, style.StylePrompt),
		"Style rule: " + assetBackgroundRuleEN(assetType),
	}
	if assetType != AssetWhite {
		lines = append(lines,
			"Asset differentiation rule: "+assetSeriesRuleEN(assetType),
			"Non-white assets only need to keep the same product identity, brand tone and basic material feel; main, scene/detail and price images must use different compositions, information hierarchy and usage context.",
			"Do not make the other assets look like simple crops of the same image; redesign background, whitespace, text zones and visual focus for the current asset type.",
			assetNonWhiteHardRuleEN(assetType),
		)
	}
	return strings.Join(nonEmptyLines(lines), "\n")
}

func negativePromptLayerCN(assetType, languageRule, creativeRequirement string) string {
	lines := []string{
		"【4. 负面提示词层】",
		"不得改变商品主体，不得生成不同产品、不同颜色、不同材质、不同结构、不同角度或不同配件。",
		"不得新增与商品无关的外部品牌、额外 Logo、名人脸、二维码、水印、无关贴纸、无关图标或虚假背书；但参考图商品本体/包装上已有的品牌 Logo、商标和标签必须保留。",
		"不得新增、替换、改写任何数字价格、折扣、标题、规格、型号。",
		"不得生成价格数字、容量数字、功率参数、折扣数字、型号或规格，除非用户明确提供。",
		"不得生成密集小字、乱码、英文混排、角落说明、过多参数块或不可读文字。",
		languageRule,
	}
	if assetType == AssetWhite {
		lines = append(lines, "白底图必须纯白或接近纯白背景；不得加入场景、道具、人物手部、额外标题、额外卖点文字、额外价格、额外促销标签、额外图标、贴纸、边框或水印。商品本体/包装上已有的品牌 Logo、商标、标签和包装文字必须保留。")
	}
	if strings.TrimSpace(creativeRequirement) != "" {
		lines = append(lines, "如原始需求含有人物、代言氛围、首页场景或品牌活动要求，仅可作为氛围方向处理，不得牺牲商品主体保真。")
		lines = append(lines, "如果原始需求明确提到特朗普、Trump、代言人、Logo、背书或站台，第 2 层的人物/代言视觉必须执行并突出；这些由用户明确要求的元素不属于负面项，不得被本层规则压掉。")
	}
	return strings.Join(nonEmptyLines(lines), "\n")
}

func negativePromptLayerEN(assetType, languageRule, creativeRequirement string) string {
	lines := []string{
		"[4. Negative Prompt Layer]",
		"Do not alter the product subject. Do not create a different product, color, material, structure, angle or accessory.",
		"Do not add external brands, extra logos, celebrity faces, QR codes, watermarks, irrelevant stickers, irrelevant icons or false endorsements unrelated to the product; brand logos, trademarks and labels already present on the reference product/package must be preserved.",
		"Do not add, replace or rewrite any numeric price, discount, title, spec or model.",
		"Do not invent price numbers, capacity numbers, wattage, discount numbers, model names or specs unless explicitly provided by the user.",
		"Do not create dense tiny copy, garbled text, mixed-language clutter, corner notes, excessive parameter blocks or unreadable text.",
		languageRule,
	}
	if assetType == AssetWhite {
		lines = append(lines, "White-background image must use a pure or near-white background; do not add scenes, props, hands, people, extra titles, extra selling-point text, extra prices, extra promotion tags, extra icons, stickers, borders or watermarks. Brand logos, trademarks, labels and package text already present on the product/package must be preserved.")
	}
	if strings.TrimSpace(creativeRequirement) != "" {
		lines = append(lines, "If the original requirement includes people, spokesperson mood, homepage scenes or campaign atmosphere, use it only as mood direction and never sacrifice product fidelity.")
		lines = append(lines, "If the original requirement explicitly mentions Trump, a spokesperson, logos, endorsement or stage support, the person/spokesperson visual in layer 2 must be followed and made prominent; these user-requested elements are not negative items and must not be suppressed by this layer.")
	}
	return strings.Join(nonEmptyLines(lines), "\n")
}

func publicFigureMoodCN(requirement string) string {
	req := strings.ToLower(strings.TrimSpace(requirement))
	if req == "" {
		return ""
	}
	if strings.Contains(requirement, "特朗普") || strings.Contains(req, "trump") {
		return "高优先级执行：非白底图必须出现醒目的人物代言/站台主视觉，以特朗普式竞选广告为核心；突出金发、深色西装、红领带、演讲台、挥手或指向商品、红白蓝竞选海报、聚光灯和强势表情。人物需要和商品同框，形成“特朗普式代言广告”视觉焦点。"
	}
	if strings.Contains(requirement, "代言") || strings.Contains(req, "endorsement") || strings.Contains(req, "spokesperson") {
		return "高优先级执行：非白底图必须突出人物代言/站台视觉，可以加入代言人、发布会、海报站台或品牌活动氛围。"
	}
	return ""
}

func publicFigureMoodEN(requirement string) string {
	req := strings.ToLower(strings.TrimSpace(requirement))
	if req == "" {
		return ""
	}
	if strings.Contains(requirement, "特朗普") || strings.Contains(req, "trump") {
		return "High priority: every non-white asset must include a prominent spokesperson/stage visual centered on a Trump-style campaign advertisement look; emphasize blond hair, dark suit, red tie, podium, waving or pointing toward the product, red-white-blue campaign-poster graphics, spotlights and a forceful expression. The person and product must appear in the same frame as the visual focus."
	}
	if strings.Contains(requirement, "代言") || strings.Contains(req, "endorsement") || strings.Contains(req, "spokesperson") {
		return "High priority: every non-white asset must make the spokesperson/stage visual prominent; use a spokesperson, launch-event setting, poster-stage pose or campaign mood."
	}
	return ""
}

func assetGoalCN(assetType string) string {
	switch assetType {
	case AssetTitle:
		return "店标题图，突出商品名称和核心价值"
	case AssetMain:
		return "电商主图，商品主体清晰，适合列表和首屏"
	case AssetWhite:
		return "商品白底图，只展示商品本体"
	case AssetDetail:
		return "详情页长图模块，展示卖点、场景和参数"
	case AssetPrice:
		return "价格促销图，突出优惠和行动号召"
	default:
		return "按当前图片类型生成电商素材"
	}
}

func assetGoalEN(assetType string) string {
	switch assetType {
	case AssetTitle:
		return "store title image that highlights the product name and core value"
	case AssetMain:
		return "main ecommerce image with a clear product hero, suitable for listings and above-the-fold display"
	case AssetWhite:
		return "white-background product image showing only the product itself"
	case AssetDetail:
		return "detail-page module image showing selling points, scenes and specs"
	case AssetPrice:
		return "price promotion image highlighting the offer and call to action"
	default:
		return "ecommerce asset for the current image type"
	}
}

func assetCompositionCN(assetType string) string {
	switch assetType {
	case AssetTitle:
		return "横版品牌海报，商品放在画面右侧或左侧三分之一，留出大标题和核心卖点区，必须有明确营销版式。"
	case AssetMain:
		return "方形电商主图，商品清晰但不要撑满画面，主体约占画面 45%-60%，保留足够留白和轻场景/渐变/色块背景；可以有少量标题或标签，但不能做成白底审核图，也不能裁得过大。"
	case AssetWhite:
		return "标准白底商品图，居中、完整、无文字，只保留商品本体。"
	case AssetDetail:
		return "竖版详情模块，必须使用满版家居/户外真实场景、有色设计底板或杂志式版面，商品融入场景；分区展示卖点、参数和局部特写，可使用信息卡片和图文排版。"
	case AssetPrice:
		return "竖版促销图，突出价格、优惠、CTA 和购买理由，使用强视觉层级、促销色块和海报式背景；商品作为辅助主视觉，不要占满整张图。"
	default:
		return "按当前图片类型设计独立构图，避免与其他资产重复。"
	}
}

func assetCompositionEN(assetType string) string {
	switch assetType {
	case AssetTitle:
		return "horizontal brand poster; place the product on the left or right third, reserve space for a large headline and key value, with a clear marketing layout."
	case AssetMain:
		return "square ecommerce main image; keep the product clear but not oversized, around 45%-60% of the canvas, with enough whitespace and a light lifestyle/gradient/color-block background; a small title or badge is allowed, but do not make it a white-background review image or an over-cropped product shot."
	case AssetWhite:
		return "standard white-background product image, centered, complete, no text, product only."
	case AssetDetail:
		return "vertical detail module on a full-bleed home/outdoor scene, colored design canvas or magazine-style layout; integrate the product into the scene; include sections for selling points, specs and close-ups with info cards and editorial layout."
	case AssetPrice:
		return "vertical promotion image emphasizing price, discount, CTA and purchase reasons with strong visual hierarchy, campaign color blocks and a poster background; keep the product as a supporting hero, not filling the whole canvas."
	default:
		return "design an independent composition for this asset type and avoid repeating other assets."
	}
}

func assetBackgroundRuleCN(assetType string) string {
	switch assetType {
	case AssetWhite:
		return "使用纯白或接近纯白背景。"
	case AssetDetail:
		return "本图必须输出详情页场景/编辑设计图，使用有色底板、真实场景或杂志式背景。"
	case AssetPrice:
		return "使用促销海报背景、色块或场景底图。"
	case AssetTitle:
		return "使用品牌海报背景或场景化背景。"
	case AssetMain:
		return "使用渐变、浅场景或平台安全背景，主体比例克制，保留留白。"
	default:
		return "按当前资产类型使用独立背景。"
	}
}

func assetBackgroundRuleEN(assetType string) string {
	switch assetType {
	case AssetWhite:
		return "Use a pure white or near-white background."
	case AssetDetail:
		return "Use a full-bleed scene, colored design canvas or editorial background."
	case AssetPrice:
		return "Use a promotional poster background, color blocks or a scene base."
	case AssetTitle:
		return "Use a brand poster background or lifestyle background."
	case AssetMain:
		return "Use a gradient, light lifestyle scene or platform-safe background, with restrained product scale and whitespace."
	default:
		return "Use an independent background for the current asset type."
	}
}

func assetNonWhiteHardRuleCN(assetType string) string {
	switch assetType {
	case AssetDetail:
		return "详情图必须是整版场景化/编辑化信息图，背景必须有场景、色块或版面层次；不得生成成与主图相同的单商品海报。"
	case AssetPrice:
		return "价格图必须是促销海报构图，价格与行动号召优先，背景必须有促销色块或场景层次；不得生成成与主图相同的单商品海报。"
	case AssetTitle:
		return "店标题图必须是横版品牌海报，保留标题区，背景必须有品牌视觉层次。"
	case AssetMain:
		return "电商主图必须是克制的主视觉海报或场景主图，商品主体约占画面 45%-60%，背景必须有渐变、色块或使用氛围，不能生成白底大商品特写。"
	default:
		return "当前资产必须保持独立版式，背景必须有明确视觉层次。"
	}
}

func assetNonWhiteHardRuleEN(assetType string) string {
	switch assetType {
	case AssetDetail:
		return "The detail image must be a scene-driven or editorial information layout with a visible scene, color blocks or layered page design; it must not look like the same single-product poster as the main image."
	case AssetPrice:
		return "The price image must read as a promotion poster with price and CTA priority, not the same single-product poster as the main image."
	case AssetTitle:
		return "The title image must read as a horizontal brand poster with a clear headline zone, not a catalog cutout."
	case AssetMain:
		return "The main image must read as a restrained hero poster or scene-led ecommerce visual; product scale should be around 45%-60% of the canvas with whitespace, gradient, color blocks or usage atmosphere, not an oversized white-background cutout."
	default:
		return "This asset must keep an independent composition and must not collapse into a catalog cutout."
	}
}

func assetSeriesRuleCN(assetType string) string {
	switch assetType {
	case AssetTitle:
		return "横版品牌海报，承担店铺首屏氛围和标题传达。"
	case AssetMain:
		return "方形主图，重点是商品识别和平台列表转化；主体不能过大，需保留留白和轻营销背景。"
	case AssetDetail:
		return "竖版详情图，重点是场景、卖点、参数和局部特写的信息组织。"
	case AssetPrice:
		return "竖版价格图，重点是价格/权益/行动号召，促销层级必须强于商品展示。"
	default:
		return "按当前资产类型做独立版式。"
	}
}

func assetSeriesRuleEN(assetType string) string {
	switch assetType {
	case AssetTitle:
		return "Horizontal brand poster for storefront atmosphere and headline delivery."
	case AssetMain:
		return "Square main image focused on product recognition and listing conversion; product must not be too large and needs whitespace plus a light marketing background."
	case AssetDetail:
		return "Vertical detail image focused on scene, selling points, specs and close-up information structure."
	case AssetPrice:
		return "Vertical price image focused on offer, benefits and CTA; promotion hierarchy must be stronger than product display."
	default:
		return "Use an independent layout for the current asset type."
	}
}

func referencesForAsset(assetType string, originalRefs, whiteRefs []imgpkg.ReferenceImage) []imgpkg.ReferenceImage {
	if assetType == AssetWhite || len(whiteRefs) == 0 {
		return originalRefs
	}
	return whiteRefs
}

func imageResultReferences(res *imgpkg.RunResult, fileName string) []imgpkg.ReferenceImage {
	if res == nil || res.Status != imgpkg.StatusSuccess || len(res.ImageBytes) == 0 {
		return nil
	}
	refs := make([]imgpkg.ReferenceImage, 0, len(res.ImageBytes))
	for i, data := range res.ImageBytes {
		if len(data) == 0 {
			continue
		}
		name := fileName
		if len(res.ImageBytes) > 1 {
			name = fmt.Sprintf("%s-%d.png", strings.TrimSuffix(fileName, filepath.Ext(fileName)), i+1)
		}
		refs = append(refs, imgpkg.ReferenceImage{Data: data, FileName: name})
	}
	return refs
}

func (r *Runner) whiteAssetReference(ctx context.Context, taskID string) []imgpkg.ReferenceImage {
	if r.dao == nil {
		return nil
	}
	assets, err := r.dao.ListAssets(ctx, taskID)
	if err != nil {
		logger.L().Warn("ecommerce list assets for white reference failed",
			zap.String("task_id", taskID), zap.Error(err))
		return nil
	}
	for _, asset := range assets {
		if asset.AssetType != AssetWhite || asset.Status != StatusSuccess || asset.ImageTaskID == "" {
			continue
		}
		refs, err := r.referenceFromImageTask(ctx, asset.ImageTaskID, AssetWhite+"-product-anchor.png")
		if err != nil {
			logger.L().Warn("ecommerce white asset reference failed",
				zap.String("task_id", taskID),
				zap.String("image_task_id", asset.ImageTaskID),
				zap.Error(err))
			return nil
		}
		return refs
	}
	return nil
}

func (r *Runner) videoReferences(ctx context.Context, taskID string, fallback []imgpkg.ReferenceImage) []imgpkg.ReferenceImage {
	if len(fallback) > 0 {
		return fallback
	}
	whiteRefs := r.whiteAssetReference(ctx, taskID)
	if len(whiteRefs) > 0 {
		return whiteRefs
	}
	logger.L().Warn("ecommerce video white anchor unavailable", zap.String("task_id", taskID))
	return nil
}

func videoReferenceImages(refs []imgpkg.ReferenceImage) ([]videogen.ImageInput, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]videogen.ImageInput, 0, len(refs))
	for i, ref := range refs {
		if len(ref.Data) == 0 {
			continue
		}
		name := videoProductAnchorName
		if len(refs) > 1 {
			name = fmt.Sprintf("%s_%d", videoProductAnchorName, i+1)
		}
		out = append(out, videogen.ImageInput{
			URL:  imageReferenceDataURL(ref),
			Name: name,
		})
	}
	return out, nil
}

func withVideoReferencePrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	line := "参考图片 @" + videoProductAnchorName + " 是商品白底图/主体锚点；视频必须以该图中的真实商品为唯一商品主体，保持外观、比例、颜色、材质、结构、Logo/标签和关键细节一致，不得替换成同类其它款。"
	if prompt == "" {
		return line
	}
	if strings.Contains(prompt, "@"+videoProductAnchorName) || strings.Contains(prompt, videoProductAnchorName) {
		return prompt
	}
	return line + "\n" + prompt
}

func imageReferenceDataURL(ref imgpkg.ReferenceImage) string {
	ct := http.DetectContentType(ref.Data)
	if !strings.HasPrefix(ct, "image/") {
		ct = contentTypeForFileName(ref.FileName)
	}
	if !strings.HasPrefix(ct, "image/") {
		ct = "image/png"
	}
	return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(ref.Data)
}

func contentTypeForFileName(name string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name)))
	if ext == "" {
		return ""
	}
	ct := mime.TypeByExtension(ext)
	if strings.HasPrefix(ct, "image/") {
		return ct
	}
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}

func (r *Runner) retryReferences(ctx context.Context, taskID string, asset Asset, fallback []imgpkg.ReferenceImage) []imgpkg.ReferenceImage {
	if asset.AssetType != AssetWhite {
		whiteRefs := r.whiteAssetReference(ctx, taskID)
		if len(whiteRefs) > 0 {
			return referencesForAsset(asset.AssetType, fallback, whiteRefs)
		}
		logger.L().Warn("ecommerce retry white anchor unavailable, fallback to original references",
			zap.String("task_id", taskID), zap.Uint64("asset_id", asset.ID))
		return referencesForAsset(asset.AssetType, fallback, nil)
	}
	if asset.Status == StatusSuccess && asset.ImageTaskID != "" {
		if refs, err := r.referenceFromImageTask(ctx, asset.ImageTaskID, asset.AssetType+"-retry-reference.png"); err == nil {
			return refs
		} else {
			logger.L().Warn("ecommerce retry current image reference failed",
				zap.String("task_id", taskID), zap.Uint64("asset_id", asset.ID), zap.Error(err))
		}
	}
	return referencesForAsset(asset.AssetType, fallback, nil)
}

func (r *Runner) referenceFromImageTask(ctx context.Context, taskID, fileName string) ([]imgpkg.ReferenceImage, error) {
	if r.imageDAO == nil {
		return nil, errors.New("image reference resolver not ready")
	}
	imgTask, err := r.imageDAO.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if imgTask.Status != imgpkg.StatusSuccess {
		return nil, fmt.Errorf("image task status %s", imgTask.Status)
	}
	if refs, err := referencesFromStoredResultURLs(imgTask.DecodeResultURLs(), fileName); err == nil && len(refs) > 0 {
		return refs, nil
	} else if err != nil {
		logger.L().Warn("ecommerce stored result URL reference failed",
			zap.String("image_task_id", taskID), zap.Error(err))
	}
	refs := imgTask.DecodeFileIDs()
	if r.imageAcc == nil {
		return nil, errors.New("image account resolver not ready")
	}
	if len(refs) == 0 || imgTask.ConversationID == "" || imgTask.AccountID == 0 {
		return nil, errors.New("image task missing download metadata")
	}
	at, deviceID, sessionID, cookies, err := r.imageAcc.AuthToken(ctx, imgTask.AccountID)
	if err != nil {
		return nil, err
	}
	cli, err := chatgpt.New(chatgpt.Options{
		AuthToken: at,
		DeviceID:  deviceID,
		SessionID: sessionID,
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

func referencesFromStoredResultURLs(urls []string, fileName string) ([]imgpkg.ReferenceImage, error) {
	if len(urls) == 0 {
		return nil, nil
	}
	data, name, err := fetchReferenceBytes(context.Background(), strings.TrimSpace(urls[0]))
	if err != nil {
		return nil, err
	}
	if len(data) > maxReferenceImageBytes {
		return nil, fmt.Errorf("参考图超过 %dMB", maxReferenceImageBytes/1024/1024)
	}
	if strings.TrimSpace(fileName) != "" {
		name = fileName
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
