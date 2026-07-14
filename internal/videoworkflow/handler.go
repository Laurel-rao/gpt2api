package videoworkflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/432539/gpt2api/internal/middleware"
	"github.com/432539/gpt2api/internal/videogen"
	"github.com/432539/gpt2api/pkg/resp"
)

type Handler struct {
	service        *Service
	workflowModels func() []videogen.WorkflowModel
}

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) SetWorkflowModels(fn func() []videogen.WorkflowModel) {
	h.workflowModels = fn
}

func (h *Handler) ListTemplates(c *gin.Context) {
	items, err := h.service.ListTemplates(c.Request.Context())
	if err != nil {
		resp.Internal(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"items": items})
}

func (h *Handler) ListWorkflowModels(c *gin.Context) {
	items := videogen.DefaultWorkflowModels()
	if h.workflowModels != nil {
		if configured := h.workflowModels(); len(configured) > 0 {
			items = configured
		}
	}
	resp.OK(c, gin.H{"items": items})
}

func (h *Handler) CreateWorkflow(c *gin.Context) {
	var request struct {
		TemplateID uint64 `json:"template_id" binding:"required"`
		Name       string `json:"name"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		abortBadRequest(c, err.Error())
		return
	}
	workflow, err := h.service.CreateWorkflow(c.Request.Context(), CreateWorkflowInput{
		UserID: middleware.UserID(c), TemplateID: request.TemplateID, Name: request.Name,
	})
	if err != nil {
		handleError(c, err)
		return
	}
	resp.OK(c, workflow)
}

func (h *Handler) ListWorkflows(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	items, total, err := h.service.ListWorkflows(c.Request.Context(), middleware.UserID(c), limit, offset)
	if err != nil {
		resp.Internal(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) GetWorkflow(c *gin.Context) {
	workflow, err := h.service.GetWorkflow(c.Request.Context(), middleware.UserID(c), c.Param("id"))
	if err != nil {
		handleError(c, err)
		return
	}
	resp.OK(c, workflow)
}

func (h *Handler) UpdateWorkflow(c *gin.Context) {
	var request struct {
		Revision uint64 `json:"revision" binding:"required"`
		Name     string `json:"name" binding:"required"`
		Graph    Graph  `json:"graph" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		abortBadRequest(c, err.Error())
		return
	}
	workflow, err := h.service.UpdateWorkflow(c.Request.Context(), UpdateWorkflowInput{
		UserID: middleware.UserID(c), WorkflowID: c.Param("id"), ExpectedRevision: request.Revision,
		Name: request.Name, Graph: request.Graph,
	})
	if err != nil {
		handleError(c, err)
		return
	}
	resp.OK(c, workflow)
}

func (h *Handler) DeleteWorkflow(c *gin.Context) {
	if err := h.service.DeleteWorkflow(c.Request.Context(), middleware.UserID(c), c.Param("id")); err != nil {
		handleError(c, err)
		return
	}
	resp.OK(c, gin.H{"ok": true})
}

func (h *Handler) ValidateWorkflow(c *gin.Context) {
	var request struct {
		Graph *Graph `json:"graph"`
	}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			abortBadRequest(c, err.Error())
			return
		}
	}
	var (
		errs ValidationErrors
		err  error
	)
	if request.Graph != nil {
		errs, err = h.service.ValidateWorkflowGraph(c.Request.Context(), middleware.UserID(c), c.Param("id"), *request.Graph)
	} else {
		errs, err = h.service.ValidateWorkflow(c.Request.Context(), middleware.UserID(c), c.Param("id"))
	}
	if err != nil {
		handleError(c, err)
		return
	}
	resp.OK(c, gin.H{"valid": len(errs) == 0, "errors": errs})
}

func (h *Handler) StartRun(c *gin.Context) {
	var request StartRunInput
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			abortBadRequest(c, err.Error())
			return
		}
	} else {
		request.RunMode = RunModeFull
		request.RequestID = "legacy-" + NewRunID()
	}
	run, err := h.service.StartRunWithInput(c.Request.Context(), middleware.UserID(c), c.Param("id"), request)
	if err != nil {
		handleError(c, err)
		return
	}
	resp.OK(c, run)
}

