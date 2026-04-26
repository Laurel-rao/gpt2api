package ecommerce

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

var ErrNotFound = errors.New("ecommerce: not found")

type DAO struct{ db *sqlx.DB }

func NewDAO(db *sqlx.DB) *DAO { return &DAO{db: db} }

type ListFilter struct {
	Keyword     string
	Status      string
	EnabledOnly bool
}

func (d *DAO) ListPlatforms(ctx context.Context, f ListFilter) ([]Platform, error) {
	where, args := buildConfigWhere(f)
	var out []Platform
	err := d.db.SelectContext(ctx, &out, `
SELECT id, code, name, language, field_schema, remark, enabled, created_at, updated_at, deleted_at
  FROM ecommerce_platforms
 WHERE `+where+`
 ORDER BY id DESC`, args...)
	return out, err
}

func (d *DAO) GetPlatform(ctx context.Context, id uint64) (*Platform, error) {
	var p Platform
	err := d.db.GetContext(ctx, &p, `
SELECT id, code, name, language, field_schema, remark, enabled, created_at, updated_at, deleted_at
  FROM ecommerce_platforms
 WHERE id=? AND deleted_at IS NULL`, id)
	return platformOrErr(&p, err)
}

func (d *DAO) CreatePlatform(ctx context.Context, p *Platform) error {
	res, err := d.db.ExecContext(ctx, `
INSERT INTO ecommerce_platforms (code, name, language, field_schema, remark, enabled)
VALUES (?, ?, ?, ?, ?, ?)`, p.Code, p.Name, nullEmpty(p.Language, "zh-CN"), nullJSON(p.FieldSchema.RawMessage()), p.Remark, p.Enabled)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	p.ID = uint64(id)
	return nil
}

func (d *DAO) UpdatePlatform(ctx context.Context, p *Platform) error {
	res, err := d.db.ExecContext(ctx, `
UPDATE ecommerce_platforms
   SET code=?, name=?, language=?, field_schema=?, remark=?, enabled=?
 WHERE id=? AND deleted_at IS NULL`, p.Code, p.Name, nullEmpty(p.Language, "zh-CN"), nullJSON(p.FieldSchema.RawMessage()), p.Remark, p.Enabled, p.ID)
	return checkRows(res, err)
}

func (d *DAO) DeletePlatform(ctx context.Context, id uint64) error {
	res, err := d.db.ExecContext(ctx, `UPDATE ecommerce_platforms SET deleted_at=NOW() WHERE id=? AND deleted_at IS NULL`, id)
	return checkRows(res, err)
}

func (d *DAO) ListPromptTemplates(ctx context.Context, f ListFilter) ([]PromptTemplate, error) {
	where, args := buildConfigWhere(f)
	var out []PromptTemplate
	err := d.db.SelectContext(ctx, &out, `
SELECT id, code, name, content_prompt, image_prompt, remark, enabled, created_at, updated_at, deleted_at
  FROM ecommerce_prompt_templates
 WHERE `+where+`
 ORDER BY id DESC`, args...)
	return out, err
}

