package ecommerce

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/432539/gpt2api/internal/audit"
	imgpkg "github.com/432539/gpt2api/internal/image"
	"github.com/432539/gpt2api/internal/middleware"
	"github.com/432539/gpt2api/pkg/resp"
)

type Handler struct {
	dao      *DAO
	runner   *Runner
	auditDAO *audit.DAO
}

func NewHandler(dao *DAO, runner *Runner, auditDAO *audit.DAO) *Handler {
	return &Handler{dao: dao, runner: runner, auditDAO: auditDAO}
}

type platformReq struct {
	Code        string          `json:"code"`
	Name        string          `json:"name"`
	Language    string          `json:"language"`
	FieldSchema json.RawMessage `json:"field_schema"`
	Remark      string          `json:"remark"`
	Enabled     *bool           `json:"enabled"`
}

type promptReq struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	ContentPrompt string `json:"content_prompt"`
	ImagePrompt   string `json:"image_prompt"`
	Remark        string `json:"remark"`
	Enabled       *bool  `json:"enabled"`
}

type styleReq struct {
	Code         string          `json:"code"`
	Name         string          `json:"name"`
	StylePrompt  string          `json:"style_prompt"`
	LayoutConfig json.RawMessage `json:"layout_config"`
	Remark       string          `json:"remark"`
	Enabled      *bool           `json:"enabled"`
}

type createTaskReq struct {
	PlatformID       uint64   `json:"platform_id"`
	PromptTemplateID uint64   `json:"prompt_template_id"`
	StyleTemplateID  uint64   `json:"style_template_id"`
	Requirement      string   `json:"requirement"`
	ReferenceImages  []string `json:"reference_images"`
}

func (h *Handler) Options(c *gin.Context) {
	ctx := c.Request.Context()
	platforms, err := h.dao.ListPlatforms(ctx, ListFilter{EnabledOnly: true})
	if err != nil {
		resp.Internal(c, err.Error())
		return
	}
	prompts, err := h.dao.ListPromptTemplates(ctx, ListFilter{EnabledOnly: true})
	if err != nil {
		resp.Internal(c, err.Error())
		return
	}
	styles, err := h.dao.ListStyleTemplates(ctx, ListFilter{EnabledOnly: true})
	if err != nil {
		resp.Internal(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"platforms": platforms, "prompt_templates": prompts, "style_templates": styles})
}

func (h *Handler) CreateTask(c *gin.Context) {
	uid := middleware.UserID(c)
	if uid == 0 {
		resp.Unauthorized(c, "not logged in")
		return
	}
	var req createTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	req.Requirement = strings.TrimSpace(req.Requirement)
	if req.PlatformID == 0 || req.PromptTemplateID == 0 || req.StyleTemplateID == 0 {
		resp.BadRequest(c, "平台、提示词模板、风格模板必选")
		return
	}
	if req.Requirement == "" {
		resp.BadRequest(c, "商品文字需求不能为空")
		return
	}
	if len(req.ReferenceImages) > maxReferenceImages {
		resp.BadRequest(c, "最多上传 4 张参考图")
		return
	}
	if _, err := h.dao.GetPlatform(c.Request.Context(), req.PlatformID); err != nil {
		resp.BadRequest(c, "平台不存在")
		return
	}
	if _, err := h.dao.GetPromptTemplate(c.Request.Context(), req.PromptTemplateID); err != nil {
		resp.BadRequest(c, "提示词模板不存在")
		return
	}
	if _, err := h.dao.GetStyleTemplate(c.Request.Context(), req.StyleTemplateID); err != nil {
		resp.BadRequest(c, "风格模板不存在")
		return
	}
	refBytes, _ := json.Marshal(req.ReferenceImages)
	t := &Task{
		TaskID:           NewTaskID(),
		UserID:           uid,
		PlatformID:       req.PlatformID,
		PromptTemplateID: req.PromptTemplateID,
		StyleTemplateID:  req.StyleTemplateID,
		Requirement:      req.Requirement,
		ReferenceImages:  RawJSON(refBytes),
		Status:           StatusQueued,
		Progress:         0,
	}
	if err := h.dao.CreateTask(c.Request.Context(), t); err != nil {
		resp.Internal(c, err.Error())
		return
	}
	h.runner.Enqueue(t.TaskID)
	row, _ := h.taskView(c.Request.Context(), t.TaskID, false)
	resp.OK(c, row)
}

