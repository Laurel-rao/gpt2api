#!/usr/bin/env bash
# 本地预编译 -> 上传远端 -> 远端备份 -> 重建 server -> 健康检查
# 支持:
#   1) deploy   发布新版本(默认先做本地 build-local)
#   2) list     查看远端备份列表
#   3) rollback 按备份 ID 回滚，可选恢复数据库

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

REMOTE_HOST="${GPT2API_REMOTE_HOST:-43.134.21.160}"
REMOTE_USER="${GPT2API_REMOTE_USER:-root}"
REMOTE_PORT="${GPT2API_REMOTE_PORT:-22}"
REMOTE_DIR="${GPT2API_REMOTE_DIR:-/opt/gpt2api}"
HTTP_PORT="${GPT2API_HTTP_PORT:-8080}"
KEEP_BACKUPS="${GPT2API_KEEP_BACKUPS:-10}"
SKIP_BUILD=0
SKIP_DB_BACKUP=0
RESTORE_DB=0
FORCE_BUILD=0

BUNDLE_ITEMS=(
  "deploy/bin/gpt2api"
  "deploy/bin/goose"
  "deploy/Dockerfile"
  "deploy/docker-compose.yml"
  "deploy/entrypoint.sh"
  "deploy/nginx.conf"
  "sql"
  "web/dist"
)

usage() {
  cat <<'EOF'
用法:
  bash deploy/remote-release.sh deploy [选项]
  bash deploy/remote-release.sh list [选项]
  bash deploy/remote-release.sh rollback <backup_id> [选项]

命令:
  deploy              本地编译后发布到远端；发布前自动备份远端应用文件和 MySQL
  list                查看远端备份列表
  rollback <backup_id>  回滚到指定备份；可加 --restore-db 一并恢复数据库

选项:
  --host <host>         远端主机，默认 43.134.21.160
  --user <user>         SSH 用户，默认 root
  --port <port>         SSH 端口，默认 22
  --remote-dir <dir>    远端项目目录，默认 /opt/gpt2api
  --http-port <port>    健康检查端口，默认 8080
  --keep <count>        远端保留最近 N 份备份，默认 10
  --skip-build          deploy 时跳过本地 build-local
  --skip-db-backup      deploy/rollback 前跳过当前数据库备份
  --restore-db          rollback 时恢复同名数据库备份
  --force-build         deploy 时透传给 build-local.sh --force
  -h, --help            显示帮助

环境变量:
  GPT2API_REMOTE_HOST / GPT2API_REMOTE_USER / GPT2API_REMOTE_PORT
  GPT2API_REMOTE_DIR / GPT2API_HTTP_PORT / GPT2API_KEEP_BACKUPS

示例:
  bash deploy/remote-release.sh deploy
  bash deploy/remote-release.sh list
  bash deploy/remote-release.sh rollback 20260425-111530-b5c3e10
  bash deploy/remote-release.sh rollback 20260425-111530-b5c3e10 --restore-db
EOF
}

log() {
  printf '[remote-release] %s\n' "$*"
}

die() {
  printf '[remote-release] ERROR: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "缺少命令: $1"
}

ssh_base() {
  ssh -p "$REMOTE_PORT" -o BatchMode=yes -o StrictHostKeyChecking=no "${REMOTE_USER}@${REMOTE_HOST}" "$@"
}

scp_base() {
  scp -P "$REMOTE_PORT" -o BatchMode=yes -o StrictHostKeyChecking=no "$@"
}

parse_common_args() {
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
      --keep)
        [ "$#" -ge 2 ] || die "--keep 缺少参数"
        KEEP_BACKUPS="$2"
        shift 2
        ;;
      --skip-build)
        SKIP_BUILD=1
        shift
        ;;
      --skip-db-backup)
        SKIP_DB_BACKUP=1
        shift
        ;;
      --restore-db)
        RESTORE_DB=1
        shift
        ;;
      --force-build)
        FORCE_BUILD=1
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        return 1
        ;;
    esac
  done
  return 0
}

assert_local_artifacts() {
  local item
  for item in "${BUNDLE_ITEMS[@]}"; do
    [ -e "$ROOT/$item" ] || die "缺少本地产物: $item"
  done
}

run_local_build() {
  local args=()
  if [ "$FORCE_BUILD" = "1" ]; then
    args+=(--force)
  fi
  log "执行本地预编译: deploy/build-local.sh ${args[*]:-}"
  (
    cd "$ROOT"
    bash deploy/build-local.sh "${args[@]}"
  )
}