func (h *Handler) ListRuns(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, offset = normalizePage(limit, offset)
	items, total, err := h.service.ListRuns(c.Request.Context(), middleware.UserID(c), c.Param("id"), limit, offset)
	if err != nil {
		handleError(c, err)
		return
	}
	resp.OK(c, gin.H{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) EstimateRun(c *gin.Context) {
	var request struct {
		Revision    uint64  `json:"revision"`
		RunMode     RunMode `json:"run_mode"`
		StartNodeID string  `json:"start_node_id"`
	}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			abortBadRequest(c, err.Error())
			return
		}
	}
	if request.Revision != 0 {
		workflow, err := h.service.GetWorkflow(c.Request.Context(), middleware.UserID(c), c.Param("id"))
		if err != nil {
			handleError(c, err)
			return
		}
		if workflow.Revision != request.Revision {
			handleError(c, ErrRevisionConflict)
			return
		}
	}
	estimate, err := h.service.EstimateRunWithInput(c.Request.Context(), middleware.UserID(c), c.Param("id"), request.RunMode, request.StartNodeID)
	if err != nil {
		handleError(c, err)
		return
	}
	resp.OK(c, estimate)
}

func (h *Handler) CancelRun(c *gin.Context) {
	if err := h.service.CancelRun(c.Request.Context(), middleware.UserID(c), c.Param("run_id")); err != nil {
		handleError(c, err)
		return
	}
	resp.OK(c, gin.H{"ok": true})
}

func (h *Handler) ApproveCharacters(c *gin.Context) {
	var request CharacterApproval
	if err := c.ShouldBindJSON(&request); err != nil {
		abortBadRequest(c, err.Error())
		return
	}
	if err := h.service.ApproveCharacters(c.Request.Context(), middleware.UserID(c), c.Param("run_id"), request); err != nil {
		handleError(c, err)
		return
	}
	resp.OK(c, gin.H{"ok": true})
}

func (h *Handler) ApproveStoryboard(c *gin.Context) {
	var request StoryboardApproval
	if err := c.ShouldBindJSON(&request); err != nil {
		abortBadRequest(c, err.Error())
		return
	}
	if err := h.service.ApproveStoryboard(c.Request.Context(), middleware.UserID(c), c.Param("run_id"), request); err != nil {
		handleError(c, err)
		return
	}
	resp.OK(c, gin.H{"ok": true})
}

