#!/usr/bin/env bash
# 视频画布数据库与不可变素材卷的一致性快照、校验和显式恢复工具。
set -euo pipefail
umask 077

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE_FILE="${VIDEO_WORKFLOW_COMPOSE_FILE:-$ROOT/deploy/docker-compose.yml}"
COMPOSE_ENV_FILE="${VIDEO_WORKFLOW_COMPOSE_ENV_FILE:-$ROOT/deploy/.env}"
SERVER_CONTAINER="${VIDEO_WORKFLOW_SERVER_CONTAINER:-gpt2api-server}"
MYSQL_CONTAINER="${VIDEO_WORKFLOW_MYSQL_CONTAINER:-gpt2api-mysql}"
BACKUP_DIR="${VIDEO_WORKFLOW_BACKUP_DIR:-$ROOT/deploy/backups/video-workflow-assets}"
RETENTION="${VIDEO_WORKFLOW_BACKUP_RETENTION:-7}"
HTTP_PORT="${GPT2API_HTTP_PORT:-${HTTP_PORT:-}}"
ACTION="${1:-create}"

log() { printf '[video-workflow-backup] %s\n' "$*"; }
die() { printf '[video-workflow-backup] ERROR: %s\n' "$*" >&2; exit 1; }

resolve_http_port() {
  local value="$HTTP_PORT"
  if [ -z "$value" ]; then
    value="$(awk -F= '
      $1 ~ /^[[:space:]]*HTTP_PORT[[:space:]]*$/ {
        sub(/^[^=]*=/, "")
        gsub(/^[[:space:]]+|[[:space:]]+$/, "")
        value=$0
      }
      END { print value }
    ' "$COMPOSE_ENV_FILE")"
    value="$(printf '%s\n' "$value" | sed 's/[[:space:]][[:space:]]*#.*$//')"
    value="${value%\"}"
    value="${value#\"}"
    value="${value%\'}"
    value="${value#\'}"
  fi
  value="${value:-8080}"
  [[ "$value" =~ ^[0-9]+$ ]] && [ "$value" -ge 1 ] && [ "$value" -le 65535 ] \
    || die "HTTP_PORT 必须是 1-65535 的整数: $value"
  printf '%s\n' "$value"
}

require_runtime() {
  command -v docker >/dev/null 2>&1 || die "缺少 docker"
  [ -f "$COMPOSE_ENV_FILE" ] || die "缺少 Compose 环境文件: $COMPOSE_ENV_FILE"
  docker inspect "$SERVER_CONTAINER" >/dev/null 2>&1 || die "容器不存在: $SERVER_CONTAINER"
  docker inspect "$MYSQL_CONTAINER" >/dev/null 2>&1 || die "容器不存在: $MYSQL_CONTAINER"
  HTTP_PORT="$(resolve_http_port)"
}

start_services() {
  docker compose --env-file "$COMPOSE_ENV_FILE" -f "$COMPOSE_FILE" up -d --wait --wait-timeout 120 server nginx
}

stop_writers() {
  log "停止 nginx/server，冻结数据库与素材卷写入"
  docker compose --env-file "$COMPOSE_ENV_FILE" -f "$COMPOSE_FILE" stop nginx server
}

safe_tar_listing() {
  local archive="$1"
  tar -tzf "$archive" | awk '{ if ($0 ~ /^\// || $0 ~ /(^|\/)\.\.($|\/)/) exit 1 }'
  tar -tvzf "$archive" | awk 'substr($1, 1, 1) != "-" && substr($1, 1, 1) != "d" { exit 1 }'
}