make_bundle() {
  local release_id="$1"
  local bundle_file="$2"
  assert_local_artifacts
  COPYFILE_DISABLE=1 tar -C "$ROOT" -czf "$bundle_file" "${BUNDLE_ITEMS[@]}"
  log "已打包发布包: $bundle_file"
}

get_git_meta() {
  local branch commit short
  if git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    branch="$(git -C "$ROOT" branch --show-current 2>/dev/null || true)"
    commit="$(git -C "$ROOT" rev-parse HEAD 2>/dev/null || true)"
    short="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || true)"
  else
    branch=""
    commit=""
    short=""
  fi
  printf '%s\n%s\n%s\n' "${branch:-unknown}" "${commit:-unknown}" "${short:-manual}"
}

remote_preflight() {
  ssh_base "test -d '$REMOTE_DIR' && test -f '$REMOTE_DIR/deploy/docker-compose.yml' && test -f '$REMOTE_DIR/deploy/.env'"
}

list_backups() {
  log "远端备份列表 ${REMOTE_USER}@${REMOTE_HOST}:${REMOTE_DIR}"
  ssh_base "bash -s -- '$REMOTE_DIR'" <<'EOF'
set -euo pipefail
REMOTE_DIR="$1"
APP_DIR="$REMOTE_DIR/deploy/updates/app"
DB_DIR="$REMOTE_DIR/deploy/updates/db"
META_DIR="$REMOTE_DIR/deploy/updates/meta"

if [ ! -d "$APP_DIR" ]; then
  echo "(暂无备份)"
  exit 0
fi

for file in $(find "$APP_DIR" -maxdepth 1 -type f -name '*.tar.gz' | sort -r); do
  id="$(basename "$file" .tar.gz)"
  size="$(du -h "$file" | awk '{print $1}')"
  db="no"
  [ -f "$DB_DIR/$id.sql.gz" ] && db="yes"
  created="-"
  source_ref="-"
  if [ -f "$META_DIR/$id.env" ]; then
    created="$(grep '^CREATED_AT=' "$META_DIR/$id.env" | head -n1 | cut -d= -f2- || true)"
    source_ref="$(grep '^SOURCE_REF=' "$META_DIR/$id.env" | head -n1 | cut -d= -f2- || true)"
  fi
  printf '%s\tapp=%s\tdb=%s\tcreated=%s\tsource=%s\n' "$id" "$size" "$db" "$created" "$source_ref"
done
EOF
}

