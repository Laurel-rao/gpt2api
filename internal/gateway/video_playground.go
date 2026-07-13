package gateway

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/432539/gpt2api/internal/apikey"
	"github.com/432539/gpt2api/internal/billing"
	"github.com/432539/gpt2api/internal/middleware"
	"github.com/432539/gpt2api/internal/settings"
	"github.com/432539/gpt2api/internal/videogen"
	"github.com/432539/gpt2api/pkg/logger"
	"github.com/432539/gpt2api/pkg/resp"
)

type videoPlaygroundUpload struct {
	PublicURL string
	DataURL   string
}

type videoPlaygroundChannelLimits struct {
	MaxReferenceImages               int   `json:"max_reference_images"`
	MaxReferenceVideos               int   `json:"max_reference_videos"`
	MaxReferenceMedia                int   `json:"max_reference_media,omitempty"`
	MaxImageBytes                    int64 `json:"max_image_bytes"`
	MaxVideoBytes                    int64 `json:"max_video_bytes"`
	SupportsReferenceVideo           bool  `json:"supports_reference_video"`
	MinDurationSec                   int   `json:"min_duration_sec"`
	MaxDurationSec                   int   `json:"max_duration_sec"`
	MaxDurationWithReferenceVideoSec int   `json:"max_duration_with_reference_video_sec,omitempty"`
}

type VideoGenSettings interface {
	VideoGenEnabled() bool
	VideoGenChannelType() string
	VideoGenConfigForChannel(channelType string) videogen.Config
	VideoGenBillingRatio() float64
	GetString(key string) string
}

type VideoPlaygroundHandler struct {
	videoGen *videogen.Client
	billing  *billing.Engine
	settings VideoGenSettings
	tasks    sync.Map
}

