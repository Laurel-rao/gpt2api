package settings

import (
	"context"
	"errors"
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

	"github.com/432539/gpt2api/internal/audit"
	"github.com/432539/gpt2api/internal/middleware"
	"github.com/432539/gpt2api/internal/videogen"
	"github.com/432539/gpt2api/pkg/mailer"
	"github.com/432539/gpt2api/pkg/resp"
)

// Handler 系统设置 HTTP 接口。
//   - List    GET  /api/admin/settings          管理员读取所有 key
//   - Update  PUT  /api/admin/settings          管理员批量更新
//   - Reload  POST /api/admin/settings/reload   从 DB 强制重载缓存(应急)
//   - TestMail POST /api/admin/settings/test-email 管理员给任意地址发一封测试邮件
//   - UploadSiteAsset POST /api/admin/settings/site-asset 上传 favicon/logo 到本地静态目录
//   - Public  GET  /api/public/site-info        匿名可访问,返回 Public=true 的子集
type Handler struct {
	svc               *Service
	mail              *mailer.Mailer
	auditDAO          *audit.DAO
	imageGenProbe     func(context.Context) (durationMs int64, imageCount int, err error)
	textGenProbe      func(context.Context) (durationMs int64, content string, err error)
	videoGenProbe     func(context.Context) (durationMs int64, modelCount int, modelName string, models []videogen.ProbeModel, err error)
	videoGenProbeCh   func(context.Context, videogen.Config) (durationMs int64, modelCount int, modelName string, models []videogen.ProbeModel, err error)
	videoGenBalance   func(context.Context) (*videogen.Balance, error)
	videoGenBalanceCh func(context.Context, videogen.Config) (*videogen.Balance, error)
	videoGenClient    *videogen.Client
	videoGenTasks     sync.Map
}

func NewHandler(svc *Service, mail *mailer.Mailer, adao *audit.DAO) *Handler {
	return &Handler{svc: svc, mail: mail, auditDAO: adao}
}

func (h *Handler) SetImageGenProbe(fn func(context.Context) (durationMs int64, imageCount int, err error)) {
	h.imageGenProbe = fn
}

func (h *Handler) SetTextGenProbe(fn func(context.Context) (durationMs int64, content string, err error)) {
	h.textGenProbe = fn
}

func (h *Handler) SetVideoGenProbe(fn func(context.Context) (durationMs int64, modelCount int, modelName string, models []videogen.ProbeModel, err error)) {
	h.videoGenProbe = fn
}

func (h *Handler) SetVideoGenProbeForConfig(fn func(context.Context, videogen.Config) (durationMs int64, modelCount int, modelName string, models []videogen.ProbeModel, err error)) {
	h.videoGenProbeCh = fn
}

func (h *Handler) SetVideoGenBalance(fn func(context.Context) (*videogen.Balance, error)) {
	h.videoGenBalance = fn
}

func (h *Handler) SetVideoGenBalanceForConfig(fn func(context.Context, videogen.Config) (*videogen.Balance, error)) {
	h.videoGenBalanceCh = fn
}

func (h *Handler) SetVideoGenClient(client *videogen.Client) {
	h.videoGenClient = client
}

// itemView 给前端使用的完整条目(带 schema,便于统一渲染)。
type itemView struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Type     string `json:"type"`
	Category string `json:"category"`
	Label    string `json:"label"`
	Desc     string `json:"desc"`
}

const maskedPasswordValue = "__MASKED__"

type videoGenGenerateTestState struct {
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
}

// List GET /api/admin/settings
func (h *Handler) List(c *gin.Context) {
	snap := h.svc.Snapshot()
	items := make([]itemView, 0, len(Defs))
	for _, d := range Defs {
		value := snap[d.Key]
		if d.Type == "password" {
			value = maskSecret(value)
		}
		items = append(items, itemView{
			Key: d.Key, Value: value, Type: d.Type,
			Category: d.Category, Label: d.Label, Desc: d.Desc,
		})
	}
	resp.OK(c, gin.H{"items": items})
}

// Update PUT /api/admin/settings
// body: { "items": { "site.name": "...", "auth.allow_register": "true", ... } }
type updateReq struct {
	Items map[string]string `json:"items"`
}