func (h *Handler) ListTasks(c *gin.Context) {
	uid := middleware.UserID(c)
	if uid == 0 {
		resp.Unauthorized(c, "not logged in")
		return
	}
	limit := queryInt(c, "limit", 10)
	offset := queryInt(c, "offset", 0)
	if limit > 100 {
		limit = 100
	}
	rows, total, err := h.dao.ListTasksByUser(c.Request.Context(), uid, ListFilter{
		Keyword: strings.TrimSpace(c.Query("keyword")),
		Status:  strings.TrimSpace(c.Query("status")),
	}, limit, offset)
	if err != nil {
		resp.Internal(c, err.Error())
		return
	}
	items := make([]gin.H, 0, len(rows))
	for i := range rows {
		v, _ := h.taskViewFromRow(c.Request.Context(), &rows[i], false)
		items = append(items, v)
	}
	resp.OK(c, gin.H{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) GetTask(c *gin.Context) {
	uid := middleware.UserID(c)
	if uid == 0 {
		resp.Unauthorized(c, "not logged in")
		return
	}
	row, err := h.dao.GetTask(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			resp.NotFound(c, "任务不存在")
			return
		}
		resp.Internal(c, err.Error())
		return
	}
	if row.UserID != uid {
		resp.NotFound(c, "任务不存在")
		return
	}
	v, err := h.taskViewFromRow(c.Request.Context(), row, true)
	if err != nil {
		resp.Internal(c, err.Error())
		return
	}
	resp.OK(c, v)
}

func (h *Handler) RetryAsset(c *gin.Context) {
	uid := middleware.UserID(c)
	if uid == 0 {
		resp.Unauthorized(c, "not logged in")
		return
	}
	taskID := c.Param("id")
	assetID, err := strconv.ParseUint(c.Param("asset_id"), 10, 64)
	if err != nil || assetID == 0 {
		resp.BadRequest(c, "invalid asset id")
		return
	}
	row, err := h.dao.GetTask(c.Request.Context(), taskID)
	if err != nil {
		writeErr(c, err)
		return
	}
	if row.UserID != uid {
		resp.NotFound(c, "任务不存在")
		return
	}
	asset, err := h.dao.GetAsset(c.Request.Context(), assetID)
	if err != nil {
		writeErr(c, err)
		return
	}
	if asset.TaskID != taskID {
		resp.NotFound(c, "图片资产不存在")
		return
	}
	if asset.Status == StatusRunning || asset.Status == StatusQueued {
		resp.BadRequest(c, "图片正在生成中")
		return
	}
	h.runner.EnqueueAssetRetry(taskID, assetID)
	resp.OK(c, gin.H{"task_id": taskID, "asset_id": assetID, "status": StatusQueued})
}

func (h *Handler) CancelTask(c *gin.Context) {
	uid := middleware.UserID(c)
	if uid == 0 {
		resp.Unauthorized(c, "not logged in")
		return
	}
	taskID := c.Param("id")
	row, err := h.dao.GetTask(c.Request.Context(), taskID)
	if err != nil {
		writeErr(c, err)
		return
	}
	if row.UserID != uid {
		resp.NotFound(c, "任务不存在")
		return
	}
	if err := h.runner.CancelTask(c.Request.Context(), taskID); err != nil {
		if strings.Contains(err.Error(), "不能中断") {
			resp.BadRequest(c, err.Error())
			return
		}
		writeErr(c, err)
		return
	}
	v, _ := h.taskView(c.Request.Context(), taskID, true)
	resp.OK(c, v)
}

func (h *Handler) taskView(ctx context.Context, taskID string, includeRefs bool) (gin.H, error) {
	row, err := h.dao.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return h.taskViewFromRow(ctx, row, includeRefs)
}

func (h *Handler) taskViewFromRow(ctx context.Context, row *TaskRow, includeRefs bool) (gin.H, error) {
	assets, err := h.dao.ListAssets(ctx, row.TaskID)
	if err != nil {
		return nil, err
	}
	refreshAssetProxyURLs(assets)
	outputHTML := row.OutputHTML
	if out := outputFromTask(row); out.ProductTitle != "" {
		outputHTML = buildHTML(out, assets)
	}
	refCount := 0
	if len(row.ReferenceImages) > 0 {
		var refs []string
		_ = json.Unmarshal(row.ReferenceImages.RawMessage(), &refs)
		refCount = len(refs)
	}
	out := gin.H{
		"id":                    row.ID,
		"task_id":               row.TaskID,
		"user_id":               row.UserID,
		"platform_id":           row.PlatformID,
		"platform_name":         row.PlatformName,
		"prompt_template_id":    row.PromptTemplateID,
		"prompt_name":           row.PromptName,
		"style_template_id":     row.StyleTemplateID,
		"style_name":            row.StyleName,
		"requirement":           row.Requirement,
		"reference_image_count": refCount,
		"status":                row.Status,
		"progress":              row.Progress,
		"output_json":           row.OutputJSON,
		"output_html":           outputHTML,
		"assets":                assets,
		"error":                 row.Error,
		"created_at":            row.CreatedAt,
		"started_at":            row.StartedAt,
		"finished_at":           row.FinishedAt,
	}
	if includeRefs {
		out["reference_images"] = row.ReferenceImages
	}
	return out, nil
}

func refreshAssetProxyURLs(assets []Asset) {
	for i := range assets {
		if assets[i].Status != StatusSuccess || assets[i].ImageTaskID == "" {
			assets[i].URL = ""
			continue
		}
		if assets[i].FileID == "" && assets[i].URL == "" {
			assets[i].URL = ""
			continue
		}
		assets[i].URL = imgpkg.BuildProxyURL(assets[i].ImageTaskID, 0, 0)
	}
}

func (h *Handler) AdminListPlatforms(c *gin.Context) {
	rows, err := h.dao.ListPlatforms(c.Request.Context(), ListFilter{Keyword: strings.TrimSpace(c.Query("keyword"))})
	writeList(c, rows, err)
}

func (h *Handler) AdminCreatePlatform(c *gin.Context) {
	var req platformReq
	if !bindAdmin(c, &req) {
		return
	}
	if err := validateJSON(req.FieldSchema, "field_schema"); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	enabled := boolDefault(req.Enabled, true)
	row := &Platform{Code: cleanCode(req.Code), Name: strings.TrimSpace(req.Name), Language: cleanLanguage(req.Language, req.FieldSchema), FieldSchema: RawJSON(req.FieldSchema), Remark: req.Remark, Enabled: enabled}
	if row.Code == "" || row.Name == "" {
		resp.BadRequest(c, "code 和 name 必填")
		return
	}
	if err := h.dao.CreatePlatform(c.Request.Context(), row); err != nil {
		resp.Internal(c, err.Error())
		return
	}
	audit.Record(c, h.auditDAO, "ecommerce.platform.create", strconv.FormatUint(row.ID, 10), gin.H{"code": row.Code})
	resp.OK(c, row)
}

func (h *Handler) AdminUpdatePlatform(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req platformReq
	if !bindAdmin(c, &req) {
		return
	}
	if err := validateJSON(req.FieldSchema, "field_schema"); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	row := &Platform{ID: id, Code: cleanCode(req.Code), Name: strings.TrimSpace(req.Name), Language: cleanLanguage(req.Language, req.FieldSchema), FieldSchema: RawJSON(req.FieldSchema), Remark: req.Remark, Enabled: boolDefault(req.Enabled, true)}
	if row.Code == "" || row.Name == "" {
		resp.BadRequest(c, "code 和 name 必填")
		return
	}
	if err := h.dao.UpdatePlatform(c.Request.Context(), row); err != nil {
		writeErr(c, err)
		return
	}
	audit.Record(c, h.auditDAO, "ecommerce.platform.update", strconv.FormatUint(id, 10), gin.H{"code": row.Code})
	resp.OK(c, row)
}

func (h *Handler) AdminDeletePlatform(c *gin.Context) {
	h.deleteConfig(c, h.dao.DeletePlatform, "ecommerce.platform.delete")
}

func (h *Handler) AdminListPrompts(c *gin.Context) {
	rows, err := h.dao.ListPromptTemplates(c.Request.Context(), ListFilter{Keyword: strings.TrimSpace(c.Query("keyword"))})
	writeList(c, rows, err)
}

func (h *Handler) AdminCreatePrompt(c *gin.Context) {
	var req promptReq
	if !bindAdmin(c, &req) {
		return
	}
	row := &PromptTemplate{Code: cleanCode(req.Code), Name: strings.TrimSpace(req.Name), ContentPrompt: strings.TrimSpace(req.ContentPrompt), ImagePrompt: strings.TrimSpace(req.ImagePrompt), Remark: req.Remark, Enabled: boolDefault(req.Enabled, true)}
	if row.Code == "" || row.Name == "" || row.ContentPrompt == "" || row.ImagePrompt == "" {
		resp.BadRequest(c, "code、name、content_prompt、image_prompt 必填")
		return
	}
	if err := h.dao.CreatePromptTemplate(c.Request.Context(), row); err != nil {
		resp.Internal(c, err.Error())
		return
	}
	audit.Record(c, h.auditDAO, "ecommerce.prompt.create", strconv.FormatUint(row.ID, 10), gin.H{"code": row.Code})
	resp.OK(c, row)
}

func (h *Handler) AdminUpdatePrompt(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req promptReq
	if !bindAdmin(c, &req) {
		return
	}
	row := &PromptTemplate{ID: id, Code: cleanCode(req.Code), Name: strings.TrimSpace(req.Name), ContentPrompt: strings.TrimSpace(req.ContentPrompt), ImagePrompt: strings.TrimSpace(req.ImagePrompt), Remark: req.Remark, Enabled: boolDefault(req.Enabled, true)}
	if row.Code == "" || row.Name == "" || row.ContentPrompt == "" || row.ImagePrompt == "" {
		resp.BadRequest(c, "code、name、content_prompt、image_prompt 必填")
		return
	}
	if err := h.dao.UpdatePromptTemplate(c.Request.Context(), row); err != nil {
		writeErr(c, err)
		return
	}
	audit.Record(c, h.auditDAO, "ecommerce.prompt.update", strconv.FormatUint(id, 10), gin.H{"code": row.Code})
	resp.OK(c, row)
}

func (h *Handler) AdminDeletePrompt(c *gin.Context) {
	h.deleteConfig(c, h.dao.DeletePromptTemplate, "ecommerce.prompt.delete")
}

func (h *Handler) AdminListStyles(c *gin.Context) {
	rows, err := h.dao.ListStyleTemplates(c.Request.Context(), ListFilter{Keyword: strings.TrimSpace(c.Query("keyword"))})
	writeList(c, rows, err)
}

func (h *Handler) AdminCreateStyle(c *gin.Context) {
	var req styleReq
	if !bindAdmin(c, &req) {
		return
	}
	if err := validateJSON(req.LayoutConfig, "layout_config"); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	row := &StyleTemplate{Code: cleanCode(req.Code), Name: strings.TrimSpace(req.Name), StylePrompt: strings.TrimSpace(req.StylePrompt), LayoutConfig: RawJSON(req.LayoutConfig), Remark: req.Remark, Enabled: boolDefault(req.Enabled, true)}
	if row.Code == "" || row.Name == "" || row.StylePrompt == "" {
		resp.BadRequest(c, "code、name、style_prompt 必填")
		return
	}
	if err := h.dao.CreateStyleTemplate(c.Request.Context(), row); err != nil {
		resp.Internal(c, err.Error())
		return
	}
	audit.Record(c, h.auditDAO, "ecommerce.style.create", strconv.FormatUint(row.ID, 10), gin.H{"code": row.Code})
	resp.OK(c, row)
}

func (h *Handler) AdminUpdateStyle(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req styleReq
	if !bindAdmin(c, &req) {
		return
	}
	if err := validateJSON(req.LayoutConfig, "layout_config"); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	row := &StyleTemplate{ID: id, Code: cleanCode(req.Code), Name: strings.TrimSpace(req.Name), StylePrompt: strings.TrimSpace(req.StylePrompt), LayoutConfig: RawJSON(req.LayoutConfig), Remark: req.Remark, Enabled: boolDefault(req.Enabled, true)}
	if row.Code == "" || row.Name == "" || row.StylePrompt == "" {
		resp.BadRequest(c, "code、name、style_prompt 必填")
		return
	}
	if err := h.dao.UpdateStyleTemplate(c.Request.Context(), row); err != nil {
		writeErr(c, err)
		return
	}
	audit.Record(c, h.auditDAO, "ecommerce.style.update", strconv.FormatUint(id, 10), gin.H{"code": row.Code})
	resp.OK(c, row)
}

func (h *Handler) AdminDeleteStyle(c *gin.Context) {
	h.deleteConfig(c, h.dao.DeleteStyleTemplate, "ecommerce.style.delete")
}

func (h *Handler) deleteConfig(c *gin.Context, fn func(context.Context, uint64) error, action string) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := fn(c.Request.Context(), id); err != nil {
		writeErr(c, err)
		return
	}
	audit.Record(c, h.auditDAO, action, strconv.FormatUint(id, 10), nil)
	resp.OK(c, gin.H{"deleted": id})
}

