#!/usr/bin/env bash
# Linux 本地预构建脚本(服务器上直接用 / WSL / macOS 均可)
#
# 用法:
#   bash deploy/build-local.sh            # 增量:只建缺失的 goose
#   bash deploy/build-local.sh --force    # 强制重建 goose
#
# 产物:
#   deploy/bin/gpt2api        linux/amd64 可执行(后端)
#   deploy/bin/goose          linux/amd64 可执行(迁移工具)
#   web/dist/                 前端 Vite 产物
#
# 这套产物 + deploy/Dockerfile 就可以离线构建镜像,无需容器再访问外网。

set -euo pipefail

FORCE=0
for arg in "$@"; do
    case "$arg" in
        -f|--force) FORCE=1 ;;
    esac
done

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "[build-local] repo  = $ROOT"

build_server() {
    echo "[build-local] backend = cross-build gpt2api (linux/amd64)"
    mkdir -p deploy/bin
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
        go build -trimpath -buildvcs=false -ldflags "-s -w" -o deploy/bin/gpt2api ./cmd/server
}

build_goose() {
    local goose="$ROOT/deploy/bin/goose"
    if [ "$FORCE" = "1" ] || [ ! -x "$goose" ]; then
        echo "[build-local] goose   = cross-build goose (tmp module)"
        local tmp
        tmp="$(mktemp -d)"
        pushd "$tmp" >/dev/null
        go mod init goose-wrapper >/dev/null 2>&1
        go get github.com/pressly/goose/v3/cmd/goose@v3.20.0 >/dev/null 2>&1
        GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
            go build -trimpath -buildvcs=false -ldflags "-s -w" -o "$goose" github.com/pressly/goose/v3/cmd/goose
        popd >/dev/null
        rm -rf "$tmp"
    else
        echo "[build-local] goose   = skip goose (exists). use --force to rebuild"
    fi
}

build_web() {
    echo "[build-local] frontend = npm run build (web)"
    pushd web >/dev/null
    if [ ! -d node_modules ]; then
        npm install --no-audit --no-fund --loglevel=error
    fi
    npm run build
    popd >/dev/null
}

wait_job() {
    local pid="$1"
    local name="$2"
    if wait "$pid"; then
        echo "[build-local] $name done"
        return 0
    fi
    echo "[build-local] $name failed" >&2
    return 1
}

echo "[build-local] build = backend/frontend parallel"
build_server &
pid_backend=$!
build_web &
pid_frontend=$!
build_goose &
pid_goose=$!

failed=0
wait_job "$pid_backend" "backend" || failed=1
wait_job "$pid_frontend" "frontend" || failed=1
wait_job "$pid_goose" "goose" || failed=1
[ "$failed" = "0" ] || exit 1

echo "[build-local] done. artifacts:"
ls -lh deploy/bin/gpt2api deploy/bin/goose web/dist/index.html
