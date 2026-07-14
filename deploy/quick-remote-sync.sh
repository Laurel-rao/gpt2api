#!/usr/bin/env bash
# 快速同步当前工作区改动到远端并重建服务。
# 适合这类小步发布: 本地测试/构建 -> rsync 指定文件 -> 远端静态编译 -> Docker 重建 -> 健康检查。

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOCAL_ENV_FILE="${GPT2API_REMOTE_ENV_FILE:-$ROOT/deploy/remote-release.env}"

if [ -f "$LOCAL_ENV_FILE" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$LOCAL_ENV_FILE"
  set +a
fi

REMOTE_HOST="${GPT2API_REMOTE_HOST:-123.207.53.152}"
REMOTE_USER="${GPT2API_REMOTE_USER:-root}"
REMOTE_PORT="${GPT2API_REMOTE_PORT:-22}"
REMOTE_DIR="${GPT2API_REMOTE_DIR:-/opt/gpt2api}"
HTTP_PORT="${GPT2API_HTTP_PORT:-8080}"
HEALTH_URL="${GPT2API_HEALTH_URL:-http://127.0.0.1:$HTTP_PORT}"
PUBLIC_URL="${GPT2API_PUBLIC_URL:-http://$REMOTE_HOST:$HTTP_PORT/}"
RUN_LOCAL_BUILD="${GPT2API_RUN_LOCAL_BUILD:-1}"
RUN_REMOTE_TESTS="${GPT2API_RUN_REMOTE_TESTS:-1}"

FILES=(
  ".gitignore"
  "deploy/quick-remote-sync.sh"
  "deploy/README.md"
  "cmd/server/main.go"
  "cmd/server/video_workflow_adapters.go"
  "cmd/server/video_workflow_adapters_test.go"
  "configs/config.example.yaml"
  "deploy/Dockerfile"
  "deploy/.env.example"
  "deploy/docker-compose.yml"
  "deploy/nginx.conf"
  "deploy/video-workflow-assets-backup.sh"
  "internal/billing/engine.go"
  "internal/config/config.go"
  "internal/config/deploy_security_test.go"
  "internal/config/video_workflow_test.go"
  "internal/middleware/logger.go"
  "internal/middleware/logger_test.go"
  "internal/videogen/client.go"
  "internal/videogen/client_test.go"
  "internal/videoworkflow"
  "internal/rbac/menu.go"
  "internal/rbac/permission.go"
  "internal/server/router.go"
  "internal/server/readiness_test.go"
  "internal/settings/service.go"
  "sql/migrations/20260711000100_video_workflow.sql"
  "sql/migrations/20260712000100_video_workflow_runtime_fencing.sql"
  "sql/migrations/20260713000100_videogen_workflow_models.sql"
  "sql/migrations/20260714000100_video_workflow_revisions.sql"
  "web/package.json"
  "web/package-lock.json"
  "web/vitest.config.ts"
  "web/e2e/video-canvas"
  "web/src/api/videoWorkflow.spec.ts"
  "web/src/api/videoWorkflow.ts"
  "web/src/components/video-workflow"
  "web/src/router/index.ts"
  "web/src/test"
  "web/src/utils/videoWorkflowAsync.ts"
  "web/src/utils/videoWorkflowAsync.spec.ts"
  "web/src/utils/videoWorkflowGraph.ts"
  "web/src/utils/videoWorkflowGraph.spec.ts"
  "web/src/views/personal/VideoWorkflows.vue"
  "docs/plans/video-workflow-requirements.md"
  "docs/plans/video-workflow-verification.md"
  "docs/ui-prototypes/video-workflow"
)

usage() {
  cat <<'EOF'
用法:
  bash deploy/quick-remote-sync.sh [选项] [文件...]

默认动作:
  1. 用新 Compose 配置预检远端 deploy/.env
  2. 本地前端单元测试、E2E 契约测试与 npm run build
  3. 本地测试视频画布、视频渠道恢复、配置、日志、路由与服务启动包
  4. 同步默认 FILES 与 web/dist 到远端
  5. 本地交叉编译 Linux/amd64；远端有 Go 时追加远端测试与静态编译
  6. 远端 docker compose build/up --wait
  7. 检查 /healthz、/readyz 与容器状态

选项:
  --host <host>          远端主机，默认读取 deploy/remote-release.env
  --user <user>          SSH 用户，默认 root
  --port <port>          SSH 端口，默认 22
  --remote-dir <dir>     远端项目目录，默认 /opt/gpt2api
  --http-port <port>     健康检查端口，默认 8080
  --skip-local-build     跳过本地构建和本地测试
  --skip-remote-tests    跳过远端 go test
  -h, --help             显示帮助

追加参数:
  传入文件路径后，会在默认 FILES 基础上额外同步这些文件。

环境变量:
  GPT2API_HEALTH_URL 可覆盖健康检查 origin，例如 https://ai.reeko.net.cn:8000
  GPT2API_PUBLIC_URL 可覆盖发布完成后显示的访问地址

示例:
  bash deploy/quick-remote-sync.sh
  bash deploy/quick-remote-sync.sh docs/ui-prototypes/video-workflow/video-canvas-main-v1.png
  GPT2API_REMOTE_HOST=1.2.3.4 bash deploy/quick-remote-sync.sh --skip-local-build
EOF
}

log() {
  printf '[quick-sync] %s\n' "$*"
}

die() {
  printf '[quick-sync] ERROR: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "缺少命令: $1"
}

ssh_remote() {
  ssh -p "$REMOTE_PORT" -o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=10 "${REMOTE_USER}@${REMOTE_HOST}" "$@"
}

rsync_remote() {
  rsync -az --partial -e "ssh -p $REMOTE_PORT -o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=10" "$@"
}

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --host)
        [ "$#" -ge 2 ] || die "--host 缺少参数"
        REMOTE_HOST="$2"
        shift 2
        ;;
      --user)
        [ "$#" -ge 2 ] || die "--user 缺少参数"
        REMOTE_USER="$2"
        shift 2
        ;;
      --port)
        [ "$#" -ge 2 ] || die "--port 缺少参数"
        REMOTE_PORT="$2"
        shift 2
        ;;
      --remote-dir)
        [ "$#" -ge 2 ] || die "--remote-dir 缺少参数"
        REMOTE_DIR="$2"
        shift 2
        ;;
      --http-port)
        [ "$#" -ge 2 ] || die "--http-port 缺少参数"
        HTTP_PORT="$2"
        shift 2
        ;;
      --skip-local-build)
        RUN_LOCAL_BUILD=0
        shift
        ;;
      --skip-remote-tests)
        RUN_REMOTE_TESTS=0
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        FILES+=("$1")
        shift
        ;;
    esac
  done
}

