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

REMOTE_HOST="${GPT2API_REMOTE_HOST:-43.128.120.182}"
REMOTE_USER="${GPT2API_REMOTE_USER:-root}"
REMOTE_PORT="${GPT2API_REMOTE_PORT:-22}"
REMOTE_DIR="${GPT2API_REMOTE_DIR:-/opt/gpt2api}"
HTTP_PORT="${GPT2API_HTTP_PORT:-8080}"
RUN_LOCAL_BUILD="${GPT2API_RUN_LOCAL_BUILD:-1}"
RUN_REMOTE_TESTS="${GPT2API_RUN_REMOTE_TESTS:-1}"

FILES=(
  "internal/ecommerce/dao.go"
  "internal/ecommerce/handler.go"
  "internal/ecommerce/runner.go"
  "internal/server/router.go"
  "web/src/api/ecommerce.ts"
  "web/src/views/personal/EcommerceWorkbench.vue"
)

usage() {
  cat <<'EOF'
用法:
  bash deploy/quick-remote-sync.sh [选项] [文件...]

默认动作:
  1. 本地 npm run build
  2. 本地 go test ./internal/ecommerce ./internal/videogen ./internal/settings ./cmd/server
  3. 同步默认 FILES 与 web/dist 到远端
  4. 远端 go test
  5. 远端 CGO_ENABLED=0 go build -buildvcs=false
  6. 远端 docker compose build/up/restart
  7. 健康检查 /healthz

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

示例:
  bash deploy/quick-remote-sync.sh
  bash deploy/quick-remote-sync.sh web/src/views/personal/EcommerceMobileWorkbench.vue
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
  ssh -p "$REMOTE_PORT" -o BatchMode=yes -o StrictHostKeyChecking=no "${REMOTE_USER}@${REMOTE_HOST}" "$@"
}

rsync_remote() {
  rsync -az --partial -e "ssh -p $REMOTE_PORT -o BatchMode=yes -o StrictHostKeyChecking=no" "$@"
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
  (cd "$ROOT/web" && npm run build)
  log "本地 Go 测试"
  (cd "$ROOT" && go test ./internal/ecommerce ./internal/videogen ./internal/settings ./cmd/server)
}

sync_sources() {
  local target="${REMOTE_USER}@${REMOTE_HOST}:$REMOTE_DIR/"
  log "同步源码到 $target"
  (cd "$ROOT" && rsync_remote --relative "${FILES[@]}" "$target")
  log "同步 web/dist 到远端"
  rsync_remote --delete "$ROOT/web/dist/" "${REMOTE_USER}@${REMOTE_HOST}:$REMOTE_DIR/web/dist/"
}

remote_deploy() {
  log "远端编译、重建并健康检查"
  ssh_remote "bash -s" <<EOF
set -euo pipefail
cd '$REMOTE_DIR'
if [ '$RUN_REMOTE_TESTS' = '1' ]; then
  go test ./internal/ecommerce ./internal/videogen ./internal/settings ./cmd/server
fi
CGO_ENABLED=0 go build -buildvcs=false -o deploy/bin/gpt2api ./cmd/server
file deploy/bin/gpt2api
docker compose -f deploy/docker-compose.yml build server
docker compose -f deploy/docker-compose.yml up -d server nginx
docker compose -f deploy/docker-compose.yml restart nginx
curl -fsS "http://127.0.0.1:$HTTP_PORT/healthz"
EOF
}

main() {
  parse_args "$@"
  require_cmd ssh
  require_cmd rsync
  require_cmd npm
  require_cmd go
  dedupe_files
  log "目标: ${REMOTE_USER}@${REMOTE_HOST}:${REMOTE_DIR}"
  run_local_checks
  sync_sources
  remote_deploy
  log "发布完成: http://${REMOTE_HOST}:${HTTP_PORT}/"
}

main "$@"