func (d *DAO) GetPromptTemplate(ctx context.Context, id uint64) (*PromptTemplate, error) {
	var p PromptTemplate
	err := d.db.GetContext(ctx, &p, `
SELECT id, code, name, content_prompt, image_prompt, remark, enabled, created_at, updated_at, deleted_at
  FROM ecommerce_prompt_templates
 WHERE id=? AND deleted_at IS NULL`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

func (d *DAO) CreatePromptTemplate(ctx context.Context, p *PromptTemplate) error {
	res, err := d.db.ExecContext(ctx, `
INSERT INTO ecommerce_prompt_templates (code, name, content_prompt, image_prompt, remark, enabled)
VALUES (?, ?, ?, ?, ?, ?)`, p.Code, p.Name, p.ContentPrompt, p.ImagePrompt, p.Remark, p.Enabled)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	p.ID = uint64(id)
	return nil
}

func (d *DAO) UpdatePromptTemplate(ctx context.Context, p *PromptTemplate) error {
	res, err := d.db.ExecContext(ctx, `
UPDATE ecommerce_prompt_templates
   SET code=?, name=?, content_prompt=?, image_prompt=?, remark=?, enabled=?
 WHERE id=? AND deleted_at IS NULL`, p.Code, p.Name, p.ContentPrompt, p.ImagePrompt, p.Remark, p.Enabled, p.ID)
	return checkRows(res, err)
}

func (d *DAO) DeletePromptTemplate(ctx context.Context, id uint64) error {
	res, err := d.db.ExecContext(ctx, `UPDATE ecommerce_prompt_templates SET deleted_at=NOW() WHERE id=? AND deleted_at IS NULL`, id)
	return checkRows(res, err)
}

func (d *DAO) ListStyleTemplates(ctx context.Context, f ListFilter) ([]StyleTemplate, error) {
	where, args := buildConfigWhere(f)
	var out []StyleTemplate
	err := d.db.SelectContext(ctx, &out, `
SELECT id, code, name, style_prompt, layout_config, remark, enabled, created_at, updated_at, deleted_at
  FROM ecommerce_style_templates
 WHERE `+where+`
 ORDER BY id DESC`, args...)
	return out, err
}

func (d *DAO) GetStyleTemplate(ctx context.Context, id uint64) (*StyleTemplate, error) {
	var s StyleTemplate
	err := d.db.GetContext(ctx, &s, `
SELECT id, code, name, style_prompt, layout_config, remark, enabled, created_at, updated_at, deleted_at
  FROM ecommerce_style_templates
 WHERE id=? AND deleted_at IS NULL`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

func (d *DAO) CreateStyleTemplate(ctx context.Context, s *StyleTemplate) error {
	res, err := d.db.ExecContext(ctx, `
INSERT INTO ecommerce_style_templates (code, name, style_prompt, layout_config, remark, enabled)
VALUES (?, ?, ?, ?, ?, ?)`, s.Code, s.Name, s.StylePrompt, nullJSON(s.LayoutConfig.RawMessage()), s.Remark, s.Enabled)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	s.ID = uint64(id)
	return nil
}

func (d *DAO) UpdateStyleTemplate(ctx context.Context, s *StyleTemplate) error {
	res, err := d.db.ExecContext(ctx, `
UPDATE ecommerce_style_templates
   SET code=?, name=?, style_prompt=?, layout_config=?, remark=?, enabled=?
 WHERE id=? AND deleted_at IS NULL`, s.Code, s.Name, s.StylePrompt, nullJSON(s.LayoutConfig.RawMessage()), s.Remark, s.Enabled, s.ID)
	return checkRows(res, err)
}

func (d *DAO) DeleteStyleTemplate(ctx context.Context, id uint64) error {
	res, err := d.db.ExecContext(ctx, `UPDATE ecommerce_style_templates SET deleted_at=NOW() WHERE id=? AND deleted_at IS NULL`, id)
	return checkRows(res, err)
}

func (d *DAO) CreateTask(ctx context.Context, t *Task) error {
	res, err := d.db.ExecContext(ctx, `
INSERT INTO ecommerce_tasks
  (task_id, user_id, platform_id, prompt_template_id, style_template_id, requirement, reference_images, status, progress)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.TaskID, t.UserID, t.PlatformID, t.PromptTemplateID, t.StyleTemplateID,
		t.Requirement, nullJSON(t.ReferenceImages.RawMessage()), nullEmpty(t.Status, StatusQueued), t.Progress)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	t.ID = uint64(id)
	return nil
}

func (d *DAO) GetTask(ctx context.Context, taskID string) (*TaskRow, error) {
	var t TaskRow
	err := d.db.GetContext(ctx, &t, taskSelectSQL()+` WHERE t.task_id=?`, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &t, err
}

func (d *DAO) ListTasksByUser(ctx context.Context, userID uint64, f ListFilter, limit, offset int) ([]TaskRow, int64, error) {
	if limit <= 0 {
		limit = 10
	}
	where := []string{"t.user_id=?"}
	args := []interface{}{userID}
	if f.Status != "" {
		where = append(where, "t.status=?")
		args = append(args, f.Status)
	}
	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		where = append(where, "(t.requirement LIKE ? OR p.name LIKE ? OR pt.name LIKE ? OR st.name LIKE ?)")
		args = append(args, like, like, like, like)
	}
	countSQL := `
SELECT COUNT(*)
  FROM ecommerce_tasks t
  JOIN ecommerce_platforms p ON p.id=t.platform_id
  JOIN ecommerce_prompt_templates pt ON pt.id=t.prompt_template_id
  JOIN ecommerce_style_templates st ON st.id=t.style_template_id
 WHERE ` + strings.Join(where, " AND ")
	var total int64
	if err := d.db.GetContext(ctx, &total, countSQL, args...); err != nil {
		return nil, 0, err
	}
	var out []TaskRow
	err := d.db.SelectContext(ctx, &out, taskSelectSQL()+` WHERE `+strings.Join(where, " AND ")+`
 ORDER BY t.id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	return out, total, err
}

func (d *DAO) MarkTaskRunning(ctx context.Context, taskID string) error {
	res, err := d.db.ExecContext(ctx, `UPDATE ecommerce_tasks SET status='running', progress=10, started_at=NOW() WHERE task_id=? AND status='queued'`, taskID)
	return checkRows(res, err)
}

func (d *DAO) UpdateTaskProgress(ctx context.Context, taskID string, progress int) error {
	_, err := d.db.ExecContext(ctx, `UPDATE ecommerce_tasks SET progress=? WHERE task_id=? AND status<>'canceled'`, progress, taskID)
	return err
}

func (d *DAO) UpdateTaskDraft(ctx context.Context, taskID string, progress int, output json.RawMessage, html string) error {
	_, err := d.db.ExecContext(ctx, `
UPDATE ecommerce_tasks
   SET progress=?, output_json=?, output_html=?
 WHERE task_id=? AND status<>'canceled'`, progress, nullJSON(output), html, taskID)
	return err
}

func (d *DAO) MarkTaskRetrying(ctx context.Context, taskID string) error {
	_, err := d.db.ExecContext(ctx, `UPDATE ecommerce_tasks SET status='running', error='', finished_at=NULL WHERE task_id=? AND status<>'canceled'`, taskID)
	return err
}

func (d *DAO) ResetTaskForRetry(ctx context.Context, taskID string) error {
	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
UPDATE ecommerce_tasks
   SET status='queued', progress=0, output_json=NULL, output_html='', error='',
       started_at=NULL, finished_at=NULL
 WHERE task_id=? AND status IN ('failed','canceled')`, taskID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ecommerce_assets WHERE task_id=?`, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DAO) MarkTaskSuccess(ctx context.Context, taskID string, output json.RawMessage, html string) error {
	_, err := d.db.ExecContext(ctx, `
UPDATE ecommerce_tasks
   SET status='success', progress=100, output_json=?, output_html=?, error='', finished_at=NOW()
 WHERE task_id=? AND status<>'canceled'`, nullJSON(output), html, taskID)
	return err
}

func (d *DAO) MarkTaskFailed(ctx context.Context, taskID, msg string) error {
	_, err := d.db.ExecContext(ctx, `
UPDATE ecommerce_tasks
   SET status='failed', error=?, finished_at=NOW()
 WHERE task_id=? AND status<>'canceled'`, truncate(msg, 1000), taskID)
	return err
}

func (d *DAO) MarkTaskFailedWithOutput(ctx context.Context, taskID, msg string, output json.RawMessage, html string) error {
	_, err := d.db.ExecContext(ctx, `
UPDATE ecommerce_tasks
   SET status='failed', output_json=?, output_html=?, error=?, finished_at=NOW()
 WHERE task_id=? AND status<>'canceled'`, nullJSON(output), html, truncate(msg, 1000), taskID)
	return err
}

func (d *DAO) MarkTaskCanceled(ctx context.Context, taskID string) error {
	res, err := d.db.ExecContext(ctx, `
UPDATE ecommerce_tasks
   SET status='canceled', error='用户已中断生成', finished_at=NOW()
 WHERE task_id=? AND status IN ('queued','running')`, taskID)
	return checkRows(res, err)
}

func (d *DAO) CreateAsset(ctx context.Context, a *Asset) error {
	res, err := d.db.ExecContext(ctx, `
INSERT INTO ecommerce_assets (task_id, asset_type, image_task_id, url, file_id, prompt, status, error)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, a.TaskID, a.AssetType, a.ImageTaskID, a.URL, a.FileID, a.Prompt, nullEmpty(a.Status, StatusQueued), a.Error)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	a.ID = uint64(id)
	return nil
}

func (d *DAO) UpdateAssetResult(ctx context.Context, id uint64, status, imageTaskID, url, fileID, errMsg string) error {
	res, err := d.db.ExecContext(ctx, `
UPDATE ecommerce_assets
   SET status=?, image_task_id=?, url=?, file_id=?, error=?,
       started_at=CASE
         WHEN ?='running' THEN COALESCE(started_at, NOW())
         WHEN ?='success' THEN COALESCE(started_at, NOW())
         ELSE started_at
       END,
       finished_at=CASE WHEN ? IN ('success','failed') THEN NOW() ELSE NULL END
 WHERE id=? AND status<>'canceled'`, status, imageTaskID, url, fileID, truncate(errMsg, 500), status, status, status, id)
	return checkRows(res, err)
}

func (d *DAO) GetAsset(ctx context.Context, id uint64) (*Asset, error) {
	var a Asset
	err := d.db.GetContext(ctx, &a, `
SELECT id, task_id, asset_type, image_task_id, url, file_id, prompt, status, error, created_at, started_at, finished_at, updated_at
  FROM ecommerce_assets
 WHERE id=?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}

func (d *DAO) MarkAssetRetrying(ctx context.Context, id uint64, imageTaskID, prompt string) error {
	res, err := d.db.ExecContext(ctx, `
UPDATE ecommerce_assets
   SET status='running', image_task_id=?, url='', file_id='', prompt=?, error='', started_at=NOW(), finished_at=NULL
 WHERE id=? AND status<>'canceled'`, imageTaskID, prompt, id)
	return checkRows(res, err)
}

func (d *DAO) MarkTaskAssetsCanceled(ctx context.Context, taskID string) error {
	_, err := d.db.ExecContext(ctx, `
UPDATE ecommerce_assets
   SET status='canceled', error='用户已中断生成', finished_at=CASE WHEN started_at IS NULL THEN finished_at ELSE NOW() END
 WHERE task_id=? AND status IN ('queued','running')`, taskID)
	return err
}

func (d *DAO) ListAssets(ctx context.Context, taskID string) ([]Asset, error) {
	var out []Asset
	err := d.db.SelectContext(ctx, &out, `
SELECT id, task_id, asset_type, image_task_id, url, file_id, prompt, status, error, created_at, started_at, finished_at, updated_at
  FROM ecommerce_assets
 WHERE task_id=?
 ORDER BY id ASC`, taskID)
	return out, err
}

func buildConfigWhere(f ListFilter) (string, []interface{}) {
	where := []string{"deleted_at IS NULL"}
	args := []interface{}{}
	if f.EnabledOnly {
		where = append(where, "enabled=1")
	}
	if f.Keyword != "" {
		where = append(where, "(code LIKE ? OR name LIKE ? OR remark LIKE ?)")
		like := "%" + f.Keyword + "%"
		args = append(args, like, like, like)
	}
	return strings.Join(where, " AND "), args
}

func taskSelectSQL() string {
	return `
SELECT t.id, t.task_id, t.user_id, t.platform_id, t.prompt_template_id, t.style_template_id,
       t.requirement, t.reference_images, t.status, t.progress, t.output_json,
       COALESCE(t.output_html, '') AS output_html, t.error,
       t.created_at, t.started_at, t.finished_at,
       p.name AS platform_name, pt.name AS prompt_name, st.name AS style_name
  FROM ecommerce_tasks t
  JOIN ecommerce_platforms p ON p.id=t.platform_id
  JOIN ecommerce_prompt_templates pt ON pt.id=t.prompt_template_id
  JOIN ecommerce_style_templates st ON st.id=t.style_template_id`
}

func checkRows(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func platformOrErr(p *Platform, err error) (*Platform, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func nullJSON(b json.RawMessage) interface{} {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	return b
}

func nullEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func validateJSON(raw json.RawMessage, name string) error {
	if len(raw) == 0 {
		return nil
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("%s 必须是合法 JSON", name)
	}
	return nil
}