deploy_release() {
  require_cmd ssh
  require_cmd scp
  require_cmd tar
  require_cmd gzip

  remote_preflight || die "远端预检失败，请确认目录和 deploy/.env 存在: ${REMOTE_DIR}"

  if [ "$SKIP_BUILD" != "1" ]; then
    run_local_build
  fi

  local git_branch git_commit git_short release_id tmp_dir bundle_file remote_bundle
  local git_meta
  git_meta="$(get_git_meta)"
  git_branch="$(printf '%s\n' "$git_meta" | sed -n '1p')"
  git_commit="$(printf '%s\n' "$git_meta" | sed -n '2p')"
  git_short="$(printf '%s\n' "$git_meta" | sed -n '3p')"
  release_id="$(date '+%Y%m%d-%H%M%S')-${git_short}"
  tmp_dir="$(mktemp -d)"
  bundle_file="$tmp_dir/gpt2api-${release_id}.tar.gz"
  remote_bundle="/tmp/gpt2api-${release_id}.tar.gz"

  trap "rm -rf '$tmp_dir'" EXIT

  make_bundle "$release_id" "$bundle_file"

  log "上传发布包到 ${REMOTE_USER}@${REMOTE_HOST}:${remote_bundle}"
  scp_base "$bundle_file" "${REMOTE_USER}@${REMOTE_HOST}:${remote_bundle}"

  log "远端开始备份并发布 release_id=${release_id}"
  ssh_base "bash -s -- '$REMOTE_DIR' '$remote_bundle' '$release_id' '$HTTP_PORT' '$KEEP_BACKUPS' '$git_branch' '$git_commit' '$SKIP_DB_BACKUP'" <<'EOF'
set -euo pipefail

REMOTE_DIR="$1"
REMOTE_BUNDLE="$2"
RELEASE_ID="$3"
HTTP_PORT="$4"
KEEP_BACKUPS="$5"
SOURCE_BRANCH="$6"
SOURCE_COMMIT="$7"
SKIP_DB_BACKUP="$8"

APP_DIR="$REMOTE_DIR/deploy/updates/app"
DB_DIR="$REMOTE_DIR/deploy/updates/db"
META_DIR="$REMOTE_DIR/deploy/updates/meta"
APP_BACKUP="$APP_DIR/$RELEASE_ID.tar.gz"
DB_BACKUP="$DB_DIR/$RELEASE_ID.sql.gz"
META_FILE="$META_DIR/$RELEASE_ID.env"
CREATED_AT="$(date '+%F %T %Z')"
SOURCE_REF="${SOURCE_BRANCH}@${SOURCE_COMMIT}"

cleanup_remote_bundle() {
  rm -f "$REMOTE_BUNDLE"
}
trap cleanup_remote_bundle EXIT

backup_runtime() {
  mkdir -p "$APP_DIR" "$DB_DIR" "$META_DIR"
  cd "$REMOTE_DIR"
  tar -czf "$APP_BACKUP" \
    --ignore-failed-read \
    deploy/.env \
    deploy/Dockerfile \
    deploy/docker-compose.yml \
    deploy/entrypoint.sh \
    deploy/nginx.conf \
    deploy/bin \
    configs \
    sql \
    web/dist

  if [ "$SKIP_DB_BACKUP" != "1" ]; then
    (
      cd "$REMOTE_DIR/deploy"
      docker compose exec -T mysql sh -c \
        'exec mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" --single-transaction --quick "$MYSQL_DATABASE"' \
        </dev/null
    ) | gzip -1 > "$DB_BACKUP"
  fi

  {
    echo "RELEASE_ID=$RELEASE_ID"
    echo "CREATED_AT=$CREATED_AT"
    echo "SOURCE_REF=$SOURCE_REF"
    echo "APP_BACKUP=$APP_BACKUP"
    echo "DB_BACKUP=$DB_BACKUP"
  } > "$META_FILE"
}

restore_app_backup() {
  cd "$REMOTE_DIR"
  rm -rf deploy/bin web/dist configs sql
  tar -xzf "$APP_BACKUP" -C "$REMOTE_DIR"
}

health_check() {
  local i
  for i in $(seq 1 30); do
    if curl -fsS "http://127.0.0.1:${HTTP_PORT}/healthz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  return 1
}

rebuild_and_up() {
  cd "$REMOTE_DIR/deploy"
  docker compose build server
  docker compose up -d server nginx
  docker compose restart nginx
}

prune_old_backups() {
  local old
  old="$(find "$APP_DIR" -maxdepth 1 -type f -name '*.tar.gz' | sort -r | awk "NR>${KEEP_BACKUPS}")"
  [ -n "$old" ] || return 0
  while IFS= read -r file; do
    [ -n "$file" ] || continue
    id="$(basename "$file" .tar.gz)"
    rm -f "$APP_DIR/$id.tar.gz" "$DB_DIR/$id.sql.gz" "$META_DIR/$id.env"
  done <<EOF_OLD
$old
EOF_OLD
}

backup_runtime
cd "$REMOTE_DIR"
rm -rf deploy/bin web/dist sql
tar -xzf "$REMOTE_BUNDLE" -C "$REMOTE_DIR"

if ! rebuild_and_up; then
  echo "[remote-release] rebuild/up failed, restoring $APP_BACKUP" >&2
  restore_app_backup
  rebuild_and_up || true
  health_check || true
  exit 1
fi

if ! health_check; then
  echo "[remote-release] health check failed, restoring $APP_BACKUP" >&2
  restore_app_backup
  rebuild_and_up
  health_check || true
  exit 1
fi

prune_old_backups
echo "[remote-release] deploy ok release_id=$RELEASE_ID"
echo "[remote-release] app_backup=$APP_BACKUP"
if [ "$SKIP_DB_BACKUP" != "1" ]; then
  echo "[remote-release] db_backup=$DB_BACKUP"
fi
EOF
}

