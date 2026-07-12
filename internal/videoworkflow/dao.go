package videoworkflow

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/432539/gpt2api/internal/billing"
	"github.com/jmoiron/sqlx"
)

type Store interface {
	EnsureTemplate(context.Context, *Template) error
	ListTemplates(context.Context) ([]Template, error)
	GetTemplate(context.Context, uint64) (*Template, error)
	CreateWorkflow(context.Context, *Workflow) error
	ListWorkflows(context.Context, uint64, int, int) ([]Workflow, int64, error)
	GetWorkflow(context.Context, uint64, string) (*Workflow, error)
	UpdateWorkflow(context.Context, *Workflow, uint64) error
	DeleteWorkflow(context.Context, uint64, string) error
	CreateRun(context.Context, *Run) error
	ListRuns(context.Context, uint64, string, int, int) ([]RunListItem, int64, error)
	GetRun(context.Context, uint64, string) (*Run, error)
	UpdateRunStatus(context.Context, string, RunStatus, RunStatus, string) error
}

type SQLDAO struct{ db *sqlx.DB }

func NewDAO(db *sqlx.DB) *SQLDAO { return &SQLDAO{db: db} }
func (d *SQLDAO) DB() *sqlx.DB   { return d.db }

func (d *SQLDAO) EnsureBuiltinTemplate(ctx context.Context) error {
	templates, err := BuiltinTemplates()
	if err != nil {
		return err
	}
	for i := range templates {
		if errs := ValidateGraph(templates[i].Graph, true); len(errs) != 0 {
			return &GraphValidationError{Errors: errs}
		}
		if err := d.EnsureTemplate(ctx, &templates[i]); err != nil {
			return err
		}
	}
	return nil
}

const ensureTemplateSQL = `
INSERT INTO video_workflow_templates
  (code, version, name, description, source_url, graph_json, model_settings, enabled)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  version=VALUES(version), name=VALUES(name), description=VALUES(description), source_url=VALUES(source_url),
  graph_json=VALUES(graph_json), model_settings=VALUES(model_settings), enabled=VALUES(enabled)`

const disableOtherTemplateVersionsSQL = `
UPDATE video_workflow_templates
   SET enabled=0
 WHERE code=? AND version<>? AND enabled<>0`

func (d *SQLDAO) EnsureTemplate(ctx context.Context, template *Template) error {
	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, ensureTemplateSQL,
		template.Code, template.Version, template.Name, template.Description,
		template.SourceURL, template.Graph, template.Graph.Settings, template.Enabled); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, disableOtherTemplateVersionsSQL, template.Code, template.Version); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *SQLDAO) ListTemplates(ctx context.Context) ([]Template, error) {
	var out []Template
	err := d.db.SelectContext(ctx, &out, `
SELECT t.id, t.code, t.version, t.name, t.description, t.source_url,
       t.graph_json, t.enabled, t.created_at, t.updated_at
  FROM video_workflow_templates t
  JOIN (
        SELECT code, MAX(version) AS version
          FROM video_workflow_templates
         WHERE enabled=1
         GROUP BY code
       ) latest ON latest.code=t.code AND latest.version=t.version
 WHERE t.enabled=1
 ORDER BY t.code ASC, t.version DESC, t.id ASC`)
	return out, err
}

func (d *SQLDAO) GetTemplate(ctx context.Context, id uint64) (*Template, error) {
	var out Template
	err := d.db.GetContext(ctx, &out, `
SELECT id, code, version, name, description, source_url, graph_json, enabled, created_at, updated_at
  FROM video_workflow_templates
 WHERE id=? AND enabled=1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &out, err
}

func (d *SQLDAO) CreateWorkflow(ctx context.Context, workflow *Workflow) error {
	if workflow.ID == "" {
		return errors.New("videoworkflow: workflow id is required")
	}
	_, err := d.db.ExecContext(ctx, `
INSERT INTO video_workflows
  (workflow_id, user_id, template_id, template_version, name, revision, graph_json, settings_json)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, workflow.ID, workflow.UserID, workflow.TemplateID,
		workflow.TemplateVersion, workflow.Name, workflow.Revision, workflow.Graph, workflow.Graph.Settings)
	if err != nil {
		return err
	}
	return nil
}

func (d *SQLDAO) ListWorkflows(ctx context.Context, userID uint64, limit, offset int) ([]Workflow, int64, error) {
	limit, offset = normalizePage(limit, offset)
	var total int64
	if err := d.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM video_workflows WHERE user_id=? AND deleted_at IS NULL`, userID); err != nil {
		return nil, 0, err
	}
	var out []Workflow
	err := d.db.SelectContext(ctx, &out, `
SELECT workflow_id, user_id, template_id, template_version, name, revision, graph_json, created_at, updated_at, deleted_at
  FROM video_workflows
 WHERE user_id=? AND deleted_at IS NULL
 ORDER BY updated_at DESC, id DESC
 LIMIT ? OFFSET ?`, userID, limit, offset)
	return out, total, err
}

func (d *SQLDAO) GetWorkflow(ctx context.Context, userID uint64, workflowID string) (*Workflow, error) {
	var out Workflow
	err := d.db.GetContext(ctx, &out, `
SELECT workflow_id, user_id, template_id, template_version, name, revision, graph_json, created_at, updated_at, deleted_at
  FROM video_workflows
 WHERE workflow_id=? AND user_id=? AND deleted_at IS NULL`, workflowID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &out, err
}

func (d *SQLDAO) UpdateWorkflow(ctx context.Context, workflow *Workflow, expectedRevision uint64) error {
	result, err := d.db.ExecContext(ctx, `
UPDATE video_workflows
   SET name=?, graph_json=?, settings_json=?, revision=revision+1
 WHERE workflow_id=? AND user_id=? AND revision=? AND deleted_at IS NULL`,
		workflow.Name, workflow.Graph, workflow.Graph.Settings, workflow.ID, workflow.UserID, expectedRevision)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		var exists int
		if err := d.db.GetContext(ctx, &exists, `SELECT COUNT(*) FROM video_workflows WHERE workflow_id=? AND user_id=? AND deleted_at IS NULL`, workflow.ID, workflow.UserID); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
		return ErrRevisionConflict
	}
	workflow.Revision = expectedRevision + 1
	return nil
}

func (d *SQLDAO) DeleteWorkflow(ctx context.Context, userID uint64, workflowID string) error {
	result, err := d.db.ExecContext(ctx, `UPDATE video_workflows SET deleted_at=NOW() WHERE workflow_id=? AND user_id=? AND deleted_at IS NULL`, workflowID, userID)
	return rowsOrNotFound(result, err)
}

func (d *SQLDAO) CreateRun(ctx context.Context, run *Run) error {
	_, err := d.db.ExecContext(ctx, `
INSERT INTO video_workflow_runs
	  (run_id, workflow_id, user_id, workflow_revision, request_id, run_mode, start_node_id,
	   estimate_token, estimate_hash, estimated_credits, actual_credits, output_version_id,
	   status, graph_snapshot, model_settings_snapshot, progress, error_code, error_message)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, run.ID, run.WorkflowID, run.UserID, run.Revision,
		run.RequestID, run.RunMode, run.StartNodeID, run.EstimateToken, run.EstimateHash, run.EstimatedCost, run.ActualCost,
		run.OutputVersionID, run.Status, run.GraphSnapshot, run.GraphSnapshot.Settings, run.Progress, run.ErrorCode, run.ErrorMessage)
	return err
}

