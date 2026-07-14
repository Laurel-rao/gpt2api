package videoworkflow

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

var registerFakeDB sync.Once

func TestEnsureTemplateUpsertUpdatesVersion(t *testing.T) {
	normalized := strings.Join(strings.Fields(ensureTemplateSQL), " ")
	if !strings.Contains(normalized, "ON DUPLICATE KEY UPDATE version=VALUES(version)") {
		t.Fatalf("template upsert must update version: %s", normalized)
	}
}

func TestEnsureTemplateKeepsCurrentVersionAndDisablesOldVersions(t *testing.T) {
	registerFakeDB.Do(func() { sql.Register("video-workflow-fake", fakeSQLDriver{}) })
	resetTemplateSQLRecorder()
	db, err := sqlx.Open("video-workflow-fake", "record-template")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	template := blankVideoCanvasTemplate()
	template.Code = "ancient_drama_seedance"
	template.Version = 3
	template.Enabled = true
	for i := 0; i < 2; i++ {
		if err := NewDAO(db).EnsureTemplate(context.Background(), &template); err != nil {
			t.Fatal(err)
		}
	}

	executions, events := templateSQLRecorderSnapshot()
	if got := strings.Join(events, ","); got != "begin,exec,exec,commit,begin,exec,exec,commit" {
		t.Fatalf("template transaction events=%s", got)
	}
	if len(executions) != 4 {
		t.Fatalf("template executions=%d, want 4", len(executions))
	}
	for attempt := 0; attempt < 2; attempt++ {
		upsert := executions[attempt*2]
		if len(upsert.args) != 8 || upsert.args[1].Value != int64(3) || upsert.args[7].Value != true {
			t.Fatalf("attempt %d current template args=%v", attempt+1, upsert.args)
		}
		disable := executions[attempt*2+1]
		normalized := strings.Join(strings.Fields(disable.query), " ")
		if normalized != "UPDATE video_workflow_templates SET enabled=0 WHERE code=? AND version<>? AND enabled<>0" {
			t.Fatalf("attempt %d disable query=%s", attempt+1, normalized)
		}
		if len(disable.args) != 2 || disable.args[0].Value != template.Code || disable.args[1].Value != int64(template.Version) {
			t.Fatalf("attempt %d disable args=%v", attempt+1, disable.args)
		}
		if strings.Contains(strings.ToUpper(disable.query), "DELETE") {
			t.Fatalf("attempt %d deleted historical template: %s", attempt+1, disable.query)
		}
	}
}