rollback_release() {
  local backup_id="$1"
  require_cmd ssh

  remote_preflight || die "远端预检失败，请确认目录和 deploy/.env 存在: ${REMOTE_DIR}"

  log "远端回滚 backup_id=${backup_id}"
  ssh_base "bash -s -- '$REMOTE_DIR' '$backup_id' '$HTTP_PORT' '$RESTORE_DB' '$SKIP_DB_BACKUP'" <<'EOF'
set -euo pipefail

REMOTE_DIR="$1"
BACKUP_ID="$2"
HTTP_PORT="$3"
RESTORE_DB="$4"
SKIP_DB_BACKUP="$5"

APP_DIR="$REMOTE_DIR/deploy/updates/app"
DB_DIR="$REMOTE_DIR/deploy/updates/db"
META_DIR="$REMOTE_DIR/deploy/updates/meta"
APP_BACKUP="$APP_DIR/$BACKUP_ID.tar.gz"
DB_BACKUP="$DB_DIR/$BACKUP_ID.sql.gz"

[ -f "$APP_BACKUP" ] || {
  echo "[remote-release] missing app backup: $APP_BACKUP" >&2
  exit 1
}

if [ "$RESTORE_DB" = "1" ] && [ ! -f "$DB_BACKUP" ]; then
  echo "[remote-release] missing db backup: $DB_BACKUP" >&2
  exit 1
fi

CURRENT_ID="rollback-pre-$(date '+%Y%m%d-%H%M%S')"
CURRENT_APP="$APP_DIR/$CURRENT_ID.tar.gz"
CURRENT_DB="$DB_DIR/$CURRENT_ID.sql.gz"
CURRENT_META="$META_DIR/$CURRENT_ID.env"

backup_current() {
  mkdir -p "$APP_DIR" "$DB_DIR" "$META_DIR"
  cd "$REMOTE_DIR"
  tar -czf "$CURRENT_APP" \
    --ignore-failed-read \
    deploy/.env \
    deploy/Dockerfile \
    deploy/docker-compose.yml \
    deploy/entrypoint.sh \
    deploy/nginx.conf \
    deploy/bin \
    configs \
    sql \
    web/dist

  if [ "$SKIP_DB_BACKUP" != "1" ]; then
    (
      cd "$REMOTE_DIR/deploy"
      docker compose exec -T mysql sh -c \
        'exec mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" --single-transaction --quick "$MYSQL_DATABASE"' \
        </dev/null
    ) | gzip -1 > "$CURRENT_DB"
  fi

  {
    echo "RELEASE_ID=$CURRENT_ID"
    echo "CREATED_AT=$(date '+%F %T %Z')"
    echo "SOURCE_REF=rollback-pre"
    echo "APP_BACKUP=$CURRENT_APP"
    echo "DB_BACKUP=$CURRENT_DB"
  } > "$CURRENT_META"
}

restore_app() {
  cd "$REMOTE_DIR"
  rm -rf deploy/bin web/dist configs sql
  tar -xzf "$APP_BACKUP" -C "$REMOTE_DIR"
}

restore_db() {
  gunzip -c "$DB_BACKUP" | (
    cd "$REMOTE_DIR/deploy"
    docker compose exec -T mysql sh -c \
      'exec mysql -uroot -p"$MYSQL_ROOT_PASSWORD" "$MYSQL_DATABASE"'
  )
}

health_check() {
  local i
  for i in $(seq 1 30); do
    if curl -fsS "http://127.0.0.1:${HTTP_PORT}/healthz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  return 1
}

rebuild_and_up() {
  cd "$REMOTE_DIR/deploy"
  docker compose build server
  docker compose up -d server nginx
  docker compose restart nginx
}

backup_current
restore_app
if [ "$RESTORE_DB" = "1" ]; then
  restore_db
fi
rebuild_and_up
health_check
echo "[remote-release] rollback ok backup_id=$BACKUP_ID"
echo "[remote-release] rollback_safety_backup=$CURRENT_ID"
EOF
}

main() {
  local cmd rollback_id=""
  cmd="${1:-}"
  [ -n "$cmd" ] || {
    usage
    exit 1
  }
  shift || true

  case "$cmd" in
    deploy|list)
      parse_common_args "$@" || die "未知参数: $1"
      ;;
    rollback)
      [ "$#" -ge 1 ] || die "rollback 需要 backup_id"
      rollback_id="$1"
      shift
      parse_common_args "$@" || die "未知参数: $1"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "未知命令: $cmd"
      ;;
  esac

  case "$cmd" in
    deploy)
      deploy_release
      ;;
    list)
      list_backups
      ;;
    rollback)
      rollback_release "$rollback_id"
      ;;
  esac
}

main "$@"
