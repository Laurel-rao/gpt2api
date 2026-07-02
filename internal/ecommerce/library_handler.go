package ecommerce

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/432539/gpt2api/internal/audit"
	"github.com/432539/gpt2api/internal/middleware"
	"github.com/432539/gpt2api/pkg/resp"
)

type libraryAssetReq struct {
	Kind         string          `json:"kind"`
	Scope        string          `json:"scope"`
	Name         string          `json:"name"`
	Code         string          `json:"code"`
	CoverURL     string          `json:"cover_url"`
	Gallery      json.RawMessage `json:"gallery_json"`
	Tags         json.RawMessage `json:"tags_json"`
	Detail       json.RawMessage `json:"detail_json"`
	Enabled      *bool           `json:"enabled"`
	SubmitReview bool            `json:"submit_review"`
}

type reviewLibraryAssetReq struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

type setLibraryAssetEnabledReq struct {
	Enabled bool   `json:"enabled"`
	Note    string `json:"note"`
}

func (h *Handler) ListLibraryAssets(c *gin.Context) {
	uid := middleware.UserID(c)
	if uid == 0 {
		resp.Unauthorized(c, "not logged in")
		return
	}
	limit := queryInt(c, "limit", 20)
	offset := queryInt(c, "offset", 0)
	rows, total, err := h.dao.ListLibraryAssets(c.Request.Context(), LibraryAssetFilter{
		Keyword:      strings.TrimSpace(c.Query("keyword")),
		Kind:         cleanLibraryKind(c.Query("kind")),
		Scope:        cleanLibraryScope(c.Query("scope")),
		ReviewStatus: cleanLibraryReviewStatus(c.Query("review_status")),
		VisibleToUID: uid,
	}, limit, offset)
	if err != nil {
		resp.Internal(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"items": rows, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) GetLibraryAsset(c *gin.Context) {
	uid := middleware.UserID(c)
	if uid == 0 {
		resp.Unauthorized(c, "not logged in")
		return
	}
	row, err := h.dao.GetVisibleLibraryAsset(c.Request.Context(), c.Param("asset_id"), uid)
	if err != nil {
		writeErr(c, err)
		return
	}
	files, err := h.dao.ListLibraryAssetFiles(c.Request.Context(), row.AssetID)
	if err != nil {
		resp.Internal(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"asset": row, "files": files})
}

func (h *Handler) CreateLibraryAsset(c *gin.Context) {
	uid := middleware.UserID(c)
	if uid == 0 {
		resp.Unauthorized(c, "not logged in")
		return
	}
	var req libraryAssetReq
	if !bindLibraryAssetReq(c, &req) {
		return
	}
	row := libraryAssetFromReq(req, uid)
	row.AssetID = NewLibraryAssetID()
	if req.SubmitReview {
		row.Scope = LibraryScopePublic
		row.ReviewStatus = LibraryReviewPending
	}
	if err := h.dao.CreateLibraryAsset(c.Request.Context(), row); err != nil {
		resp.Internal(c, err.Error())
		return
	}
	resp.OK(c, row)
}

func (h *Handler) UpdateLibraryAsset(c *gin.Context) {
	uid := middleware.UserID(c)
	if uid == 0 {
		resp.Unauthorized(c, "not logged in")
		return
	}
	existing, err := h.dao.GetLibraryAsset(c.Request.Context(), c.Param("asset_id"))
	if err != nil {
		writeErr(c, err)
		return
	}
	if existing.OwnerUserID != uid {
		resp.NotFound(c, "资产不存在")
		return
	}
	var req libraryAssetReq
	if !bindLibraryAssetReq(c, &req) {
		return
	}
	row := libraryAssetFromReq(req, uid)
	row.AssetID = existing.AssetID
	if req.SubmitReview {
		row.Scope = LibraryScopePublic
		row.ReviewStatus = LibraryReviewPending
	} else if existing.ReviewStatus == LibraryReviewApproved && row.Scope == LibraryScopePublic {
		row.ReviewStatus = LibraryReviewApproved
	}
	if err := h.dao.UpdateLibraryAsset(c.Request.Context(), row); err != nil {
		writeErr(c, err)
		return
	}
	fresh, _ := h.dao.GetLibraryAsset(c.Request.Context(), row.AssetID)
	resp.OK(c, fresh)
}

func (h *Handler) DeleteLibraryAsset(c *gin.Context) {
	uid := middleware.UserID(c)
	if uid == 0 {
		resp.Unauthorized(c, "not logged in")
		return
	}
	if err := h.dao.DeleteLibraryAsset(c.Request.Context(), c.Param("asset_id"), uid); err != nil {
		writeErr(c, err)
		return
	}
	resp.OK(c, gin.H{"deleted": c.Param("asset_id")})
}

func (h *Handler) SubmitLibraryAssetReview(c *gin.Context) {
	uid := middleware.UserID(c)
	if uid == 0 {
		resp.Unauthorized(c, "not logged in")
		return
	}
	if err := h.dao.SubmitLibraryAssetReview(c.Request.Context(), c.Param("asset_id"), uid); err != nil {
		writeErr(c, err)
		return
	}
	row, _ := h.dao.GetLibraryAsset(c.Request.Context(), c.Param("asset_id"))
	resp.OK(c, row)
}

func (h *Handler) UploadLibraryAssetFile(c *gin.Context) {
	uid := middleware.UserID(c)
	if uid == 0 {
		resp.Unauthorized(c, "not logged in")
		return
	}
	assetID := c.Param("asset_id")
	asset, err := h.dao.GetLibraryAsset(c.Request.Context(), assetID)
	if err != nil {
		writeErr(c, err)
		return
	}
	if asset.OwnerUserID != uid {
		resp.NotFound(c, "资产不存在")
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		resp.BadRequest(c, "file required")
		return
	}
	src, err := fh.Open()
	if err != nil {
		resp.Internal(c, "open upload failed: "+err.Error())
		return
	}
	defer src.Close()
	usage := strings.TrimSpace(c.PostForm("usage"))
	if usage == "" {
		usage = "gallery"
	}
	sortOrder, _ := strconv.Atoi(c.PostForm("sort_order"))
	saved, err := saveLibraryImage(assetID, fh.Filename, src)
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	file := &LibraryAssetFile{
		AssetID:    assetID,
		FileUsage:  usage,
		OriginName: fh.Filename,
		MIME:       saved.MIME,
		SizeBytes:  saved.SizeBytes,
		Width:      saved.Width,
		Height:     saved.Height,
		SHA256:     saved.SHA256,
		URL:        saved.URL,
		SortOrder:  sortOrder,
	}
	if err := h.dao.AddLibraryAssetFile(c.Request.Context(), file); err != nil {
		resp.Internal(c, err.Error())
		return
	}
	files, err := h.dao.ListLibraryAssetFiles(c.Request.Context(), assetID)
	if err != nil {
		resp.Internal(c, err.Error())
		return
	}
	cover := asset.CoverURL
	if usage == "cover" || cover == "" {
		cover = saved.URL
	}
	gallery := mergeLibraryGallery(asset.GalleryJSON, files)
	_ = h.dao.SyncLibraryAssetMedia(c.Request.Context(), assetID, cover, gallery)
	resp.OK(c, gin.H{"file": file, "cover_url": cover, "gallery_json": gallery})
}

func (h *Handler) AdminListLibraryAssets(c *gin.Context) {
	limit := queryInt(c, "limit", 20)
	offset := queryInt(c, "offset", 0)
	rows, total, err := h.dao.ListLibraryAssets(c.Request.Context(), LibraryAssetFilter{
		Keyword:      strings.TrimSpace(c.Query("keyword")),
		Kind:         cleanLibraryKind(c.Query("kind")),
		Scope:        cleanLibraryScope(c.Query("scope")),
		ReviewStatus: cleanLibraryReviewStatus(c.Query("review_status")),
		Admin:        true,
	}, limit, offset)
	if err != nil {
		resp.Internal(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"items": rows, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) AdminGetLibraryAsset(c *gin.Context) {
	row, err := h.dao.GetLibraryAsset(c.Request.Context(), c.Param("asset_id"))
	if err != nil {
		writeErr(c, err)
		return
	}
	files, err := h.dao.ListLibraryAssetFiles(c.Request.Context(), row.AssetID)
	if err != nil {
		resp.Internal(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"asset": row, "files": files})
}

func (h *Handler) AdminReviewLibraryAsset(c *gin.Context) {
	var req reviewLibraryAssetReq
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		resp.BadRequest(c, "invalid request")
		return
	}
	status := cleanLibraryReviewStatus(req.Status)
	if status != LibraryReviewApproved && status != LibraryReviewRejected {
		resp.BadRequest(c, "status must be approved or rejected")
		return
	}
	actor := middleware.UserID(c)
	if err := h.dao.ReviewLibraryAsset(c.Request.Context(), c.Param("asset_id"), status, req.Note, actor); err != nil {
		writeErr(c, err)
		return
	}
	audit.Record(c, h.auditDAO, "ecommerce.library.review", c.Param("asset_id"), gin.H{"status": status})
	row, _ := h.dao.GetLibraryAsset(c.Request.Context(), c.Param("asset_id"))
	resp.OK(c, row)
}

func (h *Handler) AdminSetLibraryAssetEnabled(c *gin.Context) {
	var req setLibraryAssetEnabledReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "invalid request")
		return
	}
	actor := middleware.UserID(c)
	if err := h.dao.AdminSetLibraryAssetEnabled(c.Request.Context(), c.Param("asset_id"), req.Enabled, actor, req.Note); err != nil {
		writeErr(c, err)
		return
	}
	audit.Record(c, h.auditDAO, "ecommerce.library.enabled", c.Param("asset_id"), gin.H{"enabled": req.Enabled})
	row, _ := h.dao.GetLibraryAsset(c.Request.Context(), c.Param("asset_id"))
	resp.OK(c, row)
}

func bindLibraryAssetReq(c *gin.Context, req *libraryAssetReq) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		resp.BadRequest(c, err.Error())
		return false
	}
	if err := validateJSON(req.Gallery, "gallery_json"); err != nil {
		resp.BadRequest(c, err.Error())
		return false
	}
	if err := validateJSON(req.Tags, "tags_json"); err != nil {
		resp.BadRequest(c, err.Error())
		return false
	}
	if err := validateJSON(req.Detail, "detail_json"); err != nil {
		resp.BadRequest(c, err.Error())
		return false
	}
	if cleanLibraryKind(req.Kind) == "" {
		resp.BadRequest(c, "kind must be product or model")
		return false
	}
	if strings.TrimSpace(req.Name) == "" {
		resp.BadRequest(c, "name required")
		return false
	}
	return true
}