verify_archive() (
  local archive="${1:-}"
  [ -n "$archive" ] || die "用法: $0 verify <archive>"
  [ -f "$archive" ] || die "备份不存在: $archive"
  [ -f "$archive.sha256" ] || die "缺少 SHA-256 清单: $archive.sha256"
  gzip -t "$archive"
  safe_tar_listing "$archive"
  (cd "$(dirname "$archive")" && sha256sum -c "$(basename "$archive").sha256")

  local work assets_root key digest expected_size actual_size actual_digest
  work="$(mktemp -d "${TMPDIR:-/tmp}/video-workflow-verify.XXXXXX")"
  trap 'rm -rf "$work"' EXIT
  tar -xzf "$archive" -C "$work"
  for key in assets.tar.gz database.sql.gz asset-manifest.tsv metadata.env checksums.sha256; do
    [ -f "$work/$key" ] || die "快照缺少文件: $key"
  done
  (cd "$work" && sha256sum -c checksums.sha256)
  grep -qx 'snapshot_schema=1' "$work/metadata.env" || die "不支持的快照版本"
  gzip -t "$work/assets.tar.gz"
  gzip -t "$work/database.sql.gz"
  safe_tar_listing "$work/assets.tar.gz"

  assets_root="$work/assets"
  mkdir -p "$assets_root"
  tar -xzf "$work/assets.tar.gz" -C "$assets_root"
  while IFS=$'\t' read -r key digest expected_size; do
    [ -n "$key" ] || continue
    case "/$key/" in
      *"/../"*|*"//"*) die "素材清单含非法路径: $key" ;;
    esac
    [[ "$key" != /* ]] || die "素材清单含绝对路径: $key"
    [[ "$digest" =~ ^[0-9a-f]{64}$ ]] || die "素材摘要非法: $key"
    [[ "$expected_size" =~ ^[0-9]+$ ]] || die "素材大小非法: $key"
    [ -f "$assets_root/$key" ] || die "数据库引用的素材未进入快照: $key"
    [ ! -L "$assets_root/$key" ] || die "素材不能是符号链接: $key"
    actual_size="$(wc -c < "$assets_root/$key" | tr -d ' ')"
    [ "$actual_size" = "$expected_size" ] || die "素材大小不一致: $key"
    actual_digest="$(sha256sum "$assets_root/$key" | awk '{print $1}')"
    [ "$actual_digest" = "$digest" ] || die "素材摘要不一致: $key"
  done < "$work/asset-manifest.tsv"
  log "数据库、素材卷与逐项清单校验通过: $archive"
)

create_backup() {
  require_runtime
  mkdir -p "$BACKUP_DIR"
  chmod 700 "$BACKUP_DIR"
  local stamp archive partial work stopped=0 committed=0
  stamp="$(date -u +%Y%m%dT%H%M%SZ)"
  archive="$BACKUP_DIR/video-workflow-snapshot-$stamp.tar.gz"
  partial="$archive.partial"
  [ ! -e "$archive" ] || die "同名快照已存在: $archive"
  work="$(mktemp -d "${TMPDIR:-/tmp}/video-workflow-snapshot.XXXXXX")"
  cleanup_create() {
    local status=$?
    rm -rf "$work" "$partial"
    if [ "$committed" = "0" ]; then
      rm -f "$archive" "$archive.sha256"
    fi
    if [ "$stopped" = "1" ]; then
      log "恢复 server/nginx"
      start_services || status=1
    fi
    return "$status"
  }
  trap cleanup_create EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM

  stop_writers
  stopped=1
  log "导出一致性数据库快照"
  docker exec "$MYSQL_CONTAINER" sh -c \
    'exec mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" --single-transaction --quick --routines --triggers "$MYSQL_DATABASE"' \
    | gzip -1 > "$work/database.sql.gz"
  docker exec "$MYSQL_CONTAINER" sh -c \
    'exec mysql -uroot -p"$MYSQL_ROOT_PASSWORD" -N -B "$MYSQL_DATABASE" -e "$1"' sh \
    "SELECT storage_key, LOWER(sha256), size_bytes FROM video_workflow_asset_versions WHERE status='ready' ORDER BY storage_key" \
    > "$work/asset-manifest.tsv"

  log "导出不可变素材卷"
  docker run --rm --user 0:0 \
    --volumes-from "$SERVER_CONTAINER:ro" \
    -v "$work:/snapshot" \
    --entrypoint sh gpt2api/server:latest \
    -ceu 'cd /app/data/video-workflow-assets && tar --exclude="./.tmp" -czf /snapshot/assets.tar.gz .'

  {
    printf 'snapshot_schema=1\n'
    printf 'created_at=%s\n' "$stamp"
    printf 'writer_state=stopped\n'
  } > "$work/metadata.env"
  (cd "$work" && sha256sum assets.tar.gz database.sql.gz asset-manifest.tsv metadata.env > checksums.sha256)
  tar -czf "$partial" -C "$work" assets.tar.gz database.sql.gz asset-manifest.tsv metadata.env checksums.sha256
  printf '%s  %s\n' "$(sha256sum "$partial" | awk '{print $1}')" "$(basename "$archive")" > "$archive.sha256"
  mv "$partial" "$archive"
  verify_archive "$archive"
  committed=1

  start_services
  curl -fsS "http://127.0.0.1:$HTTP_PORT/healthz" >/dev/null
  curl -fsS "http://127.0.0.1:$HTTP_PORT/readyz" >/dev/null
  stopped=0
  trap - EXIT INT TERM
  rm -rf "$work"

  if [[ "$RETENTION" =~ ^[0-9]+$ ]] && [ "$RETENTION" -gt 0 ]; then
    find "$BACKUP_DIR" -maxdepth 1 -type f -name 'video-workflow-snapshot-*.tar.gz' -print \
      | sort -r | tail -n "+$((RETENTION + 1))" | while IFS= read -r old; do
      [ -n "$old" ] || continue
      rm -f "$old" "$old.sha256"
    done
  fi
  printf '%s\n' "$archive"
}

restore_backup() {
  local archive="${2:-}"
  [ -n "$archive" ] || die "用法: $0 restore <archive>"
  [ "${VIDEO_WORKFLOW_RESTORE_CONFIRM:-}" = "YES" ] || die "恢复会覆盖数据库，请设置 VIDEO_WORKFLOW_RESTORE_CONFIRM=YES"
  require_runtime
  verify_archive "$archive"

  local original_retention="$RETENTION" safety_snapshot work stopped=0 succeeded=0
  RETENTION=0
  log "创建恢复前一致性安全快照"
  safety_snapshot="$(create_backup | tail -n 1)"
  RETENTION="$original_retention"
  work="$(mktemp -d "${TMPDIR:-/tmp}/video-workflow-restore.XXXXXX")"
  tar -xzf "$archive" -C "$work"
  cleanup_restore() {
    local status=$?
    rm -rf "$work"
    if [ "$stopped" = "1" ]; then
      docker compose --env-file "$COMPOSE_ENV_FILE" -f "$COMPOSE_FILE" stop nginx server >/dev/null 2>&1 || true
      if [ "$succeeded" = "0" ]; then
        log "恢复失败，服务保持停止；安全快照: $safety_snapshot"
      else
        log "快照已恢复但服务未通过启动检查；服务保持停止，安全快照: $safety_snapshot"
      fi
    fi
    return "$status"
  }
  trap cleanup_restore EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM

  stop_writers
  stopped=1
  log "合并内容寻址素材；快照外孤立文件保留且不会被数据库引用"
  docker run --rm --user 0:0 \
    --volumes-from "$SERVER_CONTAINER" \
    -v "$work:/snapshot:ro" \
    --entrypoint sh gpt2api/server:latest \
    -ceu 'tar -xzf /snapshot/assets.tar.gz -C /app/data/video-workflow-assets && chown -R 10001:10001 /app/data/video-workflow-assets'

  log "恢复配对数据库快照"
  gzip -dc "$work/database.sql.gz" | docker exec -i "$MYSQL_CONTAINER" sh -c \
    'exec mysql -uroot -p"$MYSQL_ROOT_PASSWORD" "$MYSQL_DATABASE"'
  succeeded=1
  start_services
  curl -fsS "http://127.0.0.1:$HTTP_PORT/healthz" >/dev/null
  curl -fsS "http://127.0.0.1:$HTTP_PORT/readyz" >/dev/null
  stopped=0
  trap - EXIT INT TERM
  rm -rf "$work"
  log "数据库与素材卷恢复完成: $archive；安全快照: $safety_snapshot"
}

case "$ACTION" in
  create) create_backup ;;
  verify) verify_archive "${2:-}" ;;
  list) find "$BACKUP_DIR" -maxdepth 1 -type f -name 'video-workflow-snapshot-*.tar.gz' -print 2>/dev/null | sort -r ;;
  restore) restore_backup "$@" ;;
  *) die "支持动作: create | verify <archive> | list | restore <archive>" ;;
esac