func (h *Handler) Update(c *gin.Context) {
	var req updateReq
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Items) == 0 {
		resp.BadRequest(c, "items required")
		return
	}
	// 白名单过滤 + 类型轻校验(严重错误直接拒,warning 放行由前端提示)
	for k, v := range req.Items {
		if !IsAllowedKey(k) {
			resp.BadRequest(c, "unknown key: "+k)
			return
		}
		if k == VideoGenWorkflowModels {
			normalized, err := NormalizeVideoGenWorkflowModels(v)
			if err != nil {
				resp.BadRequest(c, err.Error())
				return
			}
			req.Items[k] = normalized
			continue
		}
		if def, _ := DefByKey(k); def.Type == "password" {
			if v == maskedPasswordValue {
				delete(req.Items, k)
				continue
			}
		} else if def.Type == "int" {
			if v == "" {
				req.Items[k] = "0"
				continue
			}
			if _, err := parseInt64(v); err != nil {
				resp.BadRequest(c, k+" must be integer")
				return
			}
		} else if def.Type == "float" {
			if v == "" {
				req.Items[k] = "0"
				continue
			}
			if _, err := parseFloat64(v); err != nil {
				resp.BadRequest(c, k+" must be number")
				return
			}
		}
	}
	if err := h.svc.Set(c.Request.Context(), req.Items); err != nil {
		resp.Internal(c, err.Error())
		return
	}
	if h.auditDAO != nil {
		actor := middleware.UserID(c)
		if actor > 0 {
			_ = h.auditDAO.Insert(c.Request.Context(), &audit.Log{
				ActorID: actor,
				Action:  "settings.update",
				Method:  c.Request.Method,
				Path:    c.FullPath(),
				Target:  sprintKeys(req.Items),
				IP:      c.ClientIP(),
				UA:      c.Request.UserAgent(),
			})
		}
	}
	resp.OK(c, gin.H{"updated": len(req.Items)})
}

// Reload POST /api/admin/settings/reload
func (h *Handler) Reload(c *gin.Context) {
	if err := h.svc.Reload(c.Request.Context()); err != nil {
		resp.Internal(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"reloaded": true})
}

// TestMail POST /api/admin/settings/test-email
// body: { "to": "foo@bar.com" }
type testMailReq struct {
	To string `json:"to" binding:"required,email"`
}

func (h *Handler) TestMail(c *gin.Context) {
	var req testMailReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "invalid email: "+err.Error())
		return
	}
	if h.mail == nil || h.mail.Disabled() {
		resp.Fail(c, resp.CodeBadRequest, "SMTP not configured: fill host/user/pass in config and restart")
		return
	}
	subject := "[" + h.svc.SiteName() + "] SMTP test email"
	html := `<p>This is a <b>test email</b> sent from ` + h.svc.SiteName() + ` admin console.</p>` +
		`<p>If you see this, your SMTP configuration works.</p>`
	if err := h.mail.SendSync(mailer.Message{To: req.To, Subject: subject, HTML: html}); err != nil {
		resp.Fail(c, resp.CodeInternal, "send failed: "+err.Error())
		return
	}
	resp.OK(c, gin.H{"sent": true, "to": req.To})
}

// TestImageGen POST /api/admin/settings/test-imagegen
func (h *Handler) TestImageGen(c *gin.Context) {
	if h.imageGenProbe == nil {
		resp.Internal(c, "生图网关未初始化")
		return
	}
	if !h.svc.ImageGenEnabled() {
		resp.BadRequest(c, "生图网关未启用")
		return
	}
	if h.svc.ImageGenAPIKey() == "" {
		resp.BadRequest(c, "请先配置生图网关密钥")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(h.svc.ImageGenTimeoutSec())*time.Second)
	defer cancel()
	durationMs, imageCount, err := h.imageGenProbe(ctx)
	if err != nil {
		resp.Fail(c, resp.CodeUpstream, "探测失败:"+err.Error())
		return
	}
	resp.OK(c, gin.H{
		"ok":          true,
		"duration_ms": durationMs,
		"image_count": imageCount,
	})
}

// TestTextGen POST /api/admin/settings/test-textgen
func (h *Handler) TestTextGen(c *gin.Context) {
	if h.textGenProbe == nil {
		resp.Internal(c, "文本网关未初始化")
		return
	}
	if !h.svc.TextGenEnabled() {
		resp.BadRequest(c, "文本网关未启用")
		return
	}
	if h.svc.TextGenAPIKey() == "" {
		resp.BadRequest(c, "请先配置文本网关密钥")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(h.svc.TextGenTimeoutSec())*time.Second)
	defer cancel()
	durationMs, content, err := h.textGenProbe(ctx)
	if err != nil {
		resp.Fail(c, resp.CodeUpstream, "探测失败:"+err.Error())
		return
	}
	resp.OK(c, gin.H{
		"ok":          true,
		"duration_ms": durationMs,
		"content":     content,
	})
}