func writeList[T any](c *gin.Context, rows []T, err error) {
	if err != nil {
		resp.Internal(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"items": rows, "total": len(rows)})
}

func writeErr(c *gin.Context, err error) {
	if errors.Is(err, ErrNotFound) {
		resp.NotFound(c, "记录不存在")
		return
	}
	resp.Internal(c, err.Error())
}

func bindAdmin(c *gin.Context, dst interface{}) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		resp.BadRequest(c, err.Error())
		return false
	}
	return true
}

func parseID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		resp.BadRequest(c, "invalid id")
		return 0, false
	}
	return id, true
}

func boolDefault(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}

func cleanCode(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

func cleanLanguage(s string, fieldSchema json.RawMessage) string {
	s = strings.TrimSpace(s)
	if s == "" {
		var cfg struct {
			Locale string `json:"locale"`
		}
		_ = json.Unmarshal(fieldSchema, &cfg)
		s = strings.TrimSpace(cfg.Locale)
	}
	switch strings.ToLower(s) {
	case "en", "en-us", "english":
		return "en-US"
	case "zh", "zh-cn", "cn", "chinese", "中文":
		return "zh-CN"
	default:
		if s == "" {
			return "zh-CN"
		}
		return s
	}
}

func queryInt(c *gin.Context, key string, fallback int) int {
	n, err := strconv.Atoi(c.DefaultQuery(key, strconv.Itoa(fallback)))
	if err != nil || n < 0 {
		return fallback
	}
	return n
}