// ApproveRun 是统一审批入口；approval_type 固定为 characters 或 storyboard。
func (h *Handler) ApproveRun(c *gin.Context) {
	var request struct {
		ApprovalType string               `json:"approval_type" binding:"required"`
		Selections   []CharacterSelection `json:"selections"`
		NodeRunID    string               `json:"node_run_id"`
		InputHash    string               `json:"input_hash"`
		Script       json.RawMessage      `json:"script"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		abortBadRequest(c, err.Error())
		return
	}
	var err error
	switch request.ApprovalType {
	case "characters":
		err = h.service.ApproveCharacters(c.Request.Context(), middleware.UserID(c), c.Param("run_id"), CharacterApproval{Selections: request.Selections})
	case "storyboard":
		err = h.service.ApproveStoryboard(c.Request.Context(), middleware.UserID(c), c.Param("run_id"), StoryboardApproval{
			NodeRunID: request.NodeRunID, InputHash: request.InputHash, Script: request.Script,
		})
	default:
		abortBadRequest(c, "approval_type 仅支持 characters 或 storyboard")
		return
	}
	if err != nil {
		handleError(c, err)
		return
	}
	resp.OK(c, gin.H{"ok": true})
}

func (h *Handler) GetRun(c *gin.Context) {
	run, err := h.service.GetRunDetail(c.Request.Context(), middleware.UserID(c), c.Param("run_id"))
	if err != nil {
		handleError(c, err)
		return
	}
	resp.OK(c, run)
}

func (h *Handler) ListAssets(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	items, total, err := h.service.ListAssets(c.Request.Context(), middleware.UserID(c), c.Query("kind"), limit, offset)
	if err != nil {
		handleError(c, err)
		return
	}
	resp.OK(c, gin.H{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) UploadAsset(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxVideoBytes+4*1024*1024)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		abortBadRequest(c, "file 必填")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		abortBadRequest(c, err.Error())
		return
	}
	defer file.Close()
	kind := strings.TrimSpace(c.PostForm("kind"))
	if kind == "" {
		kind = inferUploadKind(fileHeader.Filename, fileHeader.Header.Get("Content-Type"))
	}
	asset, err := h.service.UploadAsset(c.Request.Context(), AssetUploadInput{
		UserID: middleware.UserID(c), Kind: kind, Name: c.PostForm("name"),
		FileName: filepath.Base(fileHeader.Filename), Reader: file,
	})
	if err != nil {
		handleError(c, err)
		return
	}
	resp.OK(c, asset)
}

func inferUploadKind(name, contentType string) string {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(contentType, "image/") {
		return MediaKindImage
	}
	if strings.HasPrefix(contentType, "video/") {
		return MediaKindVideo
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".webp":
		return MediaKindImage
	case ".mp4", ".webm", ".mov":
		return MediaKindVideo
	default:
		return ""
	}
}

func (h *Handler) DeleteAsset(c *gin.Context) {
	if err := h.service.DeleteAsset(c.Request.Context(), middleware.UserID(c), c.Param("asset_id")); err != nil {
		handleError(c, err)
		return
	}
	resp.OK(c, gin.H{"ok": true})
}

func (h *Handler) SignAssetVersion(c *gin.Context) {
	var request struct {
		Purpose string `json:"purpose"`
	}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			abortBadRequest(c, err.Error())
			return
		}
	}
	signed, err := h.service.SignAssetVersion(c.Request.Context(), middleware.UserID(c), c.Param("asset_id"), c.Param("version_id"), request.Purpose, time.Now())
	if err != nil {
		handleError(c, err)
		return
	}
	resp.OK(c, signed)
}

func (h *Handler) TransformAssetVersion(c *gin.Context) {
	var request ImageTransform
	if err := c.ShouldBindJSON(&request); err != nil {
		abortBadRequest(c, err.Error())
		return
	}
	version, err := h.service.TransformAssetVersion(c.Request.Context(), middleware.UserID(c), c.Param("asset_id"), c.Param("version_id"), request)
	if err != nil {
		handleError(c, err)
		return
	}
	resp.OK(c, version)
}

// ServeSignedMedia 使用 http.ServeContent，原生支持 Range/If-Modified-Since 与 HEAD。
func (h *Handler) ServeSignedMedia(c *gin.Context) {
	expires, err := strconv.ParseInt(c.Query("exp"), 10, 64)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	version, file, err := h.service.OpenSignedMedia(c.Request.Context(), c.Param("version_id"), c.Query("purpose"), expires, c.Query("sig"), time.Now())
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	defer file.Close()
	if c.Query("purpose") == MediaPurposeDownload {
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", version.ID+filepath.Ext(version.FilePath)))
	}
	c.Header("Content-Type", version.MIMEType)
	c.Header("X-Content-Type-Options", "nosniff")
	http.ServeContent(c.Writer, c.Request, filepath.Base(version.FilePath), version.CreatedAt, file)
}

func handleError(c *gin.Context, err error) {
	var validation *GraphValidationError
	switch {
	case errors.As(err, &validation):
		c.AbortWithStatusJSON(http.StatusUnprocessableEntity, resp.Body{
			Code: resp.CodeBadRequest, Message: ErrInvalidGraph.Error(), Data: gin.H{"errors": validation.Errors},
		})
	case errors.Is(err, ErrRevisionConflict), errors.Is(err, ErrRequestConflict):
		c.AbortWithStatusJSON(http.StatusConflict, resp.Body{Code: resp.CodeConflict, Message: err.Error()})
	case errors.Is(err, ErrInvalidState), errors.Is(err, ErrRunsDraining):
		resp.Conflict(c, err.Error())
	case errors.Is(err, ErrNotFound):
		resp.NotFound(c, "视频工作流不存在")
	case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrUnsupportedMedia), errors.Is(err, ErrAssetQuotaExceeded), errors.Is(err, ErrInvalidMediaPurpose), errors.Is(err, ErrInvalidImageTransform), errors.Is(err, ErrInvalidEstimate):
		abortBadRequest(c, err.Error())
	default:
		resp.Internal(c, err.Error())
	}
}

func abortBadRequest(c *gin.Context, message string) {
	c.AbortWithStatusJSON(http.StatusBadRequest, resp.Body{Code: resp.CodeBadRequest, Message: message})
}
