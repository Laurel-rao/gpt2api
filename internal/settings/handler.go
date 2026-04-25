package settings

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/432539/gpt2api/internal/audit"
	"github.com/432539/gpt2api/internal/middleware"
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
	svc      *Service
	mail     *mailer.Mailer
	auditDAO *audit.DAO
}

func NewHandler(svc *Service, mail *mailer.Mailer, adao *audit.DAO) *Handler {
	return &Handler{svc: svc, mail: mail, auditDAO: adao}
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

// List GET /api/admin/settings
func (h *Handler) List(c *gin.Context) {
	snap := h.svc.Snapshot()
	items := make([]itemView, 0, len(Defs))
	for _, d := range Defs {
		items = append(items, itemView{
			Key: d.Key, Value: snap[d.Key], Type: d.Type,
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
		if def, _ := DefByKey(k); def.Type == "int" {
			if v == "" {
				req.Items[k] = "0"
				continue
			}
			if _, err := parseInt64(v); err != nil {
				resp.BadRequest(c, k+" must be integer")
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
