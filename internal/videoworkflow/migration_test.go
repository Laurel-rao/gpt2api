package videoworkflow

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRuntimeFencingMigrationHasPreflightIdempotencyAndSlots(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate migration test")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "sql", "migrations", "20260712000100_video_workflow_runtime_fencing.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.Join(strings.Fields(string(raw)), " ")
	for _, required := range []string{
		"DROP TEMPORARY TABLE IF EXISTS `video_workflow_credit_idempotency_preflight`",
		"CREATE TEMPORARY TABLE `video_workflow_credit_idempotency_preflight`",
		"WHERE `ref_id` LIKE 'video-workflow:%'",
		"SET @vw_add_idempotency_column = IF(",
		"information_schema.columns",
		"ADD COLUMN `video_workflow_idempotency_key` VARCHAR(191) GENERATED ALWAYS AS",
		"SET @vw_add_idempotency_index = IF(",
		"information_schema.statistics",
		"ADD UNIQUE KEY `uk_credit_video_workflow_idempotency`",
		"CREATE TABLE IF NOT EXISTS `video_workflow_runtime_slots`",
		"UNIQUE KEY `uk_video_workflow_runtime_slot_token` (`lease_token`)",
		"DROP TABLE IF EXISTS `video_workflow_runtime_slots`",
		"SET @vw_drop_idempotency_index = IF(",
		"DROP INDEX `uk_credit_video_workflow_idempotency`",
		"SET @vw_drop_idempotency_column = IF(",
		"DROP COLUMN `video_workflow_idempotency_key`",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration missing %q", required)
		}
	}
	for _, statement := range []string{
		"PREPARE vw_stmt FROM",
		"EXECUTE vw_stmt",
		"DEALLOCATE PREPARE vw_stmt",
	} {
		if got := strings.Count(sql, statement); got != 4 {
			t.Fatalf("migration must execute four conditional DDL statements: %q count=%d", statement, got)
		}
	}
}