// TestVideoGen POST /api/admin/settings/test-videogen
func (h *Handler) TestVideoGen(c *gin.Context) {
	channelType := videoGenRequestChannel(c)
	if channelType != "" && h.videoGenProbeCh == nil {
		resp.Internal(c, "视频网关未初始化")
		return
	}
	if channelType == "" && h.videoGenProbe == nil {
		resp.Internal(c, "视频网关未初始化")
		return
	}
	if channelType == "" && !h.svc.VideoGenEnabled() {
		resp.BadRequest(c, "视频网关未启用")
		return
	}
	if channelType == "" {
		channelType = h.svc.VideoGenChannelType()
	}
	cfg := h.svc.VideoGenConfigForChannel(channelType)
	if strings.TrimSpace(cfg.APIKey) == "" {
		resp.BadRequest(c, "请先配置视频网关密钥")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(h.svc.VideoGenTimeoutSec())*time.Second)
	defer cancel()
	var (
		durationMs int64
		modelCount int
		modelName  string
		models     []videogen.ProbeModel
		err        error
	)
	if h.videoGenProbeCh != nil {
		durationMs, modelCount, modelName, models, err = h.videoGenProbeCh(ctx, cfg)
	} else {
		durationMs, modelCount, modelName, models, err = h.videoGenProbe(ctx)
	}
	if err != nil {
		resp.Fail(c, resp.CodeUpstream, "探测失败:"+err.Error())
		return
	}
	resp.OK(c, gin.H{
		"ok":          true,
		"duration_ms": durationMs,
		"model_count": modelCount,
		"model_name":  modelName,
		"models":      models,
	})
}