type videoPlaygroundState struct {
	ID            string              `json:"id"`
	ChannelType   string              `json:"channel_type"`
	Status        string              `json:"status"`
	Progress      int                 `json:"progress"`
	ProgressKnown bool                `json:"progress_known"`
	TaskID        string              `json:"task_id,omitempty"`
	ModelID       string              `json:"model_id,omitempty"`
	ImageURL      string              `json:"image_url,omitempty"`
	VideoURL      string              `json:"video_url,omitempty"`
	ImageURLs     []string            `json:"image_urls,omitempty"`
	VideoURLs     []string            `json:"video_urls,omitempty"`
	ResultURL     string              `json:"result_url,omitempty"`
	Error         string              `json:"error,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
	DurationMs    int64               `json:"duration_ms,omitempty"`
	CostDetail    videogen.CostDetail `json:"cost_detail,omitempty"`
	CreditCost    int64               `json:"credit_cost,omitempty"`
	ExpectedCost  int64               `json:"expected_cost,omitempty"`
}

func NewVideoPlaygroundHandler(videoGen *videogen.Client, bill *billing.Engine, settingsSvc VideoGenSettings) *VideoPlaygroundHandler {
	return &VideoPlaygroundHandler{videoGen: videoGen, billing: bill, settings: settingsSvc}
}

// Channels GET /api/me/playground/video/channels
func (h *VideoPlaygroundHandler) Channels(c *gin.Context) {
	if h == nil || h.settings == nil {
		resp.Internal(c, "视频网关未初始化")
		return
	}
	current := h.settings.VideoGenChannelType()
	rows := []gin.H{
		{"type": videogen.ChannelEchoon, "name": "Echoon / AI Gen Platform", "enabled": current == videogen.ChannelEchoon, "limits": videoPlaygroundChannelLimitsFor(videogen.ChannelEchoon)},
		{"type": videogen.ChannelAPIYISeedance, "name": "API易 Seedance 2.0", "enabled": current == videogen.ChannelAPIYISeedance, "limits": videoPlaygroundChannelLimitsFor(videogen.ChannelAPIYISeedance)},
		{"type": videogen.ChannelAPIYIWan27, "name": "API易 Wan2.7", "enabled": current == videogen.ChannelAPIYIWan27, "limits": videoPlaygroundChannelLimitsFor(videogen.ChannelAPIYIWan27)},
		{"type": videogen.ChannelAPIYIHappyHorse, "name": "API易 HappyHorse", "enabled": current == videogen.ChannelAPIYIHappyHorse, "limits": videoPlaygroundChannelLimitsFor(videogen.ChannelAPIYIHappyHorse)},
	}
	resp.OK(c, gin.H{"items": rows, "default_channel_type": current})
}

// Start POST /api/me/playground/video
func (h *VideoPlaygroundHandler) Start(c *gin.Context) {
	if h == nil || h.videoGen == nil || h.settings == nil {
		resp.Internal(c, "视频网关未初始化")
		return
	}
	if !h.settings.VideoGenEnabled() {
		resp.BadRequest(c, "视频网关未启用")
		return
	}
	ak, ok := apikey.FromCtx(c)
	if !ok {
		resp.Unauthorized(c, "缺少 API Key")
		return
	}
	uid := middleware.UserID(c)
	if uid == 0 {
		resp.Unauthorized(c, "not logged in")
		return
	}
	channelType := normalizeVideoPlaygroundChannel(c.PostForm("channel_type"))
	if channelType == "" {
		channelType = h.settings.VideoGenChannelType()
	}
	cfg := h.settings.VideoGenConfigForChannel(channelType)
	if strings.TrimSpace(cfg.APIKey) == "" {
		resp.BadRequest(c, "请先配置视频网关密钥")
		return
	}
	prompt := strings.TrimSpace(c.PostForm("prompt"))
	if prompt == "" {
		resp.BadRequest(c, "请输入视频提示词")
		return
	}
	if model := strings.TrimSpace(c.PostForm("model")); model != "" {
		cfg.Model = model
	}
	applyVideoPlaygroundOptions(c, &cfg)
	limits := videoPlaygroundChannelLimitsForConfig(cfg)

	imageUploads, err := h.saveOptionalUploads(c, []string{"image", "image[]", "images"}, "image", limits.MaxImageBytes)
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	videoUploads, err := h.saveOptionalUploads(c, []string{"video", "video[]", "videos"}, "video", limits.MaxVideoBytes)
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	if err := validateVideoPlaygroundUploads(cfg, limits, len(imageUploads), len(videoUploads)); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	imageURLs := videoPlaygroundPublicURLs(imageUploads)
	imagePayloadURLs := videoPlaygroundImagePayloadURLs(cfg.ChannelType, imageUploads)
	videoURLs := videoPlaygroundPublicURLs(videoUploads)

	expectedCost := videoPlaygroundCost(h.settings)
	if h.billing == nil {
		resp.Internal(c, "视频计费未初始化")
		return
	}
	id := "vplay_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
	refID := videoPlaygroundBillingRef(id)
	if err := h.billing.PreDeduct(c.Request.Context(), uid, ak.ID, expectedCost, refID, "videogen playground prepay"); err != nil {
		if errors.Is(err, billing.ErrInsufficient) {
			resp.PaymentRequired(c, "积分不足，请前往「账单与充值」充值后再试")
			return
		}
		resp.Internal(c, "视频计费预扣失败: "+err.Error())
		return
	}

	now := time.Now()
	state := &videoPlaygroundState{
		ID:           id,
		ChannelType:  cfg.ChannelType,
		Status:       "queued",
		Progress:     0,
		ImageURL:     firstNonEmpty(imageURLs...),
		VideoURL:     firstNonEmpty(videoURLs...),
		ImageURLs:    imageURLs,
		VideoURLs:    videoURLs,
		CreatedAt:    now,
		UpdatedAt:    now,
		ExpectedCost: expectedCost,
	}
	h.tasks.Store(id, state)
	go h.run(id, uid, ak.ID, cfg, prompt, imagePayloadURLs, videoURLs, expectedCost)
	resp.OK(c, state)
}

// Get GET /api/me/playground/video/:id
func (h *VideoPlaygroundHandler) Get(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		resp.BadRequest(c, "id required")
		return
	}
	v, ok := h.tasks.Load(id)
	if !ok {
		h.getRecovered(c, id)
		return
	}
	state, ok := v.(*videoPlaygroundState)
	if !ok || state == nil {
		resp.BadRequest(c, "视频生成任务状态异常")
		return
	}
	resp.OK(c, state)
}

func (h *VideoPlaygroundHandler) getRecovered(c *gin.Context, id string) {
	if h == nil || h.videoGen == nil || h.settings == nil {
		resp.Internal(c, "视频网关未初始化")
		return
	}
	taskID := strings.TrimSpace(c.Query("task_id"))
	if taskID == "" {
		resp.BadRequest(c, "视频生成任务不存在或已过期")
		return
	}
	channelType := normalizeVideoPlaygroundChannel(c.Query("channel_type"))
	if channelType == "" {
		resp.BadRequest(c, "缺少视频任务渠道，无法恢复查询")
		return
	}
	cfg := h.settings.VideoGenConfigForChannel(channelType)
	if strings.TrimSpace(cfg.APIKey) == "" {
		resp.BadRequest(c, "请先配置视频网关密钥")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	result, err := h.videoGen.GetTaskForConfig(ctx, cfg, taskID)
	if err != nil {
		resp.Fail(c, resp.CodeUpstream, "恢复查询视频任务失败: "+err.Error())
		return
	}
	now := time.Now()
	state := &videoPlaygroundState{
		ID:            id,
		ChannelType:   cfg.ChannelType,
		Status:        strings.TrimSpace(result.Status),
		Progress:      clampPercent(result.Progress),
		ProgressKnown: result.ProgressKnown,
		TaskID:        firstNonEmpty(result.TaskID, taskID),
		ModelID:       strings.TrimSpace(result.ModelID),
		ResultURL:     strings.TrimSpace(result.ResultURL),
		Error:         strings.TrimSpace(result.ErrorMessage),
		CreatedAt:     now,
		UpdatedAt:     now,
		DurationMs:    result.DurationMs,
		CostDetail:    result.CostDetail,
	}
	if state.Status == "" {
		state.Status = "running"
	}
	terminal := strings.EqualFold(state.Status, "completed") || strings.EqualFold(state.Status, "failed")
	if terminal {
		state.Progress = 100
		state.ProgressKnown = true
	}
	if terminal {
		h.tasks.Store(id, state)
	}
	resp.OK(c, state)
}

func (h *VideoPlaygroundHandler) run(id string, userID uint64, keyID uint64, cfg videogen.Config, prompt string, imageURLs, videoURLs []string, expectedCost int64) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSec)*time.Second)
	defer cancel()
	opt := videogen.Options{
		Prompt: prompt,
		OnProgress: func(result videogen.Result) {
			h.update(id, func(state *videoPlaygroundState) {
				state.Status = strings.TrimSpace(result.Status)
				if state.Status == "" {
					state.Status = "running"
				}
				if result.ProgressKnown || !state.ProgressKnown {
					state.Progress = result.Progress
				}
				if result.ProgressKnown {
					state.ProgressKnown = true
				}
				state.TaskID = firstNonEmpty(result.TaskID, state.TaskID)
				state.ModelID = firstNonEmpty(result.ModelID, state.ModelID)
			})
		},
	}
	if strings.TrimSpace(cfg.Model) != "" {
		opt.Model = strings.TrimSpace(cfg.Model)
	}
	for i, imageURL := range imageURLs {
		imageURL = strings.TrimSpace(imageURL)
		if imageURL == "" {
			continue
		}
		opt.Images = append(opt.Images, videogen.ImageInput{
			URL:  imageURL,
			Name: fmt.Sprintf("playground_reference_%d", i+1),
		})
	}
	opt.ReferenceVideoURLs = append(opt.ReferenceVideoURLs, videoURLs...)
	if len(opt.ReferenceVideoURLs) > 0 {
		opt.ReferenceVideoURL = opt.ReferenceVideoURLs[0]
	}

	result, err := h.videoGen.GenerateForConfig(ctx, cfg, opt)
	if err != nil {
		_ = h.billing.Refund(context.Background(), userID, keyID, expectedCost, videoPlaygroundBillingRef(id), "videogen playground refund")
		h.update(id, func(state *videoPlaygroundState) {
			state.Status = "failed"
			state.Progress = 100
			state.ProgressKnown = true
			state.Error = err.Error()
			state.DurationMs = time.Since(start).Milliseconds()
		})
		return
	}
	actualCost := expectedCost
	if err := h.billing.Settle(context.Background(), userID, keyID, expectedCost, actualCost, videoPlaygroundBillingRef(id), videoPlaygroundBillingRemark(result)); err != nil {
		logger.L().Error("videogen playground billing settle",
			zap.Error(err),
			zap.String("task_id", id),
			zap.Uint64("user_id", userID))
	}
	h.update(id, func(state *videoPlaygroundState) {
		state.Status = result.Status
		if state.Status == "" {
			state.Status = "completed"
		}
		if result.ProgressKnown || !state.ProgressKnown {
			state.Progress = result.Progress
		}
		if result.ProgressKnown {
			state.ProgressKnown = true
		}
		if strings.EqualFold(state.Status, "completed") {
			state.Progress = 100
			state.ProgressKnown = true
		}
		state.TaskID = firstNonEmpty(result.TaskID, state.TaskID)
		state.ModelID = firstNonEmpty(result.ModelID, state.ModelID)
		state.ResultURL = strings.TrimSpace(result.ResultURL)
		state.Error = strings.TrimSpace(result.ErrorMessage)
		state.DurationMs = result.DurationMs
		if state.DurationMs <= 0 {
			state.DurationMs = time.Since(start).Milliseconds()
		}
		state.CostDetail = result.CostDetail
		state.CreditCost = actualCost
	})
}

func (h *VideoPlaygroundHandler) update(id string, mutate func(*videoPlaygroundState)) {
	v, ok := h.tasks.Load(id)
	if !ok {
		return
	}
	state, ok := v.(*videoPlaygroundState)
	if !ok || state == nil {
		return
	}
	copyState := *state
	mutate(&copyState)
	copyState.Progress = clampPercent(copyState.Progress)
	copyState.UpdatedAt = time.Now()
	h.tasks.Store(id, &copyState)
}

func (h *VideoPlaygroundHandler) saveOptionalUploads(c *gin.Context, fields []string, kind string, limit int64) ([]videoPlaygroundUpload, error) {
	if c == nil || c.Request == nil {
		return nil, nil
	}
	if err := c.Request.ParseMultipartForm(64 << 20); err != nil {
		if errors.Is(err, http.ErrNotMultipart) || errors.Is(err, http.ErrMissingBoundary) {
			return nil, nil
		}
		return nil, err
	}
	if c.Request.MultipartForm == nil {
		return nil, nil
	}
	var uploads []videoPlaygroundUpload
	for _, field := range fields {
		for _, fh := range c.Request.MultipartForm.File[field] {
			if fh == nil {
				continue
			}
			if fh.Size <= 0 {
				return nil, fmt.Errorf("%s 文件为空", uploadKindText(kind))
			}
			if limit > 0 && fh.Size > limit {
				return nil, fmt.Errorf("%s 文件过大: 最大 %dMB", uploadKindText(kind), limit/1024/1024)
			}
			publicPath, dataURL, err := saveVideoPlaygroundUpload(fh, kind)
			if err != nil {
				return nil, err
			}
			uploads = append(uploads, videoPlaygroundUpload{
				PublicURL: videoPlaygroundAbsoluteURL(c, h.settings, publicPath),
				DataURL:   dataURL,
			})
		}
	}
	return uploads, nil
}

func saveVideoPlaygroundUpload(fh *multipart.FileHeader, kind string) (string, string, error) {
	src, err := fh.Open()
	if err != nil {
		return "", "", err
	}
	defer src.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(src, head)
	head = head[:n]
	contentType := http.DetectContentType(head)
	ext, ok := videoPlaygroundExt(contentType, fh.Filename, kind)
	if !ok {
		return "", "", fmt.Errorf("不支持的%s文件类型", uploadKindText(kind))
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return "", "", err
	}
	dir := settings.SiteAssetDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	filename := "videogen-play-" + kind + "-" + time.Now().Format("20060102150405") + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8] + ext
	dstPath := filepath.Join(dir, filename)
	dst, err := os.Create(dstPath)
	if err != nil {
		return "", "", err
	}
	defer dst.Close()
	dataURL := ""
	if kind == "image" {
		data, err := io.ReadAll(src)
		if err != nil {
			return "", "", err
		}
		if n, err := dst.Write(data); err != nil {
			return "", "", err
		} else if n != len(data) {
			return "", "", io.ErrShortWrite
		}
		dataURL = videoPlaygroundImageDataURL(contentType, fh.Filename, data)
	} else if _, err := io.Copy(dst, src); err != nil {
		return "", "", err
	}
	return "/site-assets/" + filename, dataURL, nil
}

func videoPlaygroundImagePayloadURL(channelType string, upload videoPlaygroundUpload) string {
	switch normalizeVideoPlaygroundChannel(channelType) {
	case videogen.ChannelAPIYISeedance, videogen.ChannelAPIYIWan27, videogen.ChannelAPIYIHappyHorse:
		return firstNonEmpty(upload.DataURL, upload.PublicURL)
	default:
		return firstNonEmpty(upload.PublicURL, upload.DataURL)
	}
}

func videoPlaygroundImagePayloadURLs(channelType string, uploads []videoPlaygroundUpload) []string {
	out := make([]string, 0, len(uploads))
	for _, upload := range uploads {
		if url := videoPlaygroundImagePayloadURL(channelType, upload); url != "" {
			out = append(out, url)
		}
	}
	return out
}

func videoPlaygroundPublicURLs(uploads []videoPlaygroundUpload) []string {
	out := make([]string, 0, len(uploads))
	for _, upload := range uploads {
		if url := strings.TrimSpace(upload.PublicURL); url != "" {
			out = append(out, url)
		}
	}
	return out
}

func videoPlaygroundImageDataURL(contentType, filename string, data []byte) string {
	contentType = videoPlaygroundImageContentType(contentType, filename)
	if contentType == "" {
		contentType = "image/png"
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func videoPlaygroundImageContentType(contentType, filename string) string {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(contentType, "image/") {
		return contentType
	}
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(filename))) {
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

func videoPlaygroundExt(contentType, filename, kind string) (string, bool) {
	switch kind {
	case "video":
		switch contentType {
		case "video/mp4":
			return ".mp4", true
		case "video/quicktime":
			return ".mov", true
		case "video/webm":
			return ".webm", true
		}
		switch strings.ToLower(filepath.Ext(filename)) {
		case ".mp4", ".mov", ".webm":
			return strings.ToLower(filepath.Ext(filename)), true
		}
	default:
		switch contentType {
		case "image/png":
			return ".png", true
		case "image/jpeg":
			return ".jpg", true
		case "image/webp":
			return ".webp", true
		}
		switch strings.ToLower(filepath.Ext(filename)) {
		case ".png", ".jpg", ".jpeg", ".webp":
			if strings.EqualFold(filepath.Ext(filename), ".jpeg") {
				return ".jpg", true
			}
			return strings.ToLower(filepath.Ext(filename)), true
		}
	}
	return "", false
}

func videoPlaygroundAbsoluteURL(c *gin.Context, svc VideoGenSettings, publicPath string) string {
	base := ""
	if svc != nil {
		base = strings.TrimRight(strings.TrimSpace(svc.GetString(settings.SiteAPIBaseURL)), "/")
	}
	var req *http.Request
	if c != nil {
		req = c.Request
	}
	return settings.PublicURLFromRequest(req, base, publicPath)
}

func applyVideoPlaygroundOptions(c *gin.Context, cfg *videogen.Config) {
	if cfg == nil {
		return
	}
	if v := strings.TrimSpace(c.PostForm("ratio")); v != "" {
		cfg.AspectRatio = v
	}
	if v := strings.TrimSpace(c.PostForm("resolution")); v != "" {
		cfg.Resolution = v
	}
	if v := strings.TrimSpace(c.PostForm("duration")); v != "" {
		if n, err := parsePositiveInt(v); err == nil {
			cfg.DurationSec = n
		}
	}
}

func parsePositiveInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid positive int")
	}
	return n, nil
}

func normalizeVideoPlaygroundChannel(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case videogen.ChannelEchoon:
		return videogen.ChannelEchoon
	case videogen.ChannelAPIYISeedance:
		return videogen.ChannelAPIYISeedance
	case videogen.ChannelAPIYIWan27:
		return videogen.ChannelAPIYIWan27
	case videogen.ChannelAPIYIHappyHorse:
		return videogen.ChannelAPIYIHappyHorse
	default:
		return ""
	}
}

func videoPlaygroundSupportsReferenceVideo(channelType string) bool {
	return videoPlaygroundChannelLimitsFor(channelType).SupportsReferenceVideo
}

func videoPlaygroundChannelLimitsFor(channelType string) videoPlaygroundChannelLimits {
	switch normalizeVideoPlaygroundChannel(channelType) {
	case videogen.ChannelAPIYISeedance:
		return videoPlaygroundChannelLimits{
			MaxReferenceImages:     9,
			MaxReferenceVideos:     3,
			MaxImageBytes:          30 * 1024 * 1024,
			MaxVideoBytes:          200 * 1024 * 1024,
			SupportsReferenceVideo: true,
			MinDurationSec:         4,
			MaxDurationSec:         15,
		}
	case videogen.ChannelAPIYIWan27:
		return videoPlaygroundChannelLimits{
			MaxReferenceImages:               5,
			MaxReferenceVideos:               5,
			MaxReferenceMedia:                5,
			MaxImageBytes:                    30 * 1024 * 1024,
			MaxVideoBytes:                    200 * 1024 * 1024,
			SupportsReferenceVideo:           true,
			MinDurationSec:                   3,
			MaxDurationSec:                   15,
			MaxDurationWithReferenceVideoSec: 10,
		}
	case videogen.ChannelAPIYIHappyHorse:
		return videoPlaygroundChannelLimits{
			MaxReferenceImages:     9,
			MaxReferenceVideos:     0,
			MaxImageBytes:          30 * 1024 * 1024,
			MaxVideoBytes:          0,
			SupportsReferenceVideo: false,
			MinDurationSec:         3,
			MaxDurationSec:         10,
		}
	default:
		return videoPlaygroundChannelLimits{
			MaxReferenceImages:     1,
			MaxReferenceVideos:     0,
			MaxImageBytes:          10 * 1024 * 1024,
			MaxVideoBytes:          0,
			SupportsReferenceVideo: false,
			MinDurationSec:         3,
			MaxDurationSec:         10,
		}
	}
}

func videoPlaygroundChannelLimitsForConfig(cfg videogen.Config) videoPlaygroundChannelLimits {
	limits := videoPlaygroundChannelLimitsFor(cfg.ChannelType)
	if strings.EqualFold(strings.TrimSpace(cfg.Model), "wan2.7-i2v") {
		limits.MaxReferenceImages = 1
		limits.MaxReferenceVideos = 0
		limits.MaxReferenceMedia = 1
		limits.SupportsReferenceVideo = false
	}
	return limits
}

func validateVideoPlaygroundUploads(cfg videogen.Config, limits videoPlaygroundChannelLimits, imageCount, videoCount int) error {
	if imageCount > limits.MaxReferenceImages {
		return fmt.Errorf("参考图片最多支持 %d 张", limits.MaxReferenceImages)
	}
	if videoCount > 0 && !limits.SupportsReferenceVideo {
		return fmt.Errorf("当前渠道不支持参考视频")
	}
	if videoCount > limits.MaxReferenceVideos {
		return fmt.Errorf("参考视频最多支持 %d 段", limits.MaxReferenceVideos)
	}
	if limits.MaxReferenceMedia > 0 && imageCount+videoCount > limits.MaxReferenceMedia {
		return fmt.Errorf("参考图片和参考视频合计最多支持 %d 个", limits.MaxReferenceMedia)
	}
	if limits.MinDurationSec > 0 && cfg.DurationSec < limits.MinDurationSec {
		return fmt.Errorf("输出时长最短 %d 秒", limits.MinDurationSec)
	}
	maxDuration := limits.MaxDurationSec
	if videoCount > 0 && limits.MaxDurationWithReferenceVideoSec > 0 {
		maxDuration = limits.MaxDurationWithReferenceVideoSec
	}
	if maxDuration > 0 && cfg.DurationSec > maxDuration {
		return fmt.Errorf("输出时长最长 %d 秒", maxDuration)
	}
	return nil
}

func videoPlaygroundCost(svc VideoGenSettings) int64 {
	ratio := 10.0
	if svc != nil {
		ratio = svc.VideoGenBillingRatio()
	}
	if ratio <= 0 {
		ratio = 10
	}
	cost := int64(ratio * 10000)
	if cost <= 0 {
		return 100000
	}
	return cost
}

func videoPlaygroundBillingRef(id string) string {
	return truncateForBilling("videogen:playground:" + strings.TrimSpace(id))
}

func videoPlaygroundBillingRemark(res *videogen.Result) string {
	if res == nil {
		return "videogen playground settle"
	}
	modelName := strings.TrimSpace(res.CostDetail.ModelName)
	if modelName == "" {
		modelName = strings.TrimSpace(res.ModelID)
	}
	return truncateForBilling(fmt.Sprintf("videogen playground settle model=%s price=%.4f", modelName, res.CostDetail.Price))
}

func truncateForBilling(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 64 {
		return s
	}
	return s[:64]
}

func uploadKindText(kind string) string {
	if kind == "video" {
		return "参考视频"
	}
	return "参考图片"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func clampPercent(progress int) int {
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}
