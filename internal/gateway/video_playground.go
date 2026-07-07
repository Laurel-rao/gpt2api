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
		{"type": videogen.ChannelEchoon, "name": "Echoon / AI Gen Platform", "enabled": current == videogen.ChannelEchoon},
		{"type": videogen.ChannelAPIYISeedance, "name": "API易 Seedance 2.0", "enabled": current == videogen.ChannelAPIYISeedance},
		{"type": videogen.ChannelAPIYIWan27, "name": "API易 Wan2.7", "enabled": current == videogen.ChannelAPIYIWan27},
		{"type": videogen.ChannelAPIYIHappyHorse, "name": "API易 HappyHorse", "enabled": current == videogen.ChannelAPIYIHappyHorse},
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

	imageUpload, err := h.saveOptionalUpload(c, "image", "image")
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	videoUpload, err := h.saveOptionalUpload(c, "video", "video")
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	imageURL := imageUpload.PublicURL
	imagePayloadURL := videoPlaygroundImagePayloadURL(cfg.ChannelType, imageUpload)
	videoURL := videoUpload.PublicURL
	if videoURL != "" && !videoPlaygroundSupportsReferenceVideo(cfg.ChannelType) {
		resp.BadRequest(c, "参考视频仅支持 API易 Seedance 2.0 / Wan2.7 / HappyHorse 渠道")
		return
	}

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
		ImageURL:     imageURL,
		VideoURL:     videoURL,
		CreatedAt:    now,
		UpdatedAt:    now,
		ExpectedCost: expectedCost,
	}
	h.tasks.Store(id, state)
	go h.run(id, uid, ak.ID, cfg, prompt, imagePayloadURL, videoURL, expectedCost)
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
		resp.BadRequest(c, "视频生成任务不存在或已过期")
		return
	}
	state, ok := v.(*videoPlaygroundState)
	if !ok || state == nil {
		resp.BadRequest(c, "视频生成任务状态异常")
		return
	}
	resp.OK(c, state)
}

func (h *VideoPlaygroundHandler) run(id string, userID uint64, keyID uint64, cfg videogen.Config, prompt, imageURL, videoURL string, expectedCost int64) {
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
	if imageURL != "" {
		opt.Images = []videogen.ImageInput{{URL: imageURL, Name: "playground_reference"}}
	}
	if videoURL != "" {
		opt.ReferenceVideoURL = videoURL
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

func (h *VideoPlaygroundHandler) saveOptionalUpload(c *gin.Context, field string, kind string) (videoPlaygroundUpload, error) {
	fh, err := c.FormFile(field)
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
			return videoPlaygroundUpload{}, nil
		}
		return videoPlaygroundUpload{}, err
	}
	if fh == nil {
		return videoPlaygroundUpload{}, nil
	}
	limit := int64(10 * 1024 * 1024)
	if kind == "video" {
		limit = 200 * 1024 * 1024
	}
	if fh.Size <= 0 {
		return videoPlaygroundUpload{}, fmt.Errorf("%s 文件为空", uploadKindText(kind))
	}
	if fh.Size > limit {
		return videoPlaygroundUpload{}, fmt.Errorf("%s 文件过大: 最大 %dMB", uploadKindText(kind), limit/1024/1024)
	}
	publicPath, dataURL, err := saveVideoPlaygroundUpload(fh, kind)
	if err != nil {
		return videoPlaygroundUpload{}, err
	}
	return videoPlaygroundUpload{
		PublicURL: videoPlaygroundAbsoluteURL(c, h.settings, publicPath),
		DataURL:   dataURL,
	}, nil
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
	switch normalizeVideoPlaygroundChannel(channelType) {
	case videogen.ChannelAPIYISeedance, videogen.ChannelAPIYIWan27, videogen.ChannelAPIYIHappyHorse:
		return true
	default:
		return false
	}
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
