#!/usr/bin/env bash
# 启动本地 mock 上游，并把 system_settings 指到 mock；可选重启本机 go 后端。
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${LOCAL_GEN_MOCK_PORT:-8790}"
MOCK_PID_FILE="${ROOT}/.dev/local-gen-mock.pid"
API_PID_FILE="${ROOT}/.dev/local-backend.pid"
MYSQL_PORT="${MYSQL_PORT:-3307}"

log() { printf '[local-gen] %s\n' "$*"; }

mkdir -p "${ROOT}/.dev" "${ROOT}/data/video-workflow-assets" "${ROOT}/data/ecommerce-assets" "${ROOT}/data/backups"

if [[ "${1:-}" == "--stop" ]]; then
  for f in "${MOCK_PID_FILE}" "${API_PID_FILE}"; do
    if [[ -f "${f}" ]]; then
      while read -r pid; do
        [[ -n "${pid}" ]] && kill "${pid}" 2>/dev/null || true
      done < "${f}"
      rm -f "${f}"
    fi
  done
  log "stopped"
  exit 0
fi

# stop previous mock
if [[ -f "${MOCK_PID_FILE}" ]]; then
  while read -r pid; do kill "${pid}" 2>/dev/null || true; done < "${MOCK_PID_FILE}"
  rm -f "${MOCK_PID_FILE}"
fi

log "starting mock on :${PORT}"
nohup node "${ROOT}/scripts/local-gen-mock.mjs" --port "${PORT}" \
  > "${ROOT}/.dev/local-gen-mock.log" 2>&1 &
echo $! > "${MOCK_PID_FILE}"
sleep 1
curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null
log "mock healthy"

log "updating system_settings → mock"
# 强制 utf8mb4：docker mysql 客户端默认 latin1，中文 label 会写成 æœ¬åœ° 乱码。
# 用 unquoted heredoc 展开 ${PORT}；stdin 管道保证脚本本身的 UTF-8 字节原样进入客户端。
{
  docker run --rm --network deploy_default -i mysql:8.0 \
    mysql -hmysql -ugpt2api -pgpt2api --default-character-set=utf8mb4 gpt2api <<SQL
INSERT INTO system_settings (\`k\`, \`v\`) VALUES
  ('imagegen.enabled', 'true'),
  ('imagegen.base_url', 'http://127.0.0.1:${PORT}/v1'),
  ('imagegen.api_key', 'local-mock-key'),
  ('imagegen.response_format', 'b64_json'),
  ('textgen.enabled', 'true'),
  ('textgen.base_url', 'http://127.0.0.1:${PORT}/v1'),
  ('textgen.api_key', 'local-mock-key'),
  ('textgen.model', 'gpt-5.4'),
  ('videogen.enabled', 'true'),
  ('videogen.channel_type', 'apiyi_seedance2'),
  ('videogen.base_url', 'http://127.0.0.1:${PORT}'),
  ('videogen.api_key', 'local-mock-key'),
  ('videogen.model', 'doubao-seedance-2-0-fast-260128'),
  ('videogen.apiyi_seedance2.account', '本地 Seedance Mock'),
  ('videogen.apiyi_seedance2.api_key', 'local-mock-key'),
  ('videogen.apiyi_seedance2.base_url', 'http://127.0.0.1:${PORT}'),
  ('videogen.apiyi_seedance2.model', 'doubao-seedance-2-0-fast-260128'),
  ('videogen.duration_sec', '15'),
  ('videogen.aspect_ratio', '9:16'),
  ('videogen.resolution', '720p'),
  ('videogen.workflow_models', '[{"channel_type":"apiyi_seedance2","value":"doubao-seedance-2-0-fast-260128","label":"Seedance 2.0 Fast (本地)"},{"channel_type":"apiyi_seedance2","value":"doubao-seedance-2-0-260128","label":"Seedance 2.0 (本地)"}]')
ON DUPLICATE KEY UPDATE \`v\`=VALUES(\`v\`);
UPDATE users SET credit_balance = GREATEST(credit_balance, 20000000) WHERE role='admin' OR email LIKE '%@local.test';
SQL
} 2>/dev/null

if [[ "${1:-}" == "--with-backend" ]]; then
  if [[ -f "${API_PID_FILE}" ]]; then
    while read -r pid; do
      if [[ -n "${pid}" ]]; then
        pkill -TERM -P "${pid}" 2>/dev/null || true
        kill "${pid}" 2>/dev/null || true
      fi
    done < "${API_PID_FILE}"
    rm -f "${API_PID_FILE}"
    sleep 1
  fi
  # also kill any go server on 8080
  if lsof -nP -iTCP:8080 -sTCP:LISTEN >/dev/null 2>&1; then
    lsof -nP -iTCP:8080 -sTCP:LISTEN -t | xargs -r kill 2>/dev/null || true
    sleep 1
  fi

  export GPT2API_APP_LISTEN=':8080' \
    GPT2API_APP_BASE_URL='http://127.0.0.1:8080' \
    GPT2API_MYSQL_DSN="gpt2api:gpt2api@tcp(127.0.0.1:${MYSQL_PORT})/gpt2api?parseTime=true&loc=Local&charset=utf8mb4&collation=utf8mb4_unicode_ci&tls=false" \
    GPT2API_REDIS_ADDR='127.0.0.1:6379' \
    GPT2API_APP_ENV='dev' \
    GPT2API_JWT_SECRET='dev_local_jwt_secret_at_least_32_bytes!!' \
    GPT2API_CRYPTO_AES_KEY='0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef' \
    GPT2API_VIDEO_WORKFLOW_ASSET_DIR="${ROOT}/data/video-workflow-assets" \
    GPT2API_ECOMMERCE_ASSET_DIR="${ROOT}/data/ecommerce-assets" \
    GPT2API_BACKUP_DIR="${ROOT}/data/backups" \
    GPT2API_VIDEO_WORKFLOW_PUBLIC_BASE_URL='http://127.0.0.1:8080' \
    GPT2API_VIDEO_WORKFLOW_SIGNING_SECRET='dev_video_workflow_signing_secret_32b' \
    GPT2API_VIDEO_WORKFLOW_ALLOW_LOCAL_MEDIA='1' \
    AZT_API_KEY='local-mock-key' \
    AI_GEN_API_KEY='local-mock-key' \
    GPT2API_IMAGEGEN_BASE_URL="http://127.0.0.1:${PORT}/v1" \
    GPT2API_TEXTGEN_BASE_URL="http://127.0.0.1:${PORT}/v1" \
    GPT2API_VIDEOGEN_BASE_URL="http://127.0.0.1:${PORT}/api/v1"

  log "starting backend on :8080"
  nohup bash -c "cd \"${ROOT}\" && go run ./cmd/server -c configs/config.yaml" \
    > "${ROOT}/.dev/local-backend.log" 2>&1 &
  echo $! > "${API_PID_FILE}"
  for i in $(seq 1 60); do
    if curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
      log "backend healthy"
      break
    fi
    sleep 1
  done
fi

log "ready"
log "  mock:         http://127.0.0.1:${PORT}/healthz"
log "  gpt-image-2:  http://127.0.0.1:${PORT}/v1/images/generations"
log "  seedance2.0:  http://127.0.0.1:${PORT}/seedance/api/v3/contents/generations/tasks"
log "reload settings in admin UI if backend was already running (or pass --with-backend)"