dedupe_files() {
  local unique=()
  local seen=" "
  local file
  for file in "${FILES[@]}"; do
    case "$seen" in
      *" $file "*) ;;
      *)
        [ -e "$ROOT/$file" ] || die "文件不存在: $file"
        unique+=("$file")
        seen="$seen$file "
        ;;
    esac
  done
  FILES=("${unique[@]}")
}

run_local_checks() {
  if [ "$RUN_LOCAL_BUILD" != "1" ]; then
    log "跳过本地构建和测试"
    return
  fi
  log "本地前端构建"
  (cd "$ROOT/web" && npm run test:unit -- --run && npm run e2e:video-canvas:contract && npm run build)
  log "本地 Go 测试"
  (cd "$ROOT" && go test ./internal/videoworkflow ./internal/videogen ./internal/config ./internal/middleware ./internal/server ./internal/settings ./internal/rbac ./cmd/server)
  log "本地交叉编译 Linux/amd64 服务端"
  mkdir -p "$ROOT/deploy/bin"
  (cd "$ROOT" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "-s -w" -o deploy/bin/gpt2api ./cmd/server)
}

preflight_remote_configuration() {
  log "预检远端 deploy/.env 与新 Compose 配置"
  ssh_remote "cd '$REMOTE_DIR' && bash -s" <<'REMOTE_SCRIPT'
set -euo pipefail

env_file="deploy/.env"
test -f "$env_file" || {
  echo '[quick-sync] ERROR: 缺少远端 deploy/.env' >&2
  exit 1
}

resolved_config="$(mktemp)"
trap 'rm -f "$resolved_config"' EXIT
docker compose --env-file "$env_file" -f - config --format json >"$resolved_config" <<'COMPOSE_YAML'
services:
  production-secret-preflight:
    image: scratch
    environment:
      MYSQL_ROOT_PASSWORD: ${MYSQL_ROOT_PASSWORD-}
      MYSQL_PASSWORD: ${MYSQL_PASSWORD-}
COMPOSE_YAML

python_bin="$(command -v python3 || command -v python || true)"
if [ -z "$python_bin" ] && [ -x /usr/libexec/platform-python ]; then
  python_bin=/usr/libexec/platform-python
fi
[ -n "$python_bin" ] || {
  echo '[quick-sync] ERROR: 远端缺少 Python，无法解析 Compose 预检结果' >&2
  exit 1
}
mapfile -t mysql_passwords < <("$python_bin" - "$resolved_config" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as fh:
    config = json.load(fh)
environment = config["services"]["production-secret-preflight"].get("environment", {})
print(environment.get("MYSQL_ROOT_PASSWORD", ""))
print(environment.get("MYSQL_PASSWORD", ""))
PY
)
mysql_root_password="${mysql_passwords[0]:-}"
mysql_password="${mysql_passwords[1]:-}"

is_unsafe_mysql_password() {
  local value normalized byte_length
  byte_length="$(printf '%s' "$1" | wc -c | tr -d '[:space:]')"
  if [ "$byte_length" -lt 16 ]; then
    return 0
  fi
  value="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')"
  normalized="${value//-/_}"
  normalized="${normalized// /_}"
  case "$normalized" in
    ''|root|gpt2api|mysql|password|admin|administrator|123456|12345678|123456789|qwerty|letmein|default|\
    *change_me*|*changeme*|*please_change*|*placeholder*|*replace_me*|*your_password*|*example*|*default_password*)
      return 0
      ;;
  esac
  return 1
}