func (d *SQLDAO) ListRuns(ctx context.Context, userID uint64, workflowID string, limit, offset int) ([]RunListItem, int64, error) {
	limit, offset = normalizePage(limit, offset)
	var total int64
	if err := d.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM video_workflow_runs WHERE workflow_id=? AND user_id=?`, workflowID, userID); err != nil {
		return nil, 0, err
	}
	var out []RunListItem
	err := d.db.SelectContext(ctx, &out, `
SELECT run_id, workflow_id, workflow_revision, status, progress, run_mode, start_node_id,
       estimated_credits, actual_credits, output_version_id, error_code,
       COALESCE(error_message, '') AS error_message, created_at, started_at, finished_at
  FROM video_workflow_runs
 WHERE workflow_id=? AND user_id=?
 ORDER BY created_at DESC, id DESC
 LIMIT ? OFFSET ?`, workflowID, userID, limit, offset)
	return out, total, err
}

func (d *SQLDAO) GetRun(ctx context.Context, userID uint64, runID string) (*Run, error) {
	var out Run
	err := d.db.GetContext(ctx, &out, `
	SELECT run_id, workflow_id, user_id, workflow_revision, request_id, run_mode, start_node_id,
	       estimate_token, estimate_hash, estimated_credits, actual_credits, output_version_id,
	       graph_snapshot, status, progress, error_code, COALESCE(error_message, '') AS error_message,
	       lease_owner, lease_expires_at, heartbeat_at, created_at, started_at, finished_at
  FROM video_workflow_runs
 WHERE run_id=? AND user_id=?`, runID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &out, err
}

func (d *SQLDAO) GetRunByID(ctx context.Context, runID string) (*Run, error) {
	var out Run
	err := d.db.GetContext(ctx, &out, `
	SELECT run_id, workflow_id, user_id, workflow_revision, request_id, run_mode, start_node_id,
	       estimate_token, estimate_hash, estimated_credits, actual_credits, output_version_id,
	       graph_snapshot, status, progress, error_code, COALESCE(error_message, '') AS error_message,
	       lease_owner, lease_expires_at, heartbeat_at, created_at, started_at, finished_at
  FROM video_workflow_runs WHERE run_id=?`, runID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &out, err
}

func (d *SQLDAO) GetRunByRequestID(ctx context.Context, userID uint64, requestID string) (*Run, error) {
	var out Run
	err := d.db.GetContext(ctx, &out, runSelect+` WHERE user_id=? AND request_id=?`, userID, requestID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &out, err
}

const runSelect = `SELECT run_id, workflow_id, user_id, workflow_revision, request_id, run_mode, start_node_id,
       estimate_token, estimate_hash, estimated_credits, actual_credits, output_version_id,
       graph_snapshot, status, progress, error_code, COALESCE(error_message, '') AS error_message,
       lease_owner, lease_expires_at, heartbeat_at, created_at, started_at, finished_at
  FROM video_workflow_runs`

const claimableRunPredicate = `(
       status='queued'
       OR (status='running' AND lease_expires_at < NOW())
       OR (status='cancel_pending' AND (lease_expires_at IS NULL OR lease_expires_at < NOW()))
     )`

func (d *SQLDAO) ClaimNextRun(ctx context.Context, workerID string, lease time.Duration) (*Run, error) {
	if strings.TrimSpace(workerID) == "" {
		return nil, errors.New("videoworkflow: worker id is required")
	}
	if lease <= 0 {
		lease = 60 * time.Second
	}
	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var run Run
	err = tx.GetContext(ctx, &run, runSelect+`
	 WHERE `+claimableRunPredicate+`
	 ORDER BY created_at ASC LIMIT 1 FOR UPDATE SKIP LOCKED`)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	leaseUntil := time.Now().Add(lease)
	result, err := tx.ExecContext(ctx, `
	UPDATE video_workflow_runs
	   SET status=CASE WHEN status='cancel_pending' THEN status ELSE 'running' END,
	       lease_owner=?, lease_expires_at=?, heartbeat_at=NOW(),
	       started_at=COALESCE(started_at, NOW())
	 WHERE run_id=? AND `+claimableRunPredicate, workerID, leaseUntil, run.ID)
	if err != nil {
		return nil, err
	}
	if err := rowsOrStateConflict(result, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if run.Status != RunCancelPending {
		run.Status = RunRunning
	}
	run.LeaseOwner = workerID
	run.LeaseExpiresAt = &leaseUntil
	return &run, nil
}

func (d *SQLDAO) HeartbeatRun(ctx context.Context, runID, workerID string, expectedStatus RunStatus, lease time.Duration) error {
	if expectedStatus != RunRunning && expectedStatus != RunCancelPending {
		return ErrInvalidState
	}
	leaseUntil := time.Now().Add(lease)
	result, err := d.db.ExecContext(ctx, `
	UPDATE video_workflow_runs SET heartbeat_at=NOW(), lease_expires_at=?
	 WHERE run_id=? AND lease_owner=? AND status=?`, leaseUntil, runID, workerID, expectedStatus)
	return rowsOrStateConflict(result, err)
}

const renewRunFinalizationLeaseSQL = `
UPDATE video_workflow_runs
   SET heartbeat_at=NOW(), lease_expires_at=?
 WHERE run_id=? AND lease_owner=? AND status=?
   AND lease_expires_at IS NOT NULL AND lease_expires_at>=NOW()`

func (d *SQLDAO) RenewRunFinalizationLease(ctx context.Context, runID, workerID string, expectedStatus RunStatus, lease time.Duration) error {
	if expectedStatus != RunRunning && expectedStatus != RunCancelPending {
		return ErrInvalidState
	}
	if lease <= 0 {
		return ErrInvalidState
	}
	leaseUntil := time.Now().Add(lease)
	result, err := d.db.ExecContext(ctx, renewRunFinalizationLeaseSQL, leaseUntil, runID, workerID, expectedStatus)
	return rowsOrStateConflict(result, err)
}

const (
	ensureRuntimeSlotSQL = `
INSERT IGNORE INTO video_workflow_runtime_slots (slot_kind, slot_index)
VALUES (?, ?)`
	selectRuntimeSlotSQL = `
SELECT slot_index
  FROM video_workflow_runtime_slots
 WHERE slot_kind=? AND slot_index<?
	   AND (lease_token IS NULL OR lease_expires_at IS NULL OR lease_expires_at<NOW())
 ORDER BY slot_index ASC
 LIMIT 1 FOR UPDATE SKIP LOCKED`
	claimRuntimeSlotSQL = `
UPDATE video_workflow_runtime_slots
	   SET run_id=?, node_run_id=?, lease_owner=?, lease_token=?, lease_expires_at=?, heartbeat_at=NOW()
	 WHERE slot_kind=? AND slot_index=?
	   AND (lease_token IS NULL OR lease_expires_at IS NULL OR lease_expires_at<NOW())`
)

func (d *SQLDAO) AcquireRuntimeSlot(ctx context.Context, kind string, capacity int, runID, nodeRunID, workerID, token string, lease time.Duration) (int, error) {
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(runID) == "" || strings.TrimSpace(nodeRunID) == "" ||
		strings.TrimSpace(workerID) == "" || strings.TrimSpace(token) == "" || capacity <= 0 || lease <= 0 {
		return 0, ErrInvalidState
	}
	for index := 0; index < capacity; index++ {
		if _, err := d.db.ExecContext(ctx, ensureRuntimeSlotSQL, kind, index); err != nil {
			return 0, err
		}
	}
	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var lockedRunID string
	if err := tx.GetContext(ctx, &lockedRunID, `
SELECT run_id FROM video_workflow_runs
 WHERE run_id=? AND status='running' AND lease_owner=? AND lease_expires_at>=NOW()
 FOR UPDATE`, runID, workerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrInvalidState
		}
		return 0, err
	}
	var lockedNodeRunID string
	if err := tx.GetContext(ctx, &lockedNodeRunID, `
SELECT node_run_id FROM video_workflow_node_runs
 WHERE node_run_id=? AND run_id=? AND status='running'
 FOR UPDATE`, nodeRunID, runID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrInvalidState
		}
		return 0, err
	}
	var index int
	if err := tx.GetContext(ctx, &index, selectRuntimeSlotSQL, kind, capacity); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	leaseUntil := time.Now().Add(lease)
	result, err := tx.ExecContext(ctx, claimRuntimeSlotSQL, runID, nodeRunID, workerID, token, leaseUntil, kind, index)
	if err != nil {
		return 0, err
	}
	if err := rowsOrStateConflict(result, nil); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return index, nil
}

func (d *SQLDAO) RenewRuntimeSlot(ctx context.Context, kind string, index int, runID, nodeRunID, workerID, token string, lease time.Duration) error {
	if lease <= 0 || strings.TrimSpace(token) == "" {
		return ErrInvalidState
	}
	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lockedRunID string
	if err := tx.GetContext(ctx, &lockedRunID, `
SELECT run_id FROM video_workflow_runs
 WHERE run_id=? AND status='running' AND lease_owner=? AND lease_expires_at>=NOW()
 FOR UPDATE`, runID, workerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidState
		}
		return err
	}
	leaseUntil := time.Now().Add(lease)
	result, err := tx.ExecContext(ctx, `
UPDATE video_workflow_runtime_slots
	   SET lease_expires_at=?, heartbeat_at=NOW()
	 WHERE slot_kind=? AND slot_index=? AND run_id=? AND node_run_id=? AND lease_owner=? AND lease_token=? AND lease_expires_at>=NOW()`,
		leaseUntil, kind, index, runID, nodeRunID, workerID, token)
	if err := rowsOrStateConflict(result, err); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *SQLDAO) ReleaseRuntimeSlot(ctx context.Context, kind string, index int, token string) error {
	result, err := d.db.ExecContext(ctx, `
UPDATE video_workflow_runtime_slots
	   SET run_id='', node_run_id='', lease_owner='', lease_token=NULL, lease_expires_at=NULL
	 WHERE slot_kind=? AND slot_index=? AND lease_token=?`, kind, index, token)
	return rowsOrStateConflict(result, err)
}

func (d *SQLDAO) UpdateRunProgress(ctx context.Context, runID string, progress int) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	result, err := d.db.ExecContext(ctx, `UPDATE video_workflow_runs SET progress=GREATEST(progress, ?) WHERE run_id=?`, progress, runID)
	return rowsOrNotFound(result, err)
}

func (d *SQLDAO) UpdateRunProgressFenced(ctx context.Context, runID, workerID string, progress int) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	result, err := d.db.ExecContext(ctx, `
UPDATE video_workflow_runs
   SET progress=GREATEST(progress, ?)
 WHERE run_id=? AND status='running' AND lease_owner=? AND lease_expires_at>=NOW()`, progress, runID, workerID)
	return rowsOrStateConflict(result, err)
}

func (d *SQLDAO) CompleteRun(ctx context.Context, runID, workerID string, from, to RunStatus, actualCost int64, outputVersionID, errorCode, errorMessage string) error {
	if !CanTransitionRun(from, to) {
		return fmt.Errorf("%w: run %s -> %s", ErrInvalidState, from, to)
	}
	progress := 0
	if to == RunSucceeded {
		progress = 100
	}
	result, err := d.db.ExecContext(ctx, `
UPDATE video_workflow_runs
   SET status=?, progress=CASE WHEN ?=100 THEN 100 ELSE progress END, actual_credits=?, output_version_id=?,
       error_code=?, error_message=?, lease_owner='', lease_expires_at=NULL, finished_at=NOW()
 WHERE run_id=? AND status=? AND (?='' OR lease_owner=?)`, to, progress, actualCost, outputVersionID,
		errorCode, errorMessage, runID, from, workerID, workerID)
	return rowsOrStateConflict(result, err)
}

const updateRunStatusSQL = `
	UPDATE video_workflow_runs
	   SET status=?, error_message=?,
	       started_at=CASE WHEN ?='running' AND started_at IS NULL THEN NOW() ELSE started_at END,
	       finished_at=CASE WHEN ? THEN NOW() ELSE finished_at END,
	       lease_owner=CASE WHEN ?='cancel_pending' THEN lease_owner ELSE '' END,
	       lease_expires_at=CASE
	         WHEN ?='running' THEN DATE_SUB(NOW(), INTERVAL 1 SECOND)
	         WHEN ?='cancel_pending' THEN lease_expires_at
	         ELSE NULL
	       END
	 WHERE run_id=? AND status=?`

func (d *SQLDAO) UpdateRunStatus(ctx context.Context, runID string, from, to RunStatus, errorMessage string) error {
	if !CanTransitionRun(from, to) {
		return fmt.Errorf("%w: run %s -> %s", ErrInvalidState, from, to)
	}
	finished := to == RunSucceeded || to == RunFailed || to == RunCanceled
	result, err := d.db.ExecContext(ctx, updateRunStatusSQL, to, errorMessage, to, finished, to, to, to, runID, from)
	return rowsOrStateConflict(result, err)
}

func (d *SQLDAO) UpdateRunStatusFenced(ctx context.Context, runID, workerID string, from, to RunStatus, errorMessage string) error {
	if !CanTransitionRun(from, to) || from != RunRunning ||
		(to != RunAwaitingCharacterApproval && to != RunAwaitingStoryboardApproval) {
		return ErrInvalidState
	}
	result, err := d.db.ExecContext(ctx, `
UPDATE video_workflow_runs
   SET status=?, error_message=?, lease_owner='', lease_expires_at=NULL
 WHERE run_id=? AND status=? AND lease_owner=? AND lease_expires_at>=NOW()`, to, errorMessage, runID, from, workerID)
	return rowsOrStateConflict(result, err)
}

func (d *SQLDAO) CreateNodeRun(ctx context.Context, nodeRun *NodeRun, nodeType NodeType, attempt int) error {
	if attempt <= 0 {
		attempt = 1
	}
	nodeRun.NodeType = nodeType
	_, err := d.db.ExecContext(ctx, `
INSERT INTO video_workflow_node_runs
	  (node_run_id, run_id, node_id, node_type, input_hash, status, progress, model_snapshot,
	   upstream_task_id, provider_state, output_version_id, output_json, credit_cost, cache_hit, attempt, error_code, error_message)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, nodeRun.ID, nodeRun.RunID, nodeRun.NodeID, nodeType,
		nodeRun.InputHash, nodeRun.Status, nodeRun.Progress, nullableJSON(nodeRun.ModelSnapshot), nodeRun.UpstreamTaskID,
		nullableJSON(nodeRun.ProviderState), nodeRun.OutputVersionID, nullableJSON(nodeRun.Output), nodeRun.CreditCost, nodeRun.CacheHit,
		attempt, nodeRun.ErrorCode, nodeRun.ErrorMessage)
	return err
}