func TestSQLDAO_SuccessPaths(t *testing.T) {
	registerFakeDB.Do(func() { sql.Register("video-workflow-fake", fakeSQLDriver{}) })
	db, err := sqlx.Open("video-workflow-fake", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dao := NewDAO(db)
	ctx := context.Background()
	if dao.DB() != db {
		t.Fatal("DB accessor returned a different handle")
	}
	if err := dao.EnsureBuiltinTemplate(ctx); err != nil {
		t.Fatal(err)
	}
	templates, err := dao.ListTemplates(ctx)
	if err != nil || len(templates) != 1 {
		t.Fatalf("templates=%v err=%v", templates, err)
	}
	if _, err := dao.GetTemplate(ctx, 1); err != nil {
		t.Fatal(err)
	}

	templateID := uint64(1)
	workflow := &Workflow{ID: "workflow", UserID: 7, TemplateID: &templateID, TemplateVersion: 1, Name: "工作流", Revision: 1, Graph: MustBuiltinTemplateV1().Graph}
	if err := dao.CreateWorkflow(ctx, workflow); err != nil {
		t.Fatal(err)
	}
	if rows, total, err := dao.ListWorkflows(ctx, 7, 20, 0); err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("workflows=%v total=%d err=%v", rows, total, err)
	}
	if _, err := dao.GetWorkflow(ctx, 7, workflow.ID); err != nil {
		t.Fatal(err)
	}
	if err := dao.UpdateWorkflow(ctx, workflow, 1); err != nil || workflow.Revision != 2 {
		t.Fatalf("update revision=%d err=%v", workflow.Revision, err)
	}
	if err := dao.DeleteWorkflow(ctx, 7, workflow.ID); err != nil {
		t.Fatal(err)
	}

	run := &Run{ID: "run", WorkflowID: workflow.ID, UserID: 7, Revision: 2, GraphSnapshot: workflow.Graph, Status: RunQueued}
	if err := dao.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if rows, total, err := dao.ListRuns(ctx, 7, workflow.ID, 20, 0); err != nil || total != 1 || len(rows) != 1 || rows[0].ID != run.ID {
		t.Fatalf("runs=%v total=%d err=%v", rows, total, err)
	}
	if _, err := dao.GetRun(ctx, 7, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := dao.UpdateRunStatus(ctx, run.ID, RunQueued, RunRunning, ""); err != nil {
		t.Fatal(err)
	}

	nodeRun := &NodeRun{ID: "node-run", RunID: run.ID, NodeID: "brief", InputHash: strings.Repeat("a", 64), Status: NodeRunQueued}
	if err := dao.CreateNodeRun(ctx, nodeRun, NodeStoryBrief, 1); err != nil {
		t.Fatal(err)
	}
	if rows, err := dao.ListNodeRuns(ctx, run.ID); err != nil || len(rows) != 1 {
		t.Fatalf("node runs=%v err=%v", rows, err)
	}
	if _, err := dao.GetNodeRun(ctx, nodeRun.ID); err != nil {
		t.Fatal(err)
	}
	if err := dao.UpdateNodeRunStatus(ctx, nodeRun.ID, NodeRunQueued, NodeRunRunning, "upstream", "", ""); err != nil {
		t.Fatal(err)
	}

	asset := &Asset{ID: "asset", OwnerUserID: 7, Name: "角色", Kind: MediaKindImage, Status: AssetPending}
	if err := dao.CreateAsset(ctx, asset); err != nil {
		t.Fatal(err)
	}
	version := &AssetVersion{ID: "version", AssetID: asset.ID, OwnerUserID: 7, Status: AssetReady, MIMEType: "image/png", FilePath: "/tmp/a.png", SizeBytes: 10, SHA256: strings.Repeat("b", 64), InputHash: strings.Repeat("c", 64)}
	if err := dao.CreateAssetVersion(ctx, version); err != nil || version.Version != 1 {
		t.Fatalf("asset version=%+v err=%v", version, err)
	}
	if _, err := dao.GetAssetVersion(ctx, 7, version.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := dao.FindCachedAssetVersion(ctx, 7, version.InputHash); err != nil {
		t.Fatal(err)
	}
	if used, err := dao.UsedAssetBytes(ctx, 7); err != nil || used != 10 {
		t.Fatalf("used=%d err=%v", used, err)
	}
	ref := &AssetReference{VersionID: version.ID, UserID: 7, RefType: "run", RefID: run.ID}
	if err := dao.AddAssetReference(ctx, ref, "brief"); err != nil {
		t.Fatal(err)
	}
	if err := dao.SoftDeleteAsset(ctx, 7, asset.ID); err != nil {
		t.Fatal(err)
	}

	charge := &ChargeReservation{ID: "charge", UserID: 7, RunID: run.ID, NodeRunID: nodeRun.ID, IdempotencyKey: "run:brief:hash", Amount: 5, Status: ChargeReserved}
	if err := dao.CreateCharge(ctx, charge); err != nil {
		t.Fatal(err)
	}
	if _, err := dao.GetChargeByIdempotencyKey(ctx, charge.IdempotencyKey); err != nil {
		t.Fatal(err)
	}
	if err := dao.CompareAndSwapChargeBillingRef(ctx, charge.ID, "", "settling:worker"); err != nil {
		t.Fatal(err)
	}
	if err := dao.UpdateChargeStatus(ctx, charge.ID, ChargeReserved, ChargeSettled, 5); err != nil {
		t.Fatal(err)
	}
}

func TestSQLDAO_NotFoundAndStateConflicts(t *testing.T) {
	registerFakeDB.Do(func() { sql.Register("video-workflow-fake", fakeSQLDriver{}) })
	db, err := sqlx.Open("video-workflow-fake", "empty")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dao := NewDAO(db)
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"template": func() error { _, err := dao.GetTemplate(ctx, 1); return err },
		"workflow": func() error { _, err := dao.GetWorkflow(ctx, 7, "missing"); return err },
		"run":      func() error { _, err := dao.GetRun(ctx, 7, "missing"); return err },
		"node":     func() error { _, err := dao.GetNodeRun(ctx, "missing"); return err },
		"version":  func() error { _, err := dao.GetAssetVersion(ctx, 7, "missing"); return err },
		"cache":    func() error { _, err := dao.FindCachedAssetVersion(ctx, 7, "hash"); return err },
		"charge":   func() error { _, err := dao.GetChargeByIdempotencyKey(ctx, "missing"); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, ErrNotFound) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if err := dao.DeleteWorkflow(ctx, 7, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete error = %v", err)
	}
	workflow := &Workflow{ID: "missing", UserID: 7, Name: "missing", Revision: 1, Graph: validSettingsGraph()}
	if err := dao.UpdateWorkflow(ctx, workflow, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update workflow error = %v", err)
	}
	if rows, total, err := dao.ListWorkflows(ctx, 7, -1, -1); err != nil || len(rows) != 0 || total != 0 {
		t.Fatalf("empty workflows=%v total=%d err=%v", rows, total, err)
	}
	if rows, total, err := dao.ListRuns(ctx, 7, "missing", -1, -1); err != nil || len(rows) != 0 || total != 0 {
		t.Fatalf("empty runs=%v total=%d err=%v", rows, total, err)
	}
	if err := dao.UpdateRunStatus(ctx, "run", RunRunning, RunQueued, ""); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("invalid run transition error = %v", err)
	}
	if err := dao.UpdateRunStatus(ctx, "run", RunQueued, RunRunning, ""); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("run CAS error = %v", err)
	}
	if err := dao.UpdateNodeRunStatus(ctx, "node", NodeRunSucceeded, NodeRunRunning, "", "", ""); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("invalid node transition error = %v", err)
	}
	if err := dao.UpdateNodeRunStatus(ctx, "node", NodeRunQueued, NodeRunRunning, "", "", ""); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("node CAS error = %v", err)
	}
	if err := dao.UpdateChargeStatus(ctx, "charge", ChargeSettled, ChargeRefunded, 0); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("invalid charge transition error = %v", err)
	}
	if err := dao.UpdateChargeStatus(ctx, "charge", ChargeReserved, ChargeSettled, 0); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("charge CAS error = %v", err)
	}
	if err := dao.CompareAndSwapChargeBillingRef(ctx, "charge", "reserved", "settling:worker"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("charge settlement lock error = %v", err)
	}
}

