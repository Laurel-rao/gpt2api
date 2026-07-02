package ecommerce

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type LibraryAssetFilter struct {
	Keyword      string
	Kind         string
	Scope        string
	ReviewStatus string
	OwnerUserID  uint64
	VisibleToUID uint64
	Admin        bool
}

func (d *DAO) ListLibraryAssets(ctx context.Context, f LibraryAssetFilter, limit, offset int) ([]LibraryAsset, int64, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	where, args := buildLibraryWhere(f)
	var total int64
	if err := d.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM ecommerce_library_assets WHERE `+where, args...); err != nil {
		return nil, 0, err
	}
	var out []LibraryAsset
	err := d.db.SelectContext(ctx, &out, libraryAssetSelectSQL()+` WHERE `+where+`
 ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	return out, total, err
}

func (d *DAO) GetLibraryAsset(ctx context.Context, assetID string) (*LibraryAsset, error) {
	var a LibraryAsset
	err := d.db.GetContext(ctx, &a, libraryAssetSelectSQL()+` WHERE asset_id=? AND deleted_at IS NULL`, assetID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}

func (d *DAO) GetVisibleLibraryAsset(ctx context.Context, assetID string, uid uint64) (*LibraryAsset, error) {
	a, err := d.GetLibraryAsset(ctx, assetID)
	if err != nil {
		return nil, err
	}
	if libraryAssetVisibleTo(*a, uid) {
		return a, nil
	}
	return nil, ErrNotFound
}

func (d *DAO) CreateLibraryAsset(ctx context.Context, a *LibraryAsset) error {
	res, err := d.db.ExecContext(ctx, `
INSERT INTO ecommerce_library_assets
  (asset_id, owner_user_id, kind, scope, review_status, name, code, cover_url, gallery_json, tags_json, detail_json, enabled)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.AssetID, a.OwnerUserID, a.Kind, nullEmpty(a.Scope, LibraryScopePrivate), nullEmpty(a.ReviewStatus, LibraryReviewDraft),
		a.Name, a.Code, a.CoverURL, nullJSON(a.GalleryJSON.RawMessage()), nullJSON(a.TagsJSON.RawMessage()), nullJSON(a.DetailJSON.RawMessage()), a.Enabled)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	a.ID = uint64(id)
	return nil
}

func (d *DAO) UpdateLibraryAsset(ctx context.Context, a *LibraryAsset) error {
	res, err := d.db.ExecContext(ctx, `
UPDATE ecommerce_library_assets
   SET kind=?, scope=?, review_status=?, name=?, code=?, cover_url=?, gallery_json=?, tags_json=?, detail_json=?, enabled=?,
       review_note=CASE WHEN ?='pending' THEN '' ELSE review_note END,
       reviewed_by=CASE WHEN ?='pending' THEN 0 ELSE reviewed_by END,
       reviewed_at=CASE WHEN ?='pending' THEN NULL ELSE reviewed_at END
 WHERE asset_id=? AND owner_user_id=? AND deleted_at IS NULL`,
		a.Kind, a.Scope, a.ReviewStatus, a.Name, a.Code, a.CoverURL,
		nullJSON(a.GalleryJSON.RawMessage()), nullJSON(a.TagsJSON.RawMessage()), nullJSON(a.DetailJSON.RawMessage()), a.Enabled,
		a.ReviewStatus, a.ReviewStatus, a.ReviewStatus, a.AssetID, a.OwnerUserID)
	return checkRows(res, err)
}

func (d *DAO) DeleteLibraryAsset(ctx context.Context, assetID string, ownerUserID uint64) error {
	res, err := d.db.ExecContext(ctx, `
UPDATE ecommerce_library_assets
   SET deleted_at=NOW(), enabled=0
 WHERE asset_id=? AND owner_user_id=? AND deleted_at IS NULL`, assetID, ownerUserID)
	return checkRows(res, err)
}

func (d *DAO) SubmitLibraryAssetReview(ctx context.Context, assetID string, ownerUserID uint64) error {
	res, err := d.db.ExecContext(ctx, `
UPDATE ecommerce_library_assets
   SET scope='public', review_status='pending', review_note='', reviewed_by=0, reviewed_at=NULL
 WHERE asset_id=? AND owner_user_id=? AND deleted_at IS NULL AND enabled=1`, assetID, ownerUserID)
	return checkRows(res, err)
}

func (d *DAO) ReviewLibraryAsset(ctx context.Context, assetID, status, note string, reviewerID uint64) error {
	scope := LibraryScopePrivate
	if status == LibraryReviewApproved {
		scope = LibraryScopePublic
	}
	res, err := d.db.ExecContext(ctx, `
UPDATE ecommerce_library_assets
   SET scope=?, review_status=?, review_note=?, reviewed_by=?, reviewed_at=NOW(),
       enabled=CASE WHEN ?='rejected' THEN enabled ELSE enabled END
 WHERE asset_id=? AND deleted_at IS NULL`, scope, status, truncate(note, 500), reviewerID, status, assetID)
	return checkRows(res, err)
}

func (d *DAO) AdminSetLibraryAssetEnabled(ctx context.Context, assetID string, enabled bool, reviewerID uint64, note string) error {
	res, err := d.db.ExecContext(ctx, `
UPDATE ecommerce_library_assets
   SET enabled=?, review_note=?, reviewed_by=?, reviewed_at=NOW()
 WHERE asset_id=? AND deleted_at IS NULL`, enabled, truncate(note, 500), reviewerID, assetID)
	return checkRows(res, err)
}

func (d *DAO) AddLibraryAssetFile(ctx context.Context, f *LibraryAssetFile) error {
	res, err := d.db.ExecContext(ctx, `
INSERT INTO ecommerce_library_asset_files
  (asset_id, file_usage, origin_name, mime, size_bytes, width, height, sha256, url, sort_order)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.AssetID, nullEmpty(f.FileUsage, "gallery"), f.OriginName, f.MIME, f.SizeBytes, f.Width, f.Height, f.SHA256, f.URL, f.SortOrder)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	f.ID = uint64(id)
	return nil
}

func (d *DAO) ListLibraryAssetFiles(ctx context.Context, assetID string) ([]LibraryAssetFile, error) {
	var out []LibraryAssetFile
	err := d.db.SelectContext(ctx, &out, `
SELECT id, asset_id, file_usage, origin_name, mime, size_bytes, width, height, sha256, url, sort_order, created_at
  FROM ecommerce_library_asset_files
 WHERE asset_id=?
 ORDER BY sort_order ASC, id ASC`, assetID)
	return out, err
}

func (d *DAO) SyncLibraryAssetMedia(ctx context.Context, assetID string, coverURL string, galleryJSON RawJSON) error {
	res, err := d.db.ExecContext(ctx, `
UPDATE ecommerce_library_assets
   SET cover_url=?, gallery_json=?
 WHERE asset_id=? AND deleted_at IS NULL`, coverURL, nullJSON(galleryJSON.RawMessage()), assetID)
	return checkRows(res, err)
}

func buildLibraryWhere(f LibraryAssetFilter) (string, []interface{}) {
	where := []string{"deleted_at IS NULL"}
	args := []interface{}{}
	if f.Kind != "" {
		where = append(where, "kind=?")
		args = append(args, f.Kind)
	}
	if f.Scope != "" {
		where = append(where, "scope=?")
		args = append(args, f.Scope)
	}
	if f.ReviewStatus != "" {
		where = append(where, "review_status=?")
		args = append(args, f.ReviewStatus)
	}
	if f.OwnerUserID > 0 {
		where = append(where, "owner_user_id=?")
		args = append(args, f.OwnerUserID)
	}
	if !f.Admin {
		where = append(where, "enabled=1")
		if f.VisibleToUID > 0 {
			where = append(where, "(owner_user_id=? OR (scope='public' AND review_status='approved'))")
			args = append(args, f.VisibleToUID)
		} else {
			where = append(where, "scope='public' AND review_status='approved'")
		}
	}
	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		where = append(where, "(asset_id LIKE ? OR name LIKE ? OR code LIKE ? OR CAST(tags_json AS CHAR) LIKE ? OR CAST(detail_json AS CHAR) LIKE ?)")
		args = append(args, like, like, like, like, like)
	}
	return strings.Join(where, " AND "), args
}

func libraryAssetSelectSQL() string {
	return `
SELECT id, asset_id, owner_user_id, kind, scope, review_status, name, code, cover_url,
       gallery_json, tags_json, detail_json, enabled, review_note, reviewed_by, reviewed_at,
       created_at, updated_at, deleted_at
  FROM ecommerce_library_assets`
}

func libraryAssetVisibleTo(a LibraryAsset, uid uint64) bool {
	if !a.Enabled || a.DeletedAt.Valid {
		return false
	}
	if uid > 0 && a.OwnerUserID == uid {
		return true
	}
	return a.Scope == LibraryScopePublic && a.ReviewStatus == LibraryReviewApproved
}
