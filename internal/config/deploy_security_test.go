package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProductionComposeRequiresSecurityEnvironment(t *testing.T) {
	compose := readRepositoryFile(t, "deploy/docker-compose.yml")
	for _, name := range []string{
		"MYSQL_ROOT_PASSWORD", "MYSQL_PASSWORD", "JWT_SECRET", "CRYPTO_AES_KEY",
		"VIDEO_WORKFLOW_SIGNING_SECRET", "VIDEO_WORKFLOW_PUBLIC_BASE_URL",
	} {
		if !strings.Contains(compose, "${"+name+":?") {
			t.Fatalf("compose does not require %s with :?", name)
		}
	}
	for _, unsafe := range []string{
		"${JWT_SECRET:-", "${CRYPTO_AES_KEY:-", "${VIDEO_WORKFLOW_SIGNING_SECRET:-",
		"${VIDEO_WORKFLOW_PUBLIC_BASE_URL:-", "dev_secret_change_me_in_production",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	} {
		if strings.Contains(compose, unsafe) {
			t.Fatalf("compose still contains insecure fallback %q", unsafe)
		}
	}
}

func TestDeployEnvironmentExampleLeavesRequiredValuesEmpty(t *testing.T) {
	environment := readRepositoryFile(t, "deploy/.env.example")
	for _, name := range []string{
		"MYSQL_ROOT_PASSWORD", "MYSQL_PASSWORD", "JWT_SECRET", "CRYPTO_AES_KEY",
		"VIDEO_WORKFLOW_SIGNING_SECRET", "VIDEO_WORKFLOW_PUBLIC_BASE_URL",
	} {
		if !hasEmptyEnvironmentAssignment(environment, name) {
			t.Fatalf("deploy/.env.example must leave %s empty", name)
		}
	}
}

func TestDeployReadmeDocumentsFiveRequiredSecurityCategories(t *testing.T) {
	readme := readRepositoryFile(t, "deploy/README.md")
	if !strings.Contains(readme, "以下 5 类配置必须") {
		t.Fatal("deploy README does not explicitly identify the five required categories")
	}
	for _, name := range []string{
		"MYSQL_ROOT_PASSWORD", "MYSQL_PASSWORD", "JWT_SECRET", "CRYPTO_AES_KEY",
		"VIDEO_WORKFLOW_SIGNING_SECRET", "VIDEO_WORKFLOW_PUBLIC_BASE_URL",
	} {
		if !strings.Contains(readme, "`"+name+"`") {
			t.Fatalf("deploy README does not document %s", name)
		}
	}
}

func TestVideoWorkflowBackupUsesPrivatePermissions(t *testing.T) {
	script := readRepositoryFile(t, "deploy/video-workflow-assets-backup.sh")
	for _, required := range []string{`umask 077`, `chmod 700 "$BACKUP_DIR"`} {
		if !strings.Contains(script, required) {
			t.Fatalf("video workflow backup script is missing private permission guard %q", required)
		}
	}
}

func TestQuickSyncPreflightsResolvedMySQLSecretsWithoutSourcingDeployEnv(t *testing.T) {
	script := readRepositoryFile(t, "deploy/quick-remote-sync.sh")
	for _, required := range []string{
		`"internal/billing/engine.go"`,
		`docker compose --env-file "$env_file" -f - config --format json`,
		`config["services"]["production-secret-preflight"]`,
		`is_unsafe_mysql_password "$mysql_root_password"`,
		`is_unsafe_mysql_password "$mysql_password"`,
		`[ "$mysql_root_password" = "$mysql_password" ]`,
		`wc -c`,
		`[ "$byte_length" -lt 16 ]`,
		`必须至少 16 字节`,
		`*please_change*`,
		`*placeholder*`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("quick sync is missing production-secret guard %q", required)
		}
	}
	for _, unsafe := range []string{
		`. deploy/.env`,
		`source deploy/.env`,
		`. "$env_file"`,
		`source "$env_file"`,
	} {
		if strings.Contains(script, unsafe) {
			t.Fatalf("quick sync executes deploy environment file via %q", unsafe)
		}
	}
}

func TestDeployReadmeQuantifiesMySQLPasswordPolicy(t *testing.T) {
	readme := readRepositoryFile(t, "deploy/README.md")
	for _, required := range []string{"`MYSQL_ROOT_PASSWORD`", "`MYSQL_PASSWORD`", "至少 16 字节", "常见默认值", "两者必须不同"} {
		if !strings.Contains(readme, required) {
			t.Fatalf("deploy README is missing quantified MySQL password policy %q", required)
		}
	}
}

func hasEmptyEnvironmentAssignment(content, name string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == name+"=" {
			return true
		}
	}
	return false
}

func readRepositoryFile(t *testing.T, relative string) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test source path")
	}
	path := filepath.Join(filepath.Dir(source), "..", "..", filepath.FromSlash(relative))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