func TestAssetVersionQuotaIsSerializedByUserRow(t *testing.T) {
	userLock := strings.Join(strings.Fields(lockAssetQuotaOwnerSQL), " ")
	quotaRead := strings.Join(strings.Fields(readAssetQuotaUsageSQL), " ")
	assetLock := strings.Join(strings.Fields(lockAssetForVersionSQL), " ")
	if !strings.HasSuffix(userLock, "FOR UPDATE") || strings.Contains(quotaRead, "FOR UPDATE") || !strings.HasSuffix(assetLock, "FOR UPDATE") {
		t.Fatalf("unexpected quota SQL: user=%q quota=%q asset=%q", userLock, quotaRead, assetLock)
	}
}

func TestFindCachedAssetVersionExcludesSoftDeletedAsset(t *testing.T) {
	registerFakeDB.Do(func() { sql.Register("video-workflow-fake", fakeSQLDriver{}) })
	db, err := sqlx.Open("video-workflow-fake", "soft-deleted-cache")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = NewDAO(db).FindCachedAssetVersion(context.Background(), 7, strings.Repeat("c", 64))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("soft-deleted asset version remained cacheable: %v", err)
	}
}

func TestRunClaimIncludesRecoverableCancelPending(t *testing.T) {
	if !strings.Contains(claimableRunPredicate, "status='cancel_pending' AND (lease_expires_at IS NULL OR lease_expires_at < NOW())") {
		t.Fatal("cancel_pending runs are not recoverable through the lease claimer")
	}
}