func (d *SQLDAO) ListNodeRuns(ctx context.Context, runID string) ([]NodeRun, error) {
	var out []NodeRun
	err := d.db.SelectContext(ctx, &out, `
	SELECT node_run_id, run_id, node_id, node_type, input_hash, status, progress,
	       COALESCE(model_snapshot, JSON_OBJECT()) AS model_snapshot, upstream_task_id,
	       COALESCE(provider_state, JSON_OBJECT()) AS provider_state,
	       output_version_id, COALESCE(output_json, JSON_OBJECT()) AS output_json,
	       credit_cost, cache_hit, attempt, error_code, COALESCE(error_message, '') AS error_message,
       created_at, started_at, finished_at
  FROM video_workflow_node_runs
 WHERE run_id=?
 ORDER BY id ASC`, runID)
	return out, err
}

func (d *SQLDAO) GetNodeRun(ctx context.Context, nodeRunID string) (*NodeRun, error) {
	var out NodeRun
	err := d.db.GetContext(ctx, &out, `
	SELECT node_run_id, run_id, node_id, node_type, input_hash, status, progress,
	       COALESCE(model_snapshot, JSON_OBJECT()) AS model_snapshot, upstream_task_id,
	       COALESCE(provider_state, JSON_OBJECT()) AS provider_state,
	       output_version_id, COALESCE(output_json, JSON_OBJECT()) AS output_json,
	       credit_cost, cache_hit, attempt, error_code, COALESCE(error_message, '') AS error_message,
       created_at, started_at, finished_at
  FROM video_workflow_node_runs WHERE node_run_id=?`, nodeRunID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &out, err
}

func (d *SQLDAO) UpdateNodeRunOutput(ctx context.Context, nodeRunID string, output json.RawMessage, creditCost int64, cacheHit bool) error {
	result, err := d.db.ExecContext(ctx, `
	UPDATE video_workflow_node_runs
	   SET output_json=?, credit_cost=?, cache_hit=?
	 WHERE node_run_id=? AND status IN ('running', 'awaiting_approval')`, nullableJSON(output), creditCost, cacheHit, nodeRunID)
	return rowsOrStateConflict(result, err)
}

func (d *SQLDAO) RecordNodeRunCost(ctx context.Context, nodeRunID string, creditCost int64) error {
	if creditCost < 0 {
		return ErrInvalidState
	}
	result, err := d.db.ExecContext(ctx, `
UPDATE video_workflow_node_runs
   SET credit_cost=GREATEST(credit_cost, ?)
 WHERE node_run_id=? AND status IN ('running', 'awaiting_approval', 'cancel_pending')`, creditCost, nodeRunID)
	return rowsOrStateConflict(result, err)
}

func (d *SQLDAO) CommitNodeResult(ctx context.Context, commit *NodeResultCommit) error {
	if commit == nil || strings.TrimSpace(commit.RunID) == "" || strings.TrimSpace(commit.WorkerID) == "" ||
		strings.TrimSpace(commit.NodeRunID) == "" || strings.TrimSpace(commit.NodeID) == "" || strings.TrimSpace(commit.InputHash) == "" ||
		commit.ExpectedRunStatus != RunRunning || !CanTransitionNodeRun(commit.From, commit.To) {
		return ErrInvalidState
	}
	if commit.To != NodeRunSucceeded && commit.To != NodeRunAwaitingApproval {
		return ErrInvalidState
	}
	if (commit.Approval != nil) != (commit.To == NodeRunAwaitingApproval) {
		return ErrInvalidState
	}
	if (len(commit.Assets) == 0) != (len(commit.Versions) == 0) || (len(commit.Versions) == 0 && len(commit.References) != 0) {
		return ErrInvalidState
	}
	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var lockedRunID string
	if err := tx.GetContext(ctx, &lockedRunID, `
SELECT run_id FROM video_workflow_runs
 WHERE run_id=? AND status=? AND lease_owner=? AND lease_expires_at>=NOW()
 FOR UPDATE`, commit.RunID, commit.ExpectedRunStatus, commit.WorkerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidState
		}
		return err
	}
	var lockedNode struct {
		RunID     string        `db:"run_id"`
		NodeID    string        `db:"node_id"`
		InputHash string        `db:"input_hash"`
		Status    NodeRunStatus `db:"status"`
	}
	if err := tx.GetContext(ctx, &lockedNode, `
SELECT run_id, node_id, input_hash, status
  FROM video_workflow_node_runs
 WHERE node_run_id=?
 FOR UPDATE`, commit.NodeRunID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if lockedNode.RunID != commit.RunID || lockedNode.NodeID != commit.NodeID || lockedNode.InputHash != commit.InputHash || lockedNode.Status != commit.From {
		return ErrInvalidState
	}

	if len(commit.Versions) > 0 {
		ownerID := commit.Versions[0].OwnerUserID
		var lockedUserID uint64
		if err := tx.GetContext(ctx, &lockedUserID, lockAssetQuotaOwnerSQL, ownerID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		var usedBytes int64
		if err := tx.GetContext(ctx, &usedBytes, readAssetQuotaUsageSQL, ownerID); err != nil {
			return err
		}
		newBytes := int64(0)
		assetOwners := make(map[string]uint64, len(commit.Assets))
		assetVersionCounts := make(map[string]int, len(commit.Assets))
		for i := range commit.Assets {
			asset := &commit.Assets[i]
			if asset.OwnerUserID != ownerID || strings.TrimSpace(asset.ID) == "" {
				return ErrInvalidState
			}
			assetOwners[asset.ID] = asset.OwnerUserID
			if _, err := tx.ExecContext(ctx, `
INSERT INTO video_workflow_assets (asset_id, owner_user_id, name, kind, status)
VALUES (?, ?, ?, ?, ?)`, asset.ID, asset.OwnerUserID, asset.Name, asset.Kind, AssetPending); err != nil {
				return err
			}
		}
		for i := range commit.Versions {
			version := &commit.Versions[i]
			if version.OwnerUserID != ownerID || assetOwners[version.AssetID] != ownerID || version.Status != AssetReady ||
				version.CreatedByRunID != commit.RunID || version.CreatedByNodeRunID != commit.NodeRunID || version.InputHash != commit.InputHash ||
				version.SizeBytes < 0 || newBytes > AssetQuotaBytes-version.SizeBytes {
				return ErrInvalidState
			}
			assetVersionCounts[version.AssetID]++
			newBytes += version.SizeBytes
		}
		for assetID := range assetOwners {
			if assetVersionCounts[assetID] == 0 {
				return ErrInvalidState
			}
		}
		if usedBytes < 0 || newBytes > AssetQuotaBytes-usedBytes {
			return ErrAssetQuotaExceeded
		}
		versionNumbers := make(map[string]uint64, len(commit.Assets))
		currentVersions := make(map[string]string, len(commit.Assets))
		versionAssets := make(map[string]string, len(commit.Versions))
		for i := range commit.Versions {
			version := &commit.Versions[i]
			versionNumbers[version.AssetID]++
			if version.Version == 0 {
				version.Version = versionNumbers[version.AssetID]
			}
			if strings.TrimSpace(version.StorageKey) == "" {
				version.StorageKey = filepath.ToSlash(filepath.Base(version.FilePath))
			}
			if strings.TrimSpace(version.SourceType) == "" {
				version.SourceType = "generated"
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO video_workflow_asset_versions
      (version_id, asset_id, owner_user_id, version, status, mime_type, size_bytes, sha256, storage_key, file_path,
       parent_version_id, source_type, metadata_json, created_by_run_id, created_by_node_run_id, width, height, duration_ms, input_hash)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, version.ID, version.AssetID, version.OwnerUserID,
				version.Version, version.Status, version.MIMEType, version.SizeBytes, version.SHA256, version.StorageKey, version.FilePath,
				version.ParentVersionID, version.SourceType, nullableJSON(version.Metadata), version.CreatedByRunID, version.CreatedByNodeRunID,
				version.Width, version.Height, version.DurationMS, version.InputHash); err != nil {
				return err
			}
			currentVersions[version.AssetID] = version.ID
			versionAssets[version.ID] = version.AssetID
		}
		if selectedAssetID := versionAssets[commit.OutputVersionID]; selectedAssetID != "" {
			currentVersions[selectedAssetID] = commit.OutputVersionID
		}
		for assetID, versionID := range currentVersions {
			result, err := tx.ExecContext(ctx, `UPDATE video_workflow_assets SET current_version_id=?, status='ready' WHERE asset_id=? AND deleted_at IS NULL`, versionID, assetID)
			if err != nil {
				return err
			}
			if err := rowsOrStateConflict(result, nil); err != nil {
				return err
			}
		}
		batchVersions := make(map[string]uint64, len(commit.Versions))
		for i := range commit.Versions {
			batchVersions[commit.Versions[i].ID] = commit.Versions[i].OwnerUserID
		}
		if batchVersions[commit.OutputVersionID] != ownerID {
			return ErrInvalidState
		}
		if len(commit.References) != len(commit.Versions) {
			return ErrInvalidState
		}
		seenReferences := make(map[string]bool, len(commit.References))
		for i := range commit.References {
			ref := &commit.References[i]
			if batchVersions[ref.VersionID] != ownerID || seenReferences[ref.VersionID] || ref.UserID != ownerID || ref.RefType != "run" || ref.RefID != commit.RunID || ref.NodeID != commit.NodeID {
				return ErrInvalidState
			}
			seenReferences[ref.VersionID] = true
			if _, err := tx.ExecContext(ctx, `
INSERT INTO video_workflow_asset_refs (version_id, user_id, ref_type, ref_id, node_id)
VALUES (?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE deleted_at=NULL`, ref.VersionID, ref.UserID, ref.RefType, ref.RefID, ref.NodeID); err != nil {
				return err
			}
		}
	}

	finished := commit.To == NodeRunSucceeded
	result, err := tx.ExecContext(ctx, `
UPDATE video_workflow_node_runs
   SET status=?, progress=CASE WHEN ? THEN 100 ELSE progress END, output_json=?, credit_cost=?, cache_hit=?,
       upstream_task_id=CASE WHEN ?='' THEN upstream_task_id ELSE ? END, output_version_id=?, error_code='', error_message='',
       finished_at=CASE WHEN ? THEN NOW() ELSE finished_at END
 WHERE node_run_id=? AND status=?`, commit.To, finished, nullableJSON(commit.Output), commit.CreditCost, commit.CacheHit,
		commit.UpstreamTaskID, commit.UpstreamTaskID, commit.OutputVersionID, finished, commit.NodeRunID, commit.From)
	if err != nil {
		return err
	}
	if err := rowsOrStateConflict(result, nil); err != nil {
		return err
	}
	if commit.Approval != nil {
		approval := commit.Approval
		if _, err := tx.ExecContext(ctx, `
INSERT INTO video_workflow_approvals
  (approval_id, run_id, node_run_id, node_id, input_hash, approval_type, payload_json, status, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE payload_json=VALUES(payload_json), expires_at=VALUES(expires_at)`, approval.ID, approval.RunID,
			approval.NodeRunID, approval.NodeID, approval.InputHash, approval.Type, nullableJSON(approval.Payload), approval.Status, approval.ExpiresAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *SQLDAO) UpdateNodeRunProgress(ctx context.Context, nodeRunID string, progress int, upstreamTaskID string, providerState json.RawMessage) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	result, err := d.db.ExecContext(ctx, `
UPDATE video_workflow_node_runs
   SET progress=GREATEST(progress, ?), upstream_task_id=CASE WHEN ?='' THEN upstream_task_id ELSE ? END,
       provider_state=CASE WHEN ? IS NULL THEN provider_state ELSE ? END
 WHERE node_run_id=?`, progress, upstreamTaskID, upstreamTaskID, nullableJSON(providerState), nullableJSON(providerState), nodeRunID)
	return rowsOrNotFound(result, err)
}

func (d *SQLDAO) UpdateNodeRunProgressFenced(ctx context.Context, runID, workerID, nodeRunID, inputHash string, progress int, upstreamTaskID string, providerState json.RawMessage) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	result, err := d.db.ExecContext(ctx, `
UPDATE video_workflow_node_runs n
JOIN video_workflow_runs r ON r.run_id=n.run_id
   SET n.progress=GREATEST(n.progress, ?), n.upstream_task_id=CASE WHEN ?='' THEN n.upstream_task_id ELSE ? END,
       n.provider_state=CASE WHEN ? IS NULL THEN n.provider_state ELSE ? END
 WHERE r.run_id=? AND r.status='running' AND r.lease_owner=? AND r.lease_expires_at>=NOW()
   AND n.node_run_id=? AND n.input_hash=? AND n.status='running'`, progress, upstreamTaskID, upstreamTaskID,
		nullableJSON(providerState), nullableJSON(providerState), runID, workerID, nodeRunID, inputHash)
	return rowsOrStateConflict(result, err)
}

func (d *SQLDAO) FindNodeRun(ctx context.Context, runID, nodeID string) (*NodeRun, error) {
	var out NodeRun
	err := d.db.GetContext(ctx, &out, `
	SELECT node_run_id, run_id, node_id, node_type, input_hash, status, progress,
	       COALESCE(model_snapshot, JSON_OBJECT()) AS model_snapshot, upstream_task_id,
	       COALESCE(provider_state, JSON_OBJECT()) AS provider_state,
	       output_version_id, COALESCE(output_json, JSON_OBJECT()) AS output_json,
	       credit_cost, cache_hit, attempt, error_code, COALESCE(error_message, '') AS error_message,
       created_at, started_at, finished_at
  FROM video_workflow_node_runs WHERE run_id=? AND node_id=? ORDER BY attempt DESC LIMIT 1`, runID, nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &out, err
}

func (d *SQLDAO) CreateApproval(ctx context.Context, approval *Approval) error {
	_, err := d.db.ExecContext(ctx, `
INSERT INTO video_workflow_approvals
  (approval_id, run_id, node_run_id, node_id, input_hash, approval_type, payload_json, status, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE payload_json=VALUES(payload_json), expires_at=VALUES(expires_at)`, approval.ID, approval.RunID,
		approval.NodeRunID, approval.NodeID, approval.InputHash, approval.Type, nullableJSON(approval.Payload), approval.Status, approval.ExpiresAt)
	return err
}

func (d *SQLDAO) DecideApproval(ctx context.Context, runID, nodeRunID, inputHash, approvalType string, payload json.RawMessage) error {
	result, err := d.db.ExecContext(ctx, `
UPDATE video_workflow_approvals SET status='approved', payload_json=?, decided_at=NOW()
 WHERE run_id=? AND node_run_id=? AND input_hash=? AND approval_type=? AND status='pending' AND expires_at>NOW()`,
		nullableJSON(payload), runID, nodeRunID, inputHash, approvalType)
	return rowsOrStateConflict(result, err)
}

func (d *SQLDAO) CommitApprovalDecision(ctx context.Context, commit *ApprovalDecisionCommit) error {
	if commit == nil || strings.TrimSpace(commit.RunID) == "" || len(commit.Decisions) == 0 ||
		(commit.ExpectedStatus != RunAwaitingCharacterApproval && commit.ExpectedStatus != RunAwaitingStoryboardApproval) {
		return ErrInvalidState
	}
	decisions := append([]ApprovalNodeDecision(nil), commit.Decisions...)
	sort.Slice(decisions, func(i, j int) bool { return decisions[i].NodeRunID < decisions[j].NodeRunID })
	approvalType := decisions[0].ApprovalType
	if (commit.ExpectedStatus == RunAwaitingCharacterApproval) != (approvalType == "characters") ||
		(commit.ExpectedStatus == RunAwaitingStoryboardApproval) != (approvalType == "storyboard") {
		return ErrInvalidState
	}
	seenNodes := make(map[string]bool, len(decisions))
	for i := range decisions {
		decision := &decisions[i]
		if strings.TrimSpace(decision.NodeRunID) == "" || strings.TrimSpace(decision.NodeID) == "" || strings.TrimSpace(decision.InputHash) == "" ||
			decision.ApprovalType != approvalType || !json.Valid(decision.DecisionPayload) || !json.Valid(decision.Output) || seenNodes[decision.NodeRunID] {
			return ErrInvalidState
		}
		seenNodes[decision.NodeRunID] = true
	}
	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var runStatus RunStatus
	if err := tx.GetContext(ctx, &runStatus, `SELECT status FROM video_workflow_runs WHERE run_id=? FOR UPDATE`, commit.RunID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if !isApprovalDecisionSuccessor(commit.ExpectedStatus, runStatus) {
		return ErrInvalidState
	}
	idempotentRetry := runStatus != commit.ExpectedStatus
	var approvals []Approval
	if err := tx.SelectContext(ctx, &approvals, `
SELECT approval_id, run_id, node_run_id, node_id, input_hash, approval_type,
       COALESCE(payload_json, JSON_OBJECT()) AS payload_json, status,
       decision_note, expires_at, decided_at, created_at, updated_at
  FROM video_workflow_approvals
 WHERE run_id=? AND approval_type=?
 ORDER BY node_run_id ASC
 FOR UPDATE`, commit.RunID, approvalType); err != nil {
		return err
	}
	approvalByNode := make(map[string]*Approval, len(approvals))
	for i := range approvals {
		approvalByNode[approvals[i].NodeRunID] = &approvals[i]
	}
	for i := range decisions {
		decision := &decisions[i]
		approval := approvalByNode[decision.NodeRunID]
		if approval == nil || approval.NodeID != decision.NodeID || approval.InputHash != decision.InputHash {
			return ErrInvalidState
		}
		var node NodeRun
		if err := tx.GetContext(ctx, &node, `
SELECT node_run_id, run_id, node_id, input_hash, status, output_version_id,
       COALESCE(output_json, JSON_OBJECT()) AS output_json
  FROM video_workflow_node_runs
 WHERE node_run_id=?
 FOR UPDATE`, decision.NodeRunID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if node.RunID != commit.RunID || node.NodeID != decision.NodeID || node.InputHash != decision.InputHash {
			return ErrInvalidState
		}
		if idempotentRetry {
			if approval.Status != "approved" || node.Status != NodeRunSucceeded || node.OutputVersionID != decision.OutputVersionID ||
				!bytes.Equal(normalizeJSON(approval.Payload), normalizeJSON(decision.DecisionPayload)) ||
				!bytes.Equal(normalizeJSON(node.Output), normalizeJSON(decision.Output)) {
				return ErrInvalidState
			}
			continue
		}
		if approval.Status != "pending" || !approval.ExpiresAt.After(time.Now()) || node.Status != NodeRunAwaitingApproval {
			return ErrInvalidState
		}
		result, err := tx.ExecContext(ctx, `
UPDATE video_workflow_approvals SET status='approved', payload_json=?, decided_at=NOW()
 WHERE approval_id=? AND status='pending' AND expires_at>NOW()`, nullableJSON(decision.DecisionPayload), approval.ID)
		if err != nil {
			return err
		}
		if err := rowsOrStateConflict(result, nil); err != nil {
			return err
		}
		result, err = tx.ExecContext(ctx, `
UPDATE video_workflow_node_runs
   SET status='running', output_json=?, output_version_id=CASE WHEN ?='' THEN output_version_id ELSE ? END
 WHERE node_run_id=? AND status='awaiting_approval'`, nullableJSON(decision.Output), decision.OutputVersionID, decision.OutputVersionID, decision.NodeRunID)
		if err != nil {
			return err
		}
		if err := rowsOrStateConflict(result, nil); err != nil {
			return err
		}
		result, err = tx.ExecContext(ctx, `
UPDATE video_workflow_node_runs SET status='succeeded', progress=100, finished_at=NOW()
 WHERE node_run_id=? AND status='running'`, decision.NodeRunID)
		if err != nil {
			return err
		}
		if err := rowsOrStateConflict(result, nil); err != nil {
			return err
		}
	}
	if len(approvals) != len(decisions) {
		return ErrInvalidState
	}
	for i := range approvals {
		approval := &approvals[i]
		if !seenNodes[approval.NodeRunID] {
			return ErrInvalidState
		}
	}
	if !idempotentRetry {
		result, err := tx.ExecContext(ctx, `
UPDATE video_workflow_runs
   SET status='running', lease_owner='', lease_expires_at=DATE_SUB(NOW(), INTERVAL 1 SECOND), heartbeat_at=NULL
 WHERE run_id=? AND status=?`, commit.RunID, commit.ExpectedStatus)
		if err != nil {
			return err
		}
		if err := rowsOrStateConflict(result, nil); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *SQLDAO) GetPendingApproval(ctx context.Context, runID, approvalType string) (*Approval, error) {
	var out Approval
	err := d.db.GetContext(ctx, &out, `
SELECT approval_id, run_id, node_run_id, node_id, input_hash, approval_type,
       COALESCE(payload_json, JSON_OBJECT()) AS payload_json, status,
       decision_note, expires_at, decided_at, created_at, updated_at
  FROM video_workflow_approvals WHERE run_id=? AND approval_type=? AND status='pending'
 ORDER BY created_at ASC LIMIT 1`, runID, approvalType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &out, err
}

func (d *SQLDAO) GetApprovalByNode(ctx context.Context, runID, nodeRunID, inputHash, approvalType string) (*Approval, error) {
	var out Approval
	err := d.db.GetContext(ctx, &out, `
SELECT approval_id, run_id, node_run_id, node_id, input_hash, approval_type,
       COALESCE(payload_json, JSON_OBJECT()) AS payload_json, status,
       decision_note, expires_at, decided_at, created_at, updated_at
  FROM video_workflow_approvals
 WHERE run_id=? AND node_run_id=? AND input_hash=? AND approval_type=?
 LIMIT 1`, runID, nodeRunID, inputHash, approvalType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &out, err
}

func (d *SQLDAO) CancelExpiredApprovals(ctx context.Context) ([]string, error) {
	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var expired []struct {
		ApprovalID string `db:"approval_id"`
		RunID      string `db:"run_id"`
		Type       string `db:"approval_type"`
		Status     string `db:"status"`
	}
	if err := tx.SelectContext(ctx, &expired, `
SELECT a.approval_id, a.run_id, a.approval_type, a.status
  FROM video_workflow_approvals a
  JOIN video_workflow_runs r ON r.run_id=a.run_id
 WHERE a.expires_at<=NOW()
   AND (a.status='pending'
        OR (a.status='expired' AND
            ((a.approval_type='characters' AND r.status='awaiting_character_approval')
             OR (a.approval_type='storyboard' AND r.status='awaiting_storyboard_approval'))))
 ORDER BY a.id ASC
	 FOR UPDATE`); err != nil {
		return nil, err
	}
	if len(expired) == 0 {
		return nil, nil
	}
	runIDs := make([]string, 0, len(expired))
	seenRuns := make(map[string]bool, len(expired))
	for i := range expired {
		approval := &expired[i]
		if approval.Status == "pending" {
			if _, err := tx.ExecContext(ctx, `
UPDATE video_workflow_approvals SET status='expired', decided_at=NOW()
 WHERE approval_id=? AND status='pending'`, approval.ApprovalID); err != nil {
				return nil, err
			}
		}
		expected := RunAwaitingStoryboardApproval
		if approval.Type == "characters" {
			expected = RunAwaitingCharacterApproval
		}
		result, err := tx.ExecContext(ctx, `
UPDATE video_workflow_runs
   SET status='cancel_pending', error_message='approval expired', lease_owner='', lease_expires_at=NULL
 WHERE run_id=? AND status=?`, approval.RunID, expected)
		if err != nil {
			return nil, err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if rows > 0 && !seenRuns[approval.RunID] {
			seenRuns[approval.RunID] = true
			runIDs = append(runIDs, approval.RunID)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return runIDs, nil
}

func (d *SQLDAO) UpdateNodeRunStatus(ctx context.Context, nodeRunID string, from, to NodeRunStatus, upstreamTaskID, outputVersionID, errorMessage string) error {
	if !CanTransitionNodeRun(from, to) {
		return fmt.Errorf("%w: node run %s -> %s", ErrInvalidState, from, to)
	}
	finished := to == NodeRunSucceeded || to == NodeRunFailed || to == NodeRunCanceled
	result, err := d.db.ExecContext(ctx, `
UPDATE video_workflow_node_runs
   SET status=?, upstream_task_id=CASE WHEN ?='' THEN upstream_task_id ELSE ? END,
	       output_version_id=CASE WHEN ?='' THEN output_version_id ELSE ? END,
	       error_message=?, started_at=CASE WHEN ?='running' AND started_at IS NULL THEN NOW() ELSE started_at END,
       finished_at=CASE WHEN ? THEN NOW() ELSE finished_at END
 WHERE node_run_id=? AND status=?`, to, upstreamTaskID, upstreamTaskID, outputVersionID, outputVersionID,
		errorMessage, to, finished, nodeRunID, from)
	return rowsOrStateConflict(result, err)
}

func (d *SQLDAO) UpdateNodeRunStatusFenced(ctx context.Context, transition *FencedNodeRunTransition) error {
	if transition == nil || !CanTransitionNodeRun(transition.From, transition.To) || strings.TrimSpace(transition.RunID) == "" ||
		strings.TrimSpace(transition.WorkerID) == "" || strings.TrimSpace(transition.NodeRunID) == "" ||
		strings.TrimSpace(transition.NodeID) == "" || strings.TrimSpace(transition.InputHash) == "" {
		return ErrInvalidState
	}
	finished := transition.To == NodeRunSucceeded || transition.To == NodeRunFailed || transition.To == NodeRunCanceled
	result, err := d.db.ExecContext(ctx, `
UPDATE video_workflow_node_runs n
JOIN video_workflow_runs r ON r.run_id=n.run_id
	   SET n.status=?, n.upstream_task_id=CASE WHEN ?='' THEN n.upstream_task_id ELSE ? END,
	       n.output_version_id=CASE WHEN ?='' THEN n.output_version_id ELSE ? END,
	       n.provider_state=CASE WHEN ? IS NULL THEN n.provider_state ELSE ? END,
	       n.error_message=?, n.started_at=CASE WHEN ?='running' AND n.started_at IS NULL THEN NOW() ELSE n.started_at END,
       n.finished_at=CASE WHEN ? THEN NOW() ELSE n.finished_at END
 WHERE r.run_id=? AND r.status='running' AND r.lease_owner=? AND r.lease_expires_at>=NOW()
	   AND n.node_run_id=? AND n.node_id=? AND n.input_hash=? AND n.status=?`, transition.To,
		transition.UpstreamTaskID, transition.UpstreamTaskID, transition.OutputVersionID, transition.OutputVersionID,
		nullableJSON(transition.ProviderState), nullableJSON(transition.ProviderState), transition.ErrorMessage, transition.To, finished, transition.RunID, transition.WorkerID,
		transition.NodeRunID, transition.NodeID, transition.InputHash, transition.From)
	return rowsOrStateConflict(result, err)
}

func (d *SQLDAO) CreateAsset(ctx context.Context, asset *Asset) error {
	_, err := d.db.ExecContext(ctx, `
INSERT INTO video_workflow_assets (asset_id, owner_user_id, name, kind, status)
VALUES (?, ?, ?, ?, ?)`, asset.ID, asset.OwnerUserID, asset.Name, asset.Kind, asset.Status)
	return err
}

const (
	lockAssetQuotaOwnerSQL = `SELECT id FROM users
 WHERE id=? AND deleted_at IS NULL
 FOR UPDATE`
	readAssetQuotaUsageSQL = `SELECT COALESCE(SUM(size_bytes), 0)
  FROM video_workflow_asset_versions
 WHERE owner_user_id=? AND deleted_at IS NULL`
	lockAssetForVersionSQL = `SELECT id FROM video_workflow_assets
 WHERE asset_id=? AND owner_user_id=? AND deleted_at IS NULL
 FOR UPDATE`
)

func (d *SQLDAO) CreateAssetVersion(ctx context.Context, version *AssetVersion) error {
	if strings.TrimSpace(version.StorageKey) == "" {
		version.StorageKey = filepath.ToSlash(filepath.Base(version.FilePath))
	}
	if strings.TrimSpace(version.SourceType) == "" {
		version.SourceType = "upload"
	}
	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// users 行是每用户素材配额的数据库互斥锁。所有上传、图片变换、运行生成
	// 和最终合成都汇聚到本方法，因此跨进程并发也只能串行核算同一用户配额。
	var lockedUserID uint64
	if err := tx.GetContext(ctx, &lockedUserID, lockAssetQuotaOwnerSQL, version.OwnerUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var usedBytes int64
	if err := tx.GetContext(ctx, &usedBytes, readAssetQuotaUsageSQL, version.OwnerUserID); err != nil {
		return err
	}
	if version.SizeBytes < 0 || usedBytes < 0 || version.SizeBytes > AssetQuotaBytes-usedBytes {
		return ErrAssetQuotaExceeded
	}
	var lockedAssetID uint64
	if err := tx.GetContext(ctx, &lockedAssetID, lockAssetForVersionSQL, version.AssetID, version.OwnerUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if version.Version == 0 {
		if err := tx.GetContext(ctx, &version.Version, `
SELECT COALESCE(MAX(version), 0) + 1
  FROM video_workflow_asset_versions
 WHERE asset_id=?
 FOR UPDATE`, version.AssetID); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO video_workflow_asset_versions
	  (version_id, asset_id, owner_user_id, version, status, mime_type, size_bytes, sha256, storage_key, file_path,
	   parent_version_id, source_type, metadata_json, created_by_run_id, created_by_node_run_id, width, height, duration_ms, input_hash)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, version.ID, version.AssetID, version.OwnerUserID,
		version.Version, version.Status, version.MIMEType, version.SizeBytes, version.SHA256, version.StorageKey, version.FilePath,
		version.ParentVersionID, version.SourceType, nullableJSON(version.Metadata), version.CreatedByRunID, version.CreatedByNodeRunID,
		version.Width, version.Height, version.DurationMS, version.InputHash)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE video_workflow_assets SET current_version_id=?, status='ready' WHERE asset_id=? AND owner_user_id=? AND deleted_at IS NULL`, version.ID, version.AssetID, version.OwnerUserID)
	if err != nil {
		return err
	}
	if err := rowsOrNotFound(result, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *SQLDAO) GetAssetVersion(ctx context.Context, userID uint64, versionID string) (*AssetVersion, error) {
	var out AssetVersion
	err := d.db.GetContext(ctx, &out, `
	SELECT version_id, asset_id, owner_user_id, version, status, mime_type, storage_key, file_path, size_bytes,
	       sha256, parent_version_id, source_type, COALESCE(metadata_json, JSON_OBJECT()) AS metadata_json, created_by_run_id, created_by_node_run_id,
	       width, height, duration_ms, input_hash, created_at, deleted_at
  FROM video_workflow_asset_versions
 WHERE version_id=? AND owner_user_id=? AND deleted_at IS NULL`, versionID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &out, err
}

const findCachedAssetVersionSQL = `
	SELECT av.version_id, av.asset_id, av.owner_user_id, av.version, av.status, av.mime_type, av.storage_key, av.file_path, av.size_bytes,
	       av.sha256, av.parent_version_id, av.source_type, COALESCE(av.metadata_json, JSON_OBJECT()) AS metadata_json,
	       av.created_by_run_id, av.created_by_node_run_id, av.width, av.height, av.duration_ms, av.input_hash, av.created_at, av.deleted_at
  FROM video_workflow_asset_versions av
  JOIN video_workflow_assets a
    ON a.asset_id=av.asset_id AND a.owner_user_id=av.owner_user_id
	LEFT JOIN video_workflow_node_runs nr
	  ON nr.node_run_id=av.created_by_node_run_id AND nr.run_id=av.created_by_run_id
	 WHERE av.owner_user_id=? AND av.input_hash=? AND av.status='ready'
	   AND av.deleted_at IS NULL AND a.deleted_at IS NULL
	   AND (av.created_by_node_run_id='' OR (nr.status='succeeded' AND nr.output_version_id=av.version_id))
 ORDER BY av.id DESC LIMIT 1`

func (d *SQLDAO) FindCachedAssetVersion(ctx context.Context, userID uint64, inputHash string) (*AssetVersion, error) {
	var out AssetVersion
	err := d.db.GetContext(ctx, &out, findCachedAssetVersionSQL, userID, inputHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &out, err
}

func (d *SQLDAO) UsedAssetBytes(ctx context.Context, userID uint64) (int64, error) {
	var used int64
	err := d.db.GetContext(ctx, &used, `SELECT COALESCE(SUM(size_bytes), 0) FROM video_workflow_asset_versions WHERE owner_user_id=? AND deleted_at IS NULL`, userID)
	return used, err
}

func (d *SQLDAO) AddAssetReference(ctx context.Context, ref *AssetReference, nodeID string) error {
	_, err := d.db.ExecContext(ctx, `
INSERT INTO video_workflow_asset_refs (version_id, user_id, ref_type, ref_id, node_id)
VALUES (?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE deleted_at=NULL`, ref.VersionID, ref.UserID, ref.RefType, ref.RefID, nodeID)
	return err
}

func (d *SQLDAO) SoftDeleteAsset(ctx context.Context, userID uint64, assetID string) error {
	result, err := d.db.ExecContext(ctx, `UPDATE video_workflow_assets SET status='deleted', deleted_at=NOW() WHERE asset_id=? AND owner_user_id=? AND deleted_at IS NULL`, assetID, userID)
	return rowsOrNotFound(result, err)
}

func (d *SQLDAO) ListAssets(ctx context.Context, userID uint64, kind string, limit, offset int) ([]Asset, int64, error) {
	limit, offset = normalizePage(limit, offset)
	where := "owner_user_id=? AND deleted_at IS NULL"
	args := []any{userID}
	if kind != "" {
		where += " AND kind=?"
		args = append(args, kind)
	}
	var total int64
	if err := d.db.GetContext(ctx, &total, "SELECT COUNT(*) FROM video_workflow_assets WHERE "+where, args...); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	var out []Asset
	if err := d.db.SelectContext(ctx, &out, `
SELECT asset_id, owner_user_id, kind, name, status, current_version_id, created_at, updated_at, deleted_at
  FROM video_workflow_assets WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, args...); err != nil {
		return nil, 0, err
	}
	for i := range out {
		versions, err := d.ListAssetVersions(ctx, userID, out[i].ID)
		if err != nil {
			return nil, 0, err
		}
		out[i].Versions = versions
	}
	return out, total, nil
}

func (d *SQLDAO) GetAsset(ctx context.Context, userID uint64, assetID string) (*Asset, error) {
	var out Asset
	err := d.db.GetContext(ctx, &out, `
SELECT asset_id, owner_user_id, kind, name, status, current_version_id, created_at, updated_at, deleted_at
  FROM video_workflow_assets WHERE asset_id=? AND owner_user_id=? AND deleted_at IS NULL`, assetID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out.Versions, err = d.ListAssetVersions(ctx, userID, assetID)
	return &out, err
}

func (d *SQLDAO) ListAssetVersions(ctx context.Context, userID uint64, assetID string) ([]AssetVersion, error) {
	var out []AssetVersion
	err := d.db.SelectContext(ctx, &out, `
	SELECT version_id, asset_id, owner_user_id, version, status, mime_type, storage_key, file_path, size_bytes,
	       sha256, parent_version_id, source_type, COALESCE(metadata_json, JSON_OBJECT()) AS metadata_json, created_by_run_id, created_by_node_run_id,
	       width, height, duration_ms, input_hash, created_at, deleted_at
  FROM video_workflow_asset_versions
 WHERE asset_id=? AND owner_user_id=? AND deleted_at IS NULL ORDER BY version DESC`, assetID, userID)
	return out, err
}

func (d *SQLDAO) GetAssetVersionPublic(ctx context.Context, versionID string) (*AssetVersion, error) {
	var out AssetVersion
	err := d.db.GetContext(ctx, &out, `
	SELECT v.version_id, v.asset_id, v.owner_user_id, v.version, v.status, v.mime_type, v.storage_key, v.file_path, v.size_bytes,
	       v.sha256, v.parent_version_id, v.source_type, COALESCE(v.metadata_json, JSON_OBJECT()) AS metadata_json, v.created_by_run_id, v.created_by_node_run_id,
	       v.width, v.height, v.duration_ms, v.input_hash, v.created_at, v.deleted_at
	  FROM video_workflow_asset_versions v
	  JOIN video_workflow_assets a ON a.asset_id=v.asset_id AND a.owner_user_id=v.owner_user_id
	 WHERE v.version_id=? AND v.status='ready' AND v.deleted_at IS NULL AND a.deleted_at IS NULL`, versionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &out, err
}

func (d *SQLDAO) CreateCharge(ctx context.Context, charge *ChargeReservation) error {
	_, err := d.db.ExecContext(ctx, `
INSERT INTO video_workflow_charge_reservations
	  (charge_id, user_id, run_id, node_run_id, idempotency_key, amount, status, billing_ref)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, charge.ID, charge.UserID, charge.RunID, charge.NodeRunID,
		charge.IdempotencyKey, charge.Amount, charge.Status, charge.BillingRef)
	return err
}

func (d *SQLDAO) GetChargeByIdempotencyKey(ctx context.Context, key string) (*ChargeReservation, error) {
	var out ChargeReservation
	err := d.db.GetContext(ctx, &out, `
	SELECT charge_id, user_id, run_id, node_run_id, idempotency_key, amount, actual_amount, platform_overage, status, billing_ref, created_at, updated_at
  FROM video_workflow_charge_reservations WHERE idempotency_key=?`, key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &out, err
}

func (d *SQLDAO) SetChargeBillingRef(ctx context.Context, chargeID, billingRef string) error {
	result, err := d.db.ExecContext(ctx, `UPDATE video_workflow_charge_reservations SET billing_ref=? WHERE charge_id=?`, billingRef, chargeID)
	return rowsOrNotFound(result, err)
}

func (d *SQLDAO) CompareAndSwapChargeBillingRef(ctx context.Context, chargeID, from, to string) error {
	result, err := d.db.ExecContext(ctx, `
	UPDATE video_workflow_charge_reservations
	   SET billing_ref=?
	 WHERE charge_id=? AND status='reserved' AND billing_ref=?`, to, chargeID, from)
	return rowsOrStateConflict(result, err)
}

func (d *SQLDAO) HasBillingTransaction(ctx context.Context, userID uint64, refID string, kinds ...string) (bool, error) {
	if len(kinds) == 0 {
		return false, nil
	}
	query, args, err := sqlx.In(`SELECT COUNT(*) FROM credit_transactions WHERE user_id=? AND ref_id=? AND type IN (?)`, userID, refID, kinds)
	if err != nil {
		return false, err
	}
	query = d.db.Rebind(query)
	var count int
	if err := d.db.GetContext(ctx, &count, query, args...); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (d *SQLDAO) ReserveRunCredits(ctx context.Context, reservation *RunCreditReservation) error {
	if reservation == nil || reservation.Amount <= 0 || reservation.ExpectedStatus != RunRunning ||
		strings.TrimSpace(reservation.RunID) == "" || strings.TrimSpace(reservation.WorkerID) == "" ||
		strings.TrimSpace(reservation.ChargeID) == "" || !strings.HasPrefix(reservation.LockRef, "reserving:") {
		return ErrInvalidState
	}
	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lockedRunID string
	if err := tx.GetContext(ctx, &lockedRunID, `
SELECT run_id FROM video_workflow_runs
 WHERE run_id=? AND user_id=? AND status=? AND lease_owner=? AND lease_expires_at>=NOW()
 FOR UPDATE`, reservation.RunID, reservation.UserID, reservation.ExpectedStatus, reservation.WorkerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidState
		}
		return err
	}
	var charge ChargeReservation
	if err := tx.GetContext(ctx, &charge, `
SELECT charge_id, user_id, run_id, node_run_id, idempotency_key, amount, actual_amount, platform_overage, status, billing_ref, created_at, updated_at
  FROM video_workflow_charge_reservations
 WHERE charge_id=?
 FOR UPDATE`, reservation.ChargeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if charge.UserID != reservation.UserID || charge.RunID != reservation.RunID || charge.IdempotencyKey != reservation.IdempotencyKey ||
		charge.Amount != reservation.Amount || charge.Status != ChargeReserved || charge.BillingRef != reservation.LockRef {
		return ErrInvalidState
	}
	var freezeAmount int64
	err = tx.GetContext(ctx, &freezeAmount, `
SELECT amount FROM credit_transactions
 WHERE user_id=? AND ref_id=? AND type='freeze'
 ORDER BY id DESC LIMIT 1
 FOR UPDATE`, reservation.UserID, reservation.IdempotencyKey)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if freezeAmount != -reservation.Amount {
			return ErrInvalidState
		}
	} else if err := billing.PreDeductTx(ctx, tx, reservation.UserID, 0, reservation.Amount, reservation.IdempotencyKey, "视频工作流预扣"); err != nil {
		if errors.Is(err, billing.ErrInsufficient) {
			result, updateErr := tx.ExecContext(ctx, `
UPDATE video_workflow_charge_reservations
   SET status='refunded', actual_amount=0, refunded_at=NOW()
 WHERE charge_id=? AND status='reserved' AND billing_ref=?`, charge.ID, reservation.LockRef)
			if updateErr != nil {
				return updateErr
			}
			if updateErr := rowsOrStateConflict(result, nil); updateErr != nil {
				return updateErr
			}
			if commitErr := tx.Commit(); commitErr != nil {
				return commitErr
			}
			return billing.ErrInsufficient
		}
		return err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE video_workflow_charge_reservations
   SET billing_ref='reserved'
 WHERE charge_id=? AND status='reserved' AND billing_ref=?`, charge.ID, reservation.LockRef)
	if err != nil {
		return err
	}
	if err := rowsOrStateConflict(result, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *SQLDAO) FinalizeRun(ctx context.Context, finalization *RunFinalization) (int64, error) {
	if finalization == nil || strings.TrimSpace(finalization.RunID) == "" || strings.TrimSpace(finalization.WorkerID) == "" ||
		!CanTransitionRun(finalization.ExpectedStatus, finalization.TargetStatus) {
		return 0, ErrInvalidState
	}
	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var lockedRunID string
	if err := tx.GetContext(ctx, &lockedRunID, `
SELECT run_id FROM video_workflow_runs
 WHERE run_id=? AND user_id=? AND status=? AND lease_owner=? AND lease_expires_at>=NOW()
 FOR UPDATE`, finalization.RunID, finalization.UserID, finalization.ExpectedStatus, finalization.WorkerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrInvalidState
		}
		return 0, err
	}
	var costs []int64
	if err := tx.SelectContext(ctx, &costs, `
SELECT credit_cost FROM video_workflow_node_runs
 WHERE run_id=?
 ORDER BY id ASC
 FOR UPDATE`, finalization.RunID); err != nil {
		return 0, err
	}
	actual := int64(0)
	for _, cost := range costs {
		if cost < 0 || actual > int64(^uint64(0)>>1)-cost {
			return 0, ErrInvalidState
		}
		actual += cost
	}
	if finalization.BillingEnabled && finalization.EstimatedCost > 0 {
		key := "video-workflow:" + finalization.RunID
		var charge ChargeReservation
		err := tx.GetContext(ctx, &charge, `
SELECT charge_id, user_id, run_id, node_run_id, idempotency_key, amount, actual_amount, platform_overage, status, billing_ref, created_at, updated_at
  FROM video_workflow_charge_reservations
 WHERE idempotency_key=?
 FOR UPDATE`, key)
		if errors.Is(err, sql.ErrNoRows) && finalization.TargetStatus == RunCanceled && actual == 0 {
			err = nil
		} else if err != nil {
			return 0, err
		}
		if charge.ID != "" {
			if charge.UserID != finalization.UserID || charge.RunID != finalization.RunID || charge.Amount != finalization.EstimatedCost {
				return 0, ErrInvalidState
			}
			billed := actual
			if billed > finalization.EstimatedCost {
				billed = finalization.EstimatedCost
			}
			switch charge.Status {
			case ChargeSettled:
				if charge.ActualAmount != actual {
					return 0, ErrInvalidState
				}
			case ChargeRefunded:
				if finalization.TargetStatus == RunSucceeded || actual != 0 {
					return 0, ErrInvalidState
				}
			case ChargeReserved:
				var freezeAmount int64
				freezeErr := tx.GetContext(ctx, &freezeAmount, `
SELECT amount FROM credit_transactions
 WHERE user_id=? AND ref_id=? AND type='freeze'
 ORDER BY id DESC LIMIT 1
 FOR UPDATE`, finalization.UserID, key)
				if errors.Is(freezeErr, sql.ErrNoRows) && finalization.TargetStatus == RunCanceled && actual == 0 {
					result, err := tx.ExecContext(ctx, `
UPDATE video_workflow_charge_reservations
   SET status='refunded', actual_amount=0, refunded_at=NOW()
 WHERE charge_id=? AND status='reserved'`, charge.ID)
					if err != nil {
						return 0, err
					}
					if err := rowsOrStateConflict(result, nil); err != nil {
						return 0, err
					}
					break
				}
				if freezeErr != nil {
					return 0, freezeErr
				}
				if freezeAmount != -finalization.EstimatedCost {
					return 0, ErrInvalidState
				}
				var ledger []struct {
					Type   string `db:"type"`
					Amount int64  `db:"amount"`
				}
				if err := tx.SelectContext(ctx, &ledger, `
SELECT type, amount FROM credit_transactions
 WHERE user_id=? AND ref_id=? AND type IN ('unfreeze', 'consume')
 ORDER BY id ASC
 FOR UPDATE`, finalization.UserID, key); err != nil {
					return 0, err
				}
				if len(ledger) == 0 {
					if err := billing.SettleTx(ctx, tx, finalization.UserID, 0, finalization.EstimatedCost, billed, key, "视频工作流结算"); err != nil {
						return 0, err
					}
				} else if !validSettlementLedger(ledger, finalization.EstimatedCost, billed) {
					return 0, ErrInvalidState
				}
				result, err := tx.ExecContext(ctx, `
UPDATE video_workflow_charge_reservations
   SET status='settled', actual_amount=?, platform_overage=GREATEST(?, amount)-amount, settled_at=NOW()
 WHERE charge_id=? AND status='reserved'`, actual, actual, charge.ID)
				if err != nil {
					return 0, err
				}
				if err := rowsOrStateConflict(result, nil); err != nil {
					return 0, err
				}
			default:
				return 0, ErrInvalidState
			}
		}
	}
	progress := 0
	if finalization.TargetStatus == RunSucceeded {
		progress = 100
	}
	result, err := tx.ExecContext(ctx, `
UPDATE video_workflow_runs
   SET status=?, progress=CASE WHEN ?=100 THEN 100 ELSE progress END, actual_credits=?, output_version_id=?,
       error_code=?, error_message=?, lease_owner='', lease_expires_at=NULL, finished_at=NOW()
 WHERE run_id=? AND status=? AND lease_owner=? AND lease_expires_at>=NOW()`, finalization.TargetStatus, progress, actual,
		finalization.OutputVersionID, finalization.ErrorCode, finalization.ErrorMessage, finalization.RunID,
		finalization.ExpectedStatus, finalization.WorkerID)
	if err != nil {
		return 0, err
	}
	if err := rowsOrStateConflict(result, nil); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return actual, nil
}

func validSettlementLedger(ledger []struct {
	Type   string `db:"type"`
	Amount int64  `db:"amount"`
}, expected, billed int64) bool {
	seenUnfreeze, seenConsume := false, false
	for _, entry := range ledger {
		switch entry.Type {
		case "unfreeze":
			if seenUnfreeze || entry.Amount != expected {
				return false
			}
			seenUnfreeze = true
		case "consume":
			if seenConsume || entry.Amount != -billed {
				return false
			}
			seenConsume = true
		default:
			return false
		}
	}
	return seenUnfreeze && (billed == 0 || seenConsume) && (billed != 0 || !seenConsume)
}

func (d *SQLDAO) UpdateChargeStatus(ctx context.Context, chargeID string, from, to ChargeStatus, actualAmount int64) error {
	if !CanTransitionCharge(from, to) {
		return fmt.Errorf("%w: charge %s -> %s", ErrInvalidState, from, to)
	}
	result, err := d.db.ExecContext(ctx, `
	UPDATE video_workflow_charge_reservations
	   SET status=?, actual_amount=?, platform_overage=GREATEST(?, amount)-amount,
       settled_at=CASE WHEN ?='settled' THEN NOW() ELSE settled_at END,
       refunded_at=CASE WHEN ?='refunded' THEN NOW() ELSE refunded_at END
	 WHERE charge_id=? AND status=?`, to, actualAmount, actualAmount, to, to, chargeID, from)
	return rowsOrStateConflict(result, err)
}

func normalizePage(limit, offset int) (int, int) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func rowsOrNotFound(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func rowsOrStateConflict(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrInvalidState
	}
	return nil
}

func joinError(code, message string) string {
	if code == "" {
		return message
	}
	if message == "" {
		return code
	}
	return code + ": " + message
}

var _ Store = (*SQLDAO)(nil)

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}