func libraryAssetFromReq(req libraryAssetReq, uid uint64) *LibraryAsset {
	scope := cleanLibraryScope(req.Scope)
	if scope == "" {
		scope = LibraryScopePrivate
	}
	status := LibraryReviewDraft
	if scope == LibraryScopePublic {
		status = LibraryReviewPending
	}
	return &LibraryAsset{
		OwnerUserID:  uid,
		Kind:         cleanLibraryKind(req.Kind),
		Scope:        scope,
		ReviewStatus: status,
		Name:         truncate(strings.TrimSpace(req.Name), 128),
		Code:         truncate(strings.TrimSpace(req.Code), 128),
		CoverURL:     truncate(strings.TrimSpace(req.CoverURL), 1024),
		GalleryJSON:  RawJSON(req.Gallery),
		TagsJSON:     RawJSON(req.Tags),
		DetailJSON:   RawJSON(req.Detail),
		Enabled:      boolDefault(req.Enabled, true),
	}
}

func cleanLibraryKind(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case LibraryKindProduct:
		return LibraryKindProduct
	case LibraryKindModel:
		return LibraryKindModel
	default:
		return ""
	}
}

func cleanLibraryScope(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case LibraryScopePrivate:
		return LibraryScopePrivate
	case LibraryScopePublic:
		return LibraryScopePublic
	default:
		return ""
	}
}

func cleanLibraryReviewStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case LibraryReviewDraft:
		return LibraryReviewDraft
	case LibraryReviewPending:
		return LibraryReviewPending
	case LibraryReviewApproved:
		return LibraryReviewApproved
	case LibraryReviewRejected:
		return LibraryReviewRejected
	default:
		return ""
	}
}