func TestCancelPendingTransitionPreservesActiveLease(t *testing.T) {
	normalized := strings.Join(strings.Fields(updateRunStatusSQL), " ")
	if !strings.Contains(normalized, "lease_owner=CASE WHEN ?='cancel_pending' THEN lease_owner ELSE '' END") ||
		!strings.Contains(normalized, "WHEN ?='cancel_pending' THEN lease_expires_at") {
		t.Fatalf("cancel_pending transition does not preserve active lease: %s", normalized)
	}
}

func TestCancelExpiredApprovalsCommitsApprovalAndRunAtomically(t *testing.T) {
	registerFakeDB.Do(func() { sql.Register("video-workflow-fake", fakeSQLDriver{}) })
	resetTemplateSQLRecorder()
	resetFakeExpiryState("pending", string(RunAwaitingCharacterApproval))
	db, err := sqlx.Open("video-workflow-fake", "record-expiry")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	runIDs, err := NewDAO(db).CancelExpiredApprovals(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(runIDs) != 1 || runIDs[0] != "expired-run" {
		t.Fatalf("expired run ids=%v", runIDs)
	}
	executions, events := templateSQLRecorderSnapshot()
	queries := templateSQLQueriesSnapshot()
	if got := strings.Join(events, ","); got != "begin,query,exec,exec,commit" {
		t.Fatalf("transaction events=%s", got)
	}
	if len(queries) != 1 || len(queries[0].args) != 0 || !strings.Contains(strings.Join(strings.Fields(queries[0].query), " "), "FOR UPDATE") {
		t.Fatalf("expiry select=%+v", queries)
	}
	if len(executions) != 2 || len(executions[0].args) != 1 || executions[0].args[0].Value != "expired-approval" ||
		len(executions[1].args) != 2 || executions[1].args[0].Value != "expired-run" || executions[1].args[1].Value != string(RunAwaitingCharacterApproval) {
		t.Fatalf("expiry executions=%+v", executions)
	}
	approvalStatus, runStatus := fakeExpiryStateSnapshot()
	if approvalStatus != "expired" || runStatus != string(RunCancelPending) {
		t.Fatalf("approval=%s run=%s", approvalStatus, runStatus)
	}
}

func TestCancelExpiredApprovalsRecoversExpiredApprovalAwaitingRun(t *testing.T) {
	registerFakeDB.Do(func() { sql.Register("video-workflow-fake", fakeSQLDriver{}) })
	resetTemplateSQLRecorder()
	resetFakeExpiryState("expired", string(RunAwaitingCharacterApproval))
	db, err := sqlx.Open("video-workflow-fake", "record-expiry")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	runIDs, err := NewDAO(db).CancelExpiredApprovals(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(runIDs) != 1 || runIDs[0] != "expired-run" {
		t.Fatalf("recovered run ids=%v", runIDs)
	}
	executions, events := templateSQLRecorderSnapshot()
	if got := strings.Join(events, ","); got != "begin,query,exec,commit" {
		t.Fatalf("recovery transaction events=%s", got)
	}
	if len(executions) != 1 || !strings.Contains(executions[0].query, "UPDATE video_workflow_runs") {
		t.Fatalf("recovery executions=%+v", executions)
	}
	approvalStatus, runStatus := fakeExpiryStateSnapshot()
	if approvalStatus != "expired" || runStatus != string(RunCancelPending) {
		t.Fatalf("approval=%s run=%s", approvalStatus, runStatus)
	}
}

func TestSQLDAOFencingAndGlobalSlotStatements(t *testing.T) {
	finalization := strings.Join(strings.Fields(renewRunFinalizationLeaseSQL), " ")
	if !strings.Contains(finalization, "lease_owner=? AND status=?") || !strings.Contains(finalization, "lease_expires_at>=NOW()") {
		t.Fatalf("finalization lease is not fenced: %s", finalization)
	}
	selectSlot := strings.Join(strings.Fields(selectRuntimeSlotSQL), " ")
	claimSlot := strings.Join(strings.Fields(claimRuntimeSlotSQL), " ")
	if !strings.Contains(selectSlot, "lease_token IS NULL OR lease_expires_at IS NULL OR lease_expires_at<NOW()") ||
		!strings.HasSuffix(selectSlot, "FOR UPDATE SKIP LOCKED") ||
		!strings.Contains(claimSlot, "run_id=?, node_run_id=?, lease_owner=?, lease_token=?") {
		t.Fatalf("runtime slot SQL is not lease/token fenced: select=%q claim=%q", selectSlot, claimSlot)
	}
	cache := strings.Join(strings.Fields(findCachedAssetVersionSQL), " ")
	if !strings.Contains(cache, "nr.node_run_id=av.created_by_node_run_id") ||
		!strings.Contains(cache, "av.created_by_node_run_id='' OR (nr.status='succeeded' AND nr.output_version_id=av.version_id)") {
		t.Fatalf("generated cache does not require a succeeded creator: %s", cache)
	}
}

type fakeSQLDriver struct{}

func (fakeSQLDriver) Open(name string) (driver.Conn, error) { return fakeSQLConn{mode: name}, nil }

type fakeSQLConn struct{ mode string }

func (fakeSQLConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (fakeSQLConn) Close() error                        { return nil }
func (c fakeSQLConn) Begin() (driver.Tx, error) {
	recordTemplateSQLEvent(c.mode, "begin")
	return fakeSQLTx{mode: c.mode}, nil
}
func (c fakeSQLConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	recordTemplateSQLEvent(c.mode, "begin")
	return fakeSQLTx{mode: c.mode}, nil
}

func (c fakeSQLConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	recordTemplateSQLExecution(c.mode, query, args)
	if c.mode == "record-expiry" {
		return executeFakeExpiryStatement(query, args)
	}
	if c.mode == "empty" {
		return driver.RowsAffected(0), nil
	}
	return driver.RowsAffected(1), nil
}
func (c fakeSQLConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	recordTemplateSQLQuery(c.mode, query, args)
	graph, _ := json.Marshal(MustBuiltinTemplateV1().Graph)
	now := time.Now()
	if c.mode == "empty" {
		if strings.Contains(query, "COUNT(*)") {
			return &fakeSQLRows{columns: []string{"count"}, values: [][]driver.Value{{int64(0)}}}, nil
		}
		return &fakeSQLRows{columns: fakeColumns(query)}, nil
	}
	switch {
	case c.mode == "record-expiry" && strings.Contains(query, "FROM video_workflow_approvals a"):
		approvalStatus, runStatus := fakeExpiryStateSnapshot()
		columns := []string{"approval_id", "run_id", "approval_type", "status"}
		if (approvalStatus == "pending" || approvalStatus == "expired") && runStatus == string(RunAwaitingCharacterApproval) {
			return &fakeSQLRows{columns: columns, values: [][]driver.Value{{"expired-approval", "expired-run", "characters", approvalStatus}}}, nil
		}
		return &fakeSQLRows{columns: columns}, nil
	case strings.Contains(query, "SELECT id FROM users"):
		return &fakeSQLRows{columns: []string{"id"}, values: [][]driver.Value{{int64(7)}}}, nil
	case strings.Contains(query, "SELECT id FROM video_workflow_assets"):
		return &fakeSQLRows{columns: []string{"id"}, values: [][]driver.Value{{int64(1)}}}, nil
	case strings.Contains(query, "MAX(version)"):
		return &fakeSQLRows{columns: []string{"version"}, values: [][]driver.Value{{int64(1)}}}, nil
	case strings.Contains(query, "COALESCE(SUM(size_bytes)"):
		return &fakeSQLRows{columns: []string{"used"}, values: [][]driver.Value{{int64(10)}}}, nil
	case strings.Contains(query, "COUNT(*)"):
		return &fakeSQLRows{columns: []string{"count"}, values: [][]driver.Value{{int64(1)}}}, nil
	case strings.Contains(query, "FROM video_workflow_templates"):
		return &fakeSQLRows{columns: []string{"id", "code", "version", "name", "description", "source_url", "graph_json", "enabled", "created_at", "updated_at"}, values: [][]driver.Value{{int64(1), "ancient_drama_seedance_v1", int64(1), "模板", "描述", "https://x.com", graph, true, now, now}}}, nil
	case strings.Contains(query, "FROM video_workflows"):
		return &fakeSQLRows{columns: []string{"workflow_id", "user_id", "template_id", "template_version", "name", "revision", "graph_json", "created_at", "updated_at", "deleted_at"}, values: [][]driver.Value{{"workflow", int64(7), int64(1), int64(1), "工作流", int64(1), graph, now, now, nil}}}, nil
	case strings.Contains(query, "FROM video_workflow_revisions") && strings.Contains(query, "ORDER BY revision DESC"):
		return &fakeSQLRows{columns: []string{"workflow_id", "revision", "name", "node_count", "edge_count", "created_at"}, values: [][]driver.Value{{"workflow", int64(2), "工作流", int64(3), int64(2), now}}}, nil
	case strings.Contains(query, "FROM video_workflow_revisions"):
		return &fakeSQLRows{columns: []string{"workflow_id", "user_id", "revision", "name", "graph_json", "node_count", "edge_count", "created_at"}, values: [][]driver.Value{{"workflow", int64(7), int64(2), "工作流", graph, int64(3), int64(2), now}}}, nil
	case strings.Contains(query, "FROM video_workflow_runs") && strings.Contains(query, "ORDER BY created_at DESC"):
		return &fakeSQLRows{columns: []string{"run_id", "workflow_id", "workflow_revision", "status", "progress", "run_mode", "start_node_id", "estimated_credits", "actual_credits", "output_version_id", "error_code", "error_message", "created_at", "started_at", "finished_at"}, values: [][]driver.Value{{"run", "workflow", int64(2), "queued", int64(0), "full", "", int64(10), int64(0), "", "", "", now, nil, nil}}}, nil
	case strings.Contains(query, "FROM video_workflow_runs"):
		return &fakeSQLRows{columns: []string{"run_id", "workflow_id", "user_id", "workflow_revision", "graph_snapshot", "status", "error_code", "error_message", "created_at", "started_at", "finished_at"}, values: [][]driver.Value{{"run", "workflow", int64(7), int64(2), graph, "queued", "", "", now, nil, nil}}}, nil
	case strings.Contains(query, "FROM video_workflow_node_runs"):
		if !strings.Contains(query, "COALESCE(provider_state, JSON_OBJECT())") || !strings.Contains(query, "COALESCE(output_json, JSON_OBJECT())") {
			return &fakeSQLRows{columns: []string{"provider_state"}, values: [][]driver.Value{{nil}}}, nil
		}
		return &fakeSQLRows{
			columns: []string{"node_run_id", "run_id", "node_id", "node_type", "input_hash", "status", "progress", "model_snapshot", "upstream_task_id", "provider_state", "output_version_id", "output_json", "credit_cost", "cache_hit", "attempt", "error_code", "error_message", "created_at", "started_at", "finished_at"},
			values:  [][]driver.Value{{"node-run", "run", "brief", "story_brief", strings.Repeat("a", 64), "queued", int64(0), []byte("{}"), "", []byte("{}"), "", []byte("{}"), int64(0), false, int64(1), "", "", now, nil, nil}},
		}, nil
	case strings.Contains(query, "FROM video_workflow_asset_versions"):
		columns := []string{"version_id", "asset_id", "owner_user_id", "version", "status", "mime_type", "file_path", "size_bytes", "sha256", "width", "height", "duration_ms", "input_hash", "created_at", "deleted_at"}
		normalized := strings.Join(strings.Fields(query), " ")
		if c.mode == "soft-deleted-cache" &&
			strings.Contains(normalized, "JOIN video_workflow_assets a ON a.asset_id=av.asset_id AND a.owner_user_id=av.owner_user_id") &&
			strings.Contains(normalized, "a.deleted_at IS NULL") {
			return &fakeSQLRows{columns: columns}, nil
		}
		return &fakeSQLRows{columns: columns, values: [][]driver.Value{{"version", "asset", int64(7), int64(1), "ready", "image/png", "/tmp/a.png", int64(10), strings.Repeat("b", 64), int64(1), int64(1), int64(0), strings.Repeat("c", 64), now, nil}}}, nil
	case strings.Contains(query, "FROM video_workflow_charge_reservations"):
		return &fakeSQLRows{columns: []string{"charge_id", "user_id", "run_id", "node_run_id", "idempotency_key", "amount", "status", "created_at", "updated_at"}, values: [][]driver.Value{{"charge", int64(7), "run", "node-run", "run:brief:hash", int64(5), "reserved", now, now}}}, nil
	default:
		return &fakeSQLRows{columns: []string{"value"}}, nil
	}
}

func fakeColumns(query string) []string {
	switch {
	case strings.Contains(query, "FROM video_workflow_templates"):
		return []string{"id", "code", "version", "name", "description", "source_url", "graph_json", "enabled", "created_at", "updated_at"}
	case strings.Contains(query, "FROM video_workflows"):
		return []string{"workflow_id", "user_id", "template_id", "template_version", "name", "revision", "graph_json", "created_at", "updated_at", "deleted_at"}
	case strings.Contains(query, "FROM video_workflow_runs") && strings.Contains(query, "ORDER BY created_at DESC"):
		return []string{"run_id", "workflow_id", "workflow_revision", "status", "progress", "run_mode", "start_node_id", "estimated_credits", "actual_credits", "output_version_id", "error_code", "error_message", "created_at", "started_at", "finished_at"}
	case strings.Contains(query, "FROM video_workflow_runs"):
		return []string{"run_id", "workflow_id", "user_id", "workflow_revision", "graph_snapshot", "status", "error_code", "error_message", "created_at", "started_at", "finished_at"}
	case strings.Contains(query, "FROM video_workflow_node_runs"):
		return []string{"node_run_id", "run_id", "node_id", "input_hash", "status", "upstream_task_id", "output_version_id", "credit_cost", "cache_hit", "error_code", "error_message", "created_at", "started_at", "finished_at"}
	case strings.Contains(query, "FROM video_workflow_asset_versions"):
		return []string{"version_id", "asset_id", "owner_user_id", "version", "status", "mime_type", "file_path", "size_bytes", "sha256", "width", "height", "duration_ms", "input_hash", "created_at", "deleted_at"}
	case strings.Contains(query, "FROM video_workflow_charge_reservations"):
		return []string{"charge_id", "user_id", "run_id", "node_run_id", "idempotency_key", "amount", "status", "created_at", "updated_at"}
	default:
		return []string{"value"}
	}
}

type fakeSQLTx struct{ mode string }

func (tx fakeSQLTx) Commit() error {
	recordTemplateSQLEvent(tx.mode, "commit")
	return nil
}
func (tx fakeSQLTx) Rollback() error {
	recordTemplateSQLEvent(tx.mode, "rollback")
	return nil
}

type fakeSQLExecution struct {
	query string
	args  []driver.NamedValue
}

var templateSQLRecorder struct {
	sync.Mutex
	executions []fakeSQLExecution
	queries    []fakeSQLExecution
	events     []string
}

func resetTemplateSQLRecorder() {
	templateSQLRecorder.Lock()
	defer templateSQLRecorder.Unlock()
	templateSQLRecorder.executions = nil
	templateSQLRecorder.queries = nil
	templateSQLRecorder.events = nil
}

func recordTemplateSQLExecution(mode, query string, args []driver.NamedValue) {
	if mode != "record-template" && mode != "record-expiry" {
		return
	}
	templateSQLRecorder.Lock()
	defer templateSQLRecorder.Unlock()
	templateSQLRecorder.executions = append(templateSQLRecorder.executions, fakeSQLExecution{
		query: query,
		args:  append([]driver.NamedValue(nil), args...),
	})
	templateSQLRecorder.events = append(templateSQLRecorder.events, "exec")
}

func recordTemplateSQLQuery(mode, query string, args []driver.NamedValue) {
	if mode != "record-expiry" {
		return
	}
	templateSQLRecorder.Lock()
	defer templateSQLRecorder.Unlock()
	templateSQLRecorder.queries = append(templateSQLRecorder.queries, fakeSQLExecution{
		query: query,
		args:  append([]driver.NamedValue(nil), args...),
	})
	templateSQLRecorder.events = append(templateSQLRecorder.events, "query")
}

func recordTemplateSQLEvent(mode, event string) {
	if mode != "record-template" && mode != "record-expiry" {
		return
	}
	templateSQLRecorder.Lock()
	defer templateSQLRecorder.Unlock()
	templateSQLRecorder.events = append(templateSQLRecorder.events, event)
}

func templateSQLQueriesSnapshot() []fakeSQLExecution {
	templateSQLRecorder.Lock()
	defer templateSQLRecorder.Unlock()
	return append([]fakeSQLExecution(nil), templateSQLRecorder.queries...)
}

var fakeExpiryState struct {
	sync.Mutex
	approvalStatus string
	runStatus      string
}

func resetFakeExpiryState(approvalStatus, runStatus string) {
	fakeExpiryState.Lock()
	defer fakeExpiryState.Unlock()
	fakeExpiryState.approvalStatus = approvalStatus
	fakeExpiryState.runStatus = runStatus
}

func fakeExpiryStateSnapshot() (string, string) {
	fakeExpiryState.Lock()
	defer fakeExpiryState.Unlock()
	return fakeExpiryState.approvalStatus, fakeExpiryState.runStatus
}

func executeFakeExpiryStatement(query string, args []driver.NamedValue) (driver.Result, error) {
	fakeExpiryState.Lock()
	defer fakeExpiryState.Unlock()
	normalized := strings.Join(strings.Fields(query), " ")
	switch {
	case strings.Contains(normalized, "UPDATE video_workflow_approvals SET status='expired'"):
		if len(args) != 1 || args[0].Value != "expired-approval" {
			return nil, errors.New("unexpected expired approval arguments")
		}
		if fakeExpiryState.approvalStatus != "pending" {
			return driver.RowsAffected(0), nil
		}
		fakeExpiryState.approvalStatus = "expired"
		return driver.RowsAffected(1), nil
	case strings.Contains(normalized, "UPDATE video_workflow_runs SET status='cancel_pending'"):
		if len(args) != 2 || args[0].Value != "expired-run" {
			return nil, errors.New("unexpected expired run arguments")
		}
		expected, ok := args[1].Value.(string)
		if !ok || fakeExpiryState.runStatus != expected {
			return driver.RowsAffected(0), nil
		}
		fakeExpiryState.runStatus = string(RunCancelPending)
		return driver.RowsAffected(1), nil
	default:
		return nil, errors.New("unexpected expiry statement")
	}
}

func templateSQLRecorderSnapshot() ([]fakeSQLExecution, []string) {
	templateSQLRecorder.Lock()
	defer templateSQLRecorder.Unlock()
	executions := append([]fakeSQLExecution(nil), templateSQLRecorder.executions...)
	events := append([]string(nil), templateSQLRecorder.events...)
	return executions, events
}

type fakeSQLRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *fakeSQLRows) Columns() []string { return r.columns }
func (r *fakeSQLRows) Close() error      { return nil }
func (r *fakeSQLRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

var _ driver.Driver = fakeSQLDriver{}
var _ driver.Conn = fakeSQLConn{}
var _ driver.ConnBeginTx = fakeSQLConn{}
var _ driver.ExecerContext = fakeSQLConn{}
var _ driver.QueryerContext = fakeSQLConn{}