if is_unsafe_mysql_password "$mysql_root_password"; then
  echo '[quick-sync] ERROR: MYSQL_ROOT_PASSWORD 必须至少 16 字节且不得使用常见占位/默认值' >&2
  exit 1
fi
if is_unsafe_mysql_password "$mysql_password"; then
  echo '[quick-sync] ERROR: MYSQL_PASSWORD 必须至少 16 字节且不得使用常见占位/默认值' >&2
  exit 1
fi
if [ "$mysql_root_password" = "$mysql_password" ]; then
  echo '[quick-sync] ERROR: MYSQL_ROOT_PASSWORD 与 MYSQL_PASSWORD 必须使用不同值' >&2
  exit 1
fi
REMOTE_SCRIPT
  ssh_remote "cd '$REMOTE_DIR' && test -f deploy/.env && docker compose --env-file deploy/.env -f - config --quiet" \
    < "$ROOT/deploy/docker-compose.yml"
}

sync_sources() {
  local target="${REMOTE_USER}@${REMOTE_HOST}:$REMOTE_DIR/"
  log "同步源码到 $target"
  (cd "$ROOT" && rsync_remote --relative "${FILES[@]}" "$target")
  log "同步 web/dist 到远端"
  rsync_remote --delete "$ROOT/web/dist/" "${REMOTE_USER}@${REMOTE_HOST}:$REMOTE_DIR/web/dist/"
  [ -x "$ROOT/deploy/bin/gpt2api" ] || die "缺少本地 Linux 服务端产物: deploy/bin/gpt2api"
  log "同步 Linux/amd64 服务端产物"
  rsync_remote "$ROOT/deploy/bin/gpt2api" "${REMOTE_USER}@${REMOTE_HOST}:$REMOTE_DIR/deploy/bin/gpt2api"
}

remote_deploy() {
  log "远端编译、重建并健康检查"
  ssh_remote "bash -s" <<EOF
set -euo pipefail
cd '$REMOTE_DIR'
test -f deploy/.env || { echo '[quick-sync] ERROR: 缺少远端 deploy/.env' >&2; exit 1; }
if command -v go >/dev/null 2>&1 && [ '$RUN_REMOTE_TESTS' = '1' ]; then
  go test ./internal/videoworkflow ./internal/videogen ./internal/config ./internal/middleware ./internal/server ./internal/settings ./internal/rbac ./cmd/server
fi
if command -v go >/dev/null 2>&1; then
  CGO_ENABLED=0 go build -buildvcs=false -o deploy/bin/gpt2api ./cmd/server
else
  echo '[quick-sync] 远端未安装 Go，使用已通过本地测试的 Linux/amd64 交叉编译产物'
fi
test -x deploy/bin/gpt2api
file deploy/bin/gpt2api
docker compose --env-file deploy/.env -f deploy/docker-compose.yml build server
docker compose --env-file deploy/.env -f deploy/docker-compose.yml up -d --wait --wait-timeout 120 server nginx
# server 容器重建后 nginx 可能仍缓存旧容器 IP；重启以刷新 Docker DNS。
docker compose --env-file deploy/.env -f deploy/docker-compose.yml restart nginx
healthy=0
for attempt in 1 2 3 4 5; do
  if curl -fsS "${HEALTH_URL%/}/healthz" \
    && curl -fsS "${HEALTH_URL%/}/readyz"; then
    healthy=1
    break
  fi
  sleep 2
done
[ "\$healthy" = '1' ] || { docker compose --env-file deploy/.env -f deploy/docker-compose.yml ps; exit 1; }
docker compose --env-file deploy/.env -f deploy/docker-compose.yml ps
EOF
}

main() {
  parse_args "$@"
  require_cmd ssh
  require_cmd rsync
  if [ "$RUN_LOCAL_BUILD" = "1" ]; then
    require_cmd npm
    require_cmd go
  fi
  dedupe_files
  log "目标: ${REMOTE_USER}@${REMOTE_HOST}:${REMOTE_DIR}"
  preflight_remote_configuration
  run_local_checks
  sync_sources
  remote_deploy
  log "发布完成: $PUBLIC_URL"
}

main "$@"
