#!/usr/bin/env bash
# 一键更新脚本:
#   1. 可选 git pull --ff-only
#   2. 预编译后端/前端产物
#   3. 重建 server 镜像并拉起 docker compose
#   4. 重启 nginx,让新静态包和 nginx.conf 立即生效

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$ROOT/deploy"

PULL=0
ALLOW_DIRTY=0
FORCE=0
SKIP_BUILD=0
FRONTEND_ONLY=0
SHOW_LOGS=0
SKIP_HEALTH=0

usage() {
    cat <<'EOF'
用法:
  bash update.sh [选项]

选项:
  --pull          更新前执行 git pull --ff-only
  --allow-dirty   配合 --pull 使用,允许工作区有未提交改动
  --force         透传给 deploy/build-local.sh,强制重建 goose
  --skip-build    跳过宿主预编译,直接使用现有 deploy/bin 与 web/dist
  --frontend-only 只构建前端并重启 nginx
  --logs          更新完成后跟随 server 日志
  --skip-health   跳过 /healthz 检查
  -h, --help      显示帮助

常用:
  bash update.sh
  bash update.sh --pull
  bash update.sh --pull --logs
  bash update.sh --frontend-only
EOF
}

log() {
    echo "[update] $*"
}

die() {
    echo "[update] ERROR: $*" >&2
    exit 1
}

need_cmd() {
    command -v "$1" >/dev/null 2>&1 || die "缺少命令: $1"
}

compose() {
    (cd "$DEPLOY_DIR" && docker compose "$@")
}

parse_http_port() {
    local env_file="$DEPLOY_DIR/.env"
    local port=""
    if [ -f "$env_file" ]; then
        port="$(awk -F= '
            /^[[:space:]]*HTTP_PORT[[:space:]]*=/ {
                v=$2
                gsub(/^[[:space:]]+|[[:space:]]+$/, "", v)
                gsub(/^["'\'']|["'\'']$/, "", v)
                print v
            }
        ' "$env_file" | tail -n 1)"
    fi
    echo "${port:-8080}"
}

health_check() {
    [ "$SKIP_HEALTH" = "0" ] || return 0
    need_cmd curl

    local port
    port="$(parse_http_port)"
    local url="http://127.0.0.1:${port}/healthz"

    log "health check: $url"
    for _ in $(seq 1 30); do
        if curl -fsS "$url" >/dev/null; then
            log "health check ok"
            return 0
        fi
        sleep 2
    done
    die "健康检查失败,请查看: cd deploy && docker compose logs --tail=200 server"
}

for arg in "$@"; do
    case "$arg" in
        --pull) PULL=1 ;;
        --allow-dirty) ALLOW_DIRTY=1 ;;
        --force|-f) FORCE=1 ;;
        --skip-build) SKIP_BUILD=1 ;;
        --frontend-only) FRONTEND_ONLY=1 ;;
        --logs) SHOW_LOGS=1 ;;
        --skip-health) SKIP_HEALTH=1 ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            usage
            die "未知参数: $arg"
            ;;
    esac
done

cd "$ROOT"

need_cmd git
need_cmd docker

if ! docker compose version >/dev/null 2>&1; then
    die "当前 Docker 不支持 'docker compose',请安装 docker compose v2"
fi

[ -f "$DEPLOY_DIR/docker-compose.yml" ] || die "找不到 deploy/docker-compose.yml"

log "repo: $ROOT"

if [ "$PULL" = "1" ]; then
    if [ "$ALLOW_DIRTY" = "0" ] && ! git diff --quiet; then
        die "工作区有未提交改动。确认要带着本地改动拉代码时,使用 --pull --allow-dirty"
    fi
    if [ "$ALLOW_DIRTY" = "0" ] && ! git diff --cached --quiet; then
        die "暂存区有未提交改动。确认要带着本地改动拉代码时,使用 --pull --allow-dirty"
    fi
    log "git pull --ff-only"
    git pull --ff-only
fi

if [ "$FRONTEND_ONLY" = "1" ]; then
    if [ "$SKIP_BUILD" = "0" ]; then
        need_cmd npm
        log "build frontend"
        (cd "$ROOT/web" && npm install --no-audit --no-fund --loglevel=error && npm run build)
    else
        log "skip frontend build"
    fi
    log "restart nginx"
    compose up -d nginx
    compose restart nginx
    health_check
    log "done"
    exit 0
fi

if [ "$SKIP_BUILD" = "0" ]; then
    build_args=()
    [ "$FORCE" = "0" ] || build_args+=(--force)
    log "build local artifacts"
    if [ "${#build_args[@]}" -gt 0 ]; then
        bash "$ROOT/deploy/build-local.sh" "${build_args[@]}"
    else
        bash "$ROOT/deploy/build-local.sh"
    fi
else
    log "skip local build"
fi

log "build server image"
compose build server

log "up compose services"
compose up -d

log "restart nginx"
compose restart nginx

health_check

log "compose status"
compose ps

if [ "$SHOW_LOGS" = "1" ]; then
    log "tail server logs"
    compose logs -f server
fi

log "done"