// VideoGenBalance GET /api/admin/settings/videogen-balance
func (h *Handler) VideoGenBalance(c *gin.Context) {
	channelType := videoGenRequestChannel(c)
	if channelType != "" && h.videoGenBalanceCh == nil {
		resp.Internal(c, "视频网关未初始化")
		return
	}
	if channelType == "" && h.videoGenBalance == nil {
		resp.Internal(c, "视频网关未初始化")
		return
	}
	if channelType == "" {
		channelType = h.svc.VideoGenChannelType()
	}
	cfg := h.svc.VideoGenConfigForChannel(channelType)
	if !isAPIYIUnsupportedBalanceChannel(cfg.ChannelType) && strings.TrimSpace(cfg.APIKey) == "" {
		resp.BadRequest(c, "请先配置视频网关密钥")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var (
		balance *videogen.Balance
		err     error
	)
	if h.videoGenBalanceCh != nil {
		balance, err = h.videoGenBalanceCh(ctx, cfg)
	} else {
		balance, err = h.videoGenBalance(ctx)
	}
	if err != nil {
		resp.Fail(c, resp.CodeUpstream, "余额获取失败:"+err.Error())
		return
	}
	resp.OK(c, balance)
}

// StartVideoGenGenerateTest POST /api/admin/settings/videogen-generate-test
func (h *Handler) StartVideoGenGenerateTest(c *gin.Context) {
	if h.videoGenClient == nil {
		resp.Internal(c, "视频网关未初始化")
		return
	}
	channelType := videoGenRequestChannel(c)
	if channelType == "" {
		channelType = h.svc.VideoGenChannelType()
	}
	cfg := h.svc.VideoGenConfigForChannel(channelType)
	if strings.TrimSpace(cfg.APIKey) == "" {
		resp.BadRequest(c, "请先配置视频网关密钥")
		return
	}
	prompt := strings.TrimSpace(c.PostForm("prompt"))
	if prompt == "" {
		resp.BadRequest(c, "请输入测试提示词")
		return
	}
	model := strings.TrimSpace(c.PostForm("model"))
	if model != "" {
		cfg.Model = model
	}
	imageURL := ""
	if fh, err := c.FormFile("image"); err == nil && fh != nil {
		if fh.Size <= 0 {
			resp.BadRequest(c, "测试图片为空")
			return
		}
		if fh.Size > 10*1024*1024 {
			resp.BadRequest(c, "测试图片过大: 最大 10MB")
			return
		}
		publicPath, err := saveVideoGenTestImage(fh)
		if err != nil {
			resp.Internal(c, "保存测试图片失败: "+err.Error())
			return
		}
		imageURL = absolutePublicURL(c, h.svc, publicPath)
	}
	videoURL := ""
	if fh, err := c.FormFile("video"); err == nil && fh != nil {
		if !videoGenSupportsReferenceVideo(cfg.ChannelType) {
			resp.BadRequest(c, "参考视频仅支持 API易 Seedance 2.0 / Wan2.7 / HappyHorse 渠道")
			return
		}
		if fh.Size <= 0 {
			resp.BadRequest(c, "测试视频为空")
			return
		}
		if fh.Size > 200*1024*1024 {
			resp.BadRequest(c, "测试视频过大: 最大 200MB")
			return
		}
		publicPath, err := saveVideoGenTestMedia(fh, "video")
		if err != nil {
			resp.Internal(c, "保存测试视频失败: "+err.Error())
			return
		}
		videoURL = absolutePublicURL(c, h.svc, publicPath)
	}

	id := "vtest_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
	now := time.Now()
	state := &videoGenGenerateTestState{
		ID:          id,
		ChannelType: cfg.ChannelType,
		Status:      "queued",
		Progress:    0,
		ImageURL:    imageURL,
		VideoURL:    videoURL,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	h.videoGenTasks.Store(id, state)

	go h.runVideoGenGenerateTest(id, cfg, prompt, imageURL, videoURL)
	resp.OK(c, state)
}

// GetVideoGenGenerateTest GET /api/admin/settings/videogen-generate-test/:id
func (h *Handler) GetVideoGenGenerateTest(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		resp.BadRequest(c, "id required")
		return
	}
	v, ok := h.videoGenTasks.Load(id)
	if !ok {
		resp.BadRequest(c, "测试任务不存在或已过期")
		return
	}
	state, ok := v.(*videoGenGenerateTestState)
	if !ok || state == nil {
		resp.BadRequest(c, "测试任务状态异常")
		return
	}
	resp.OK(c, state)
}

func (h *Handler) runVideoGenGenerateTest(id string, cfg videogen.Config, prompt string, imageURL string, videoURL string) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSec)*time.Second)
	defer cancel()
	opt := videogen.Options{
		Prompt: prompt,
		OnProgress: func(result videogen.Result) {
			h.updateVideoGenTestState(id, func(state *videoGenGenerateTestState) {
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
		opt.Images = []videogen.ImageInput{{URL: imageURL, Name: "admin_test_reference"}}
	}
	if videoURL != "" {
		opt.ReferenceVideoURL = videoURL
	}
	result, err := h.videoGenClient.GenerateForConfig(ctx, cfg, opt)
	if err != nil {
		h.updateVideoGenTestState(id, func(state *videoGenGenerateTestState) {
			state.Status = "failed"
			state.Progress = 100
			state.ProgressKnown = true
			state.Error = err.Error()
			state.DurationMs = time.Since(start).Milliseconds()
		})
		return
	}
	h.updateVideoGenTestState(id, func(state *videoGenGenerateTestState) {
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
	})
}

func (h *Handler) updateVideoGenTestState(id string, mutate func(*videoGenGenerateTestState)) {
	v, ok := h.videoGenTasks.Load(id)
	if !ok {
		return
	}
	state, ok := v.(*videoGenGenerateTestState)
	if !ok || state == nil {
		return
	}
	copyState := *state
	mutate(&copyState)
	copyState.Progress = clampProgress(copyState.Progress)
	copyState.UpdatedAt = time.Now()
	h.videoGenTasks.Store(id, &copyState)
}

func videoGenRequestChannel(c *gin.Context) string {
	v := strings.TrimSpace(c.Query("channel_type"))
	if v == "" {
		v = strings.TrimSpace(c.PostForm("channel_type"))
	}
	if v == "" {
		var req struct {
			ChannelType string `json:"channel_type"`
		}
		_ = c.ShouldBindJSON(&req)
		v = strings.TrimSpace(req.ChannelType)
	}
	switch strings.ToLower(v) {
	case videogen.ChannelEchoon, videogen.ChannelAPIYISeedance, videogen.ChannelAPIYIWan27, videogen.ChannelAPIYIHappyHorse:
		return strings.ToLower(v)
	default:
		return ""
	}
}

func isAPIYIUnsupportedBalanceChannel(channelType string) bool {
	switch strings.ToLower(strings.TrimSpace(channelType)) {
	case videogen.ChannelAPIYISeedance, videogen.ChannelAPIYIWan27, videogen.ChannelAPIYIHappyHorse:
		return true
	default:
		return false
	}
}

func videoGenSupportsReferenceVideo(channelType string) bool {
	switch strings.ToLower(strings.TrimSpace(channelType)) {
	case videogen.ChannelAPIYISeedance, videogen.ChannelAPIYIWan27, videogen.ChannelAPIYIHappyHorse:
		return true
	default:
		return false
	}
}

func saveVideoGenTestImage(fh *multipart.FileHeader) (string, error) {
	return saveVideoGenTestMedia(fh, "image")
}

func saveVideoGenTestMedia(fh *multipart.FileHeader, kind string) (string, error) {
	src, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer src.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(src, head)
	head = head[:n]
	contentType := http.DetectContentType(head)
	ext, ok := videoGenTestMediaExt(contentType, fh.Filename, kind)
	if !ok {
		return "", errors.New("unsupported file type")
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	dir := SiteAssetDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	filename := "videogen-test-" + kind + "-" + time.Now().Format("20060102150405") + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8] + ext
	dstPath := filepath.Join(dir, filename)
	dst, err := os.Create(dstPath)
	if err != nil {
		return "", err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return "", err
	}
	return "/site-assets/" + filename, nil
}

func videoGenTestMediaExt(contentType, filename, kind string) (string, bool) {
	if kind == "video" {
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
		return "", false
	}
	switch contentType {
	case "image/webp":
		return ".webp", true
	}
	return assetExt(contentType, filename)
}

func absolutePublicURL(c *gin.Context, svc *Service, publicPath string) string {
	base := ""
	if svc != nil {
		base = strings.TrimRight(strings.TrimSpace(svc.GetString(SiteAPIBaseURL)), "/")
	}
	var req *http.Request
	if c != nil {
		req = c.Request
	}
	return PublicURLFromRequest(req, base, publicPath)
}

func clampProgress(progress int) int {
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}

// UploadSiteAsset POST /api/admin/settings/site-asset
func (h *Handler) UploadSiteAsset(c *gin.Context) {
	key := strings.TrimSpace(c.PostForm("key"))
	if key != SiteFaviconURL && key != SiteLogoURL {
		resp.BadRequest(c, "key must be site.favicon_url or site.logo_url")
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		resp.BadRequest(c, "file required")
		return
	}
	if fh.Size <= 0 {
		resp.BadRequest(c, "empty file")
		return
	}
	if fh.Size > 2*1024*1024 {
		resp.BadRequest(c, "file too large: max 2MB")
		return
	}
	src, err := fh.Open()
	if err != nil {
		resp.Internal(c, "open upload failed: "+err.Error())
		return
	}
	defer src.Close()

	head := make([]byte, 512)
	n, _ := io.ReadFull(src, head)
	head = head[:n]
	contentType := http.DetectContentType(head)
	ext, ok := assetExt(contentType, fh.Filename)
	if !ok {
		resp.BadRequest(c, "unsupported file type")
		return
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		resp.Internal(c, "rewind upload failed: "+err.Error())
		return
	}

	dir := SiteAssetDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		resp.Internal(c, "create asset dir failed: "+err.Error())
		return
	}
	filename := strings.TrimPrefix(key, "site.") + "-" + time.Now().Format("20060102150405") + ext
	dstPath := filepath.Join(dir, filename)
	dst, err := os.Create(dstPath)
	if err != nil {
		resp.Internal(c, "create file failed: "+err.Error())
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		resp.Internal(c, "save file failed: "+err.Error())
		return
	}

	publicPath := "/site-assets/" + filename
	if err := h.svc.Set(c.Request.Context(), map[string]string{key: publicPath}); err != nil {
		resp.Internal(c, err.Error())
		return
	}
	if h.auditDAO != nil {
		actor := middleware.UserID(c)
		if actor > 0 {
			_ = h.auditDAO.Insert(c.Request.Context(), &audit.Log{
				ActorID: actor,
				Action:  "settings.site_asset_upload",
				Method:  c.Request.Method,
				Path:    c.FullPath(),
				Target:  key + "=" + publicPath,
				IP:      c.ClientIP(),
				UA:      c.Request.UserAgent(),
			})
		}
	}
	resp.OK(c, gin.H{"key": key, "url": publicPath})
}

// Public GET /api/public/site-info
func (h *Handler) Public(c *gin.Context) {
	resp.OK(c, h.svc.PublicSnapshot())
}

func assetExt(contentType, filename string) (string, bool) {
	switch contentType {
	case "image/x-icon", "image/vnd.microsoft.icon":
		return ".ico", true
	case "image/png":
		return ".png", true
	case "image/jpeg":
		return ".jpg", true
	case "image/svg+xml", "text/xml":
		return ".svg", true
	}
	switch ext := strings.ToLower(filepath.Ext(filename)); ext {
	case ".ico", ".png", ".jpg", ".svg":
		return ext, true
	case ".jpeg":
		return ".jpg", true
	}
	return "", false
}

func maskSecret(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return maskedPasswordValue
	}
	return s[:4] + "****" + s[len(s)-4:]
}
