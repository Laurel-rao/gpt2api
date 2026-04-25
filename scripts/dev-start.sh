#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${ROOT}/deploy/docker-compose.yml"
DEV_DIR="${ROOT}/.dev"
PID_FILE="${DEV_DIR}/dev-start.pid"

MYSQL_ROOT_PASSWORD="${MYSQL_ROOT_PASSWORD:-root}"
MYSQL_DATABASE="${MYSQL_DATABASE:-gpt2api}"
MYSQL_USER="${MYSQL_USER:-gpt2api}"
MYSQL_PASSWORD="${MYSQL_PASSWORD:-gpt2api}"
MYSQL_PORT="${MYSQL_PORT:-3306}"
REDIS_PORT="${REDIS_PORT:-6379}"
HTTP_PORT="${HTTP_PORT:-8080}"
WEB_PORT="${WEB_PORT:-5173}"

MYSQL_DSN=""
MIGRATE_DSN=""
REDIS_ADDR=""

log() {
  printf '[dev-start] %s\n' "$*"
}

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    log "missing command: $1"
    exit 1
  fi
}

port_in_use() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1; then
    lsof -nP -iTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1
    return
  fi
  nc -z 127.0.0.1 "${port}" >/dev/null 2>&1
}

container_owns_port() {
  local container="$1"
  local port="$2"
  docker ps --filter "name=^/${container}$" --format '{{.Ports}}' \
    | grep -Eq "(0\.0\.0\.0|127\.0\.0\.1|::):${port}->|\\[::\\]:${port}->"
}

choose_port() {
  local label="$1"
  local var_name="$2"
  local port="$3"
  local owner="${4:-}"
  while port_in_use "${port}" && ! container_owns_port "${owner}" "${port}"; do
    log "${label} port ${port} is busy, trying $((port + 1))"
    port=$((port + 1))
  done
  printf -v "${var_name}" '%s' "${port}"
}

refresh_runtime_vars() {
  MYSQL_DSN="${MYSQL_USER}:${MYSQL_PASSWORD}@tcp(127.0.0.1:${MYSQL_PORT})/${MYSQL_DATABASE}?parseTime=true&loc=Local&charset=utf8mb4&collation=utf8mb4_unicode_ci"
  MIGRATE_DSN="${MYSQL_USER}:${MYSQL_PASSWORD}@tcp(127.0.0.1:${MYSQL_PORT})/${MYSQL_DATABASE}?parseTime=true&multiStatements=true&charset=utf8mb4,utf8"
  REDIS_ADDR="127.0.0.1:${REDIS_PORT}"
}

choose_ports() {
  choose_port "mysql" MYSQL_PORT "${MYSQL_PORT}" "gpt2api-mysql"
  choose_port "redis" REDIS_PORT "${REDIS_PORT}" "gpt2api-redis"
  choose_port "backend" HTTP_PORT "${HTTP_PORT}" ""
  choose_port "frontend" WEB_PORT "${WEB_PORT}" ""
  refresh_runtime_vars
}

ensure_config() {
  if [[ -f "${ROOT}/configs/config.yaml" ]]; then
    return
  fi

  log "creating configs/config.yaml"
  cat > "${ROOT}/configs/config.yaml" <<EOF
app:
  name: gpt2api
  env: dev
  listen: ":${HTTP_PORT}"
  base_url: "http://localhost:${HTTP_PORT}"

log:
  level: info
  format: console
  output: stdout

mysql:
  dsn: "${MYSQL_DSN}"
  max_open_conns: 100
  max_idle_conns: 20
  conn_max_lifetime_sec: 3600

redis:
  addr: "${REDIS_ADDR}"
  password: ""
  db: 0
  pool_size: 100

jwt:
  secret: "dev_secret_change_me_32_bytes_minimum"
  access_ttl_sec: 86400
  refresh_ttl_sec: 2592000
  issuer: "gpt2api"

crypto:
  aes_key: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

security:
  bcrypt_cost: 10
  cors_origins:
    - "http://localhost:${WEB_PORT}"

scheduler:
  min_interval_sec: 60
  daily_usage_ratio: 0.6
  lock_ttl_sec: 1200
  cooldown_429_sec: 600
  warned_pause_hours: 24

upstream:
  base_url: "https://chatgpt.com"
  request_timeout_sec: 60
  sse_read_timeout_sec: 300

epay:
  gateway_url: ""
  pid: ""
  key: ""
  notify_url: ""
  return_url: ""
  sign_type: "MD5"
  expires_min: 30

backup:
  dir: "./data/backups"
  retention: 30
  allow_restore: false

smtp:
  host: ""
  port: 465
  username: ""
  password: ""
  from: ""
  from_name: "GPT2API"
  use_tls: true
EOF
}

start_deps() {
  log "starting docker deps: mysql redis"
  MYSQL_ROOT_PASSWORD="${MYSQL_ROOT_PASSWORD}" \
  MYSQL_DATABASE="${MYSQL_DATABASE}" \
  MYSQL_USER="${MYSQL_USER}" \
  MYSQL_PASSWORD="${MYSQL_PASSWORD}" \
  MYSQL_PORT="${MYSQL_PORT}" \
  REDIS_PORT="${REDIS_PORT}" \
    docker compose -f "${COMPOSE_FILE}" up -d --force-recreate mysql redis
}

wait_mysql() {
  log "waiting for mysql 127.0.0.1:${MYSQL_PORT}"
  for _ in $(seq 1 90); do
    if docker compose -f "${COMPOSE_FILE}" exec -T mysql mysqladmin ping \
      -h 127.0.0.1 -P 3306 -u root -p"${MYSQL_ROOT_PASSWORD}" --silent >/dev/null 2>&1; then
      return
    fi
    sleep 1
  done
  log "mysql did not become ready in 90s"
  exit 1
}

wait_redis() {
  log "waiting for redis 127.0.0.1:${REDIS_PORT}"
  for _ in $(seq 1 60); do
    if docker compose -f "${COMPOSE_FILE}" exec -T redis redis-cli ping >/dev/null 2>&1; then
      return
    fi
    sleep 1
  done
  log "redis did not become ready in 60s"
  exit 1
}

run_migrations() {
  log "running database migrations"
  if command -v goose >/dev/null 2>&1; then
    goose -dir "${ROOT}/sql/migrations" mysql "${MIGRATE_DSN}" up
  else
    go run github.com/pressly/goose/v3/cmd/goose@v3.20.0 \
      -dir "${ROOT}/sql/migrations" mysql "${MIGRATE_DSN}" up
  fi
}

ensure_web_deps() {
  if [[ ! -d "${ROOT}/web/node_modules" ]]; then
    log "installing web dependencies"
    npm --prefix "${ROOT}/web" ci
  fi
}

stop_local() {
  if [[ ! -f "${PID_FILE}" ]]; then
    log "no local pid file found"
    return
  fi

  while read -r pid; do
    if [[ -n "${pid}" ]] && kill -0 "${pid}" >/dev/null 2>&1; then
      pkill -TERM -P "${pid}" >/dev/null 2>&1 || true
      kill "${pid}" >/dev/null 2>&1 || true
    fi
  done < "${PID_FILE}"
  rm -f "${PID_FILE}"
  log "stopped local backend/frontend processes"
}

run_servers() {
  mkdir -p "${DEV_DIR}" "${ROOT}/data/backups"
  : > "${PID_FILE}"

  export GPT2API_APP_LISTEN=":${HTTP_PORT}"
  export GPT2API_APP_BASE_URL="http://localhost:${HTTP_PORT}"
  export GPT2API_MYSQL_DSN="${MYSQL_DSN}"
  export GPT2API_REDIS_ADDR="${REDIS_ADDR}"

  log "starting backend http://localhost:${HTTP_PORT}"
  (cd "${ROOT}" && go run ./cmd/server -c configs/config.yaml) &
  api_pid=$!
  echo "${api_pid}" >> "${PID_FILE}"

  log "starting frontend http://localhost:${WEB_PORT}"
  (cd "${ROOT}/web" && npm run dev -- --host 0.0.0.0 --port "${WEB_PORT}") &
  web_pid=$!
  echo "${web_pid}" >> "${PID_FILE}"

  cleanup() {
    pkill -TERM -P "${api_pid}" >/dev/null 2>&1 || true
    pkill -TERM -P "${web_pid}" >/dev/null 2>&1 || true
    kill "${api_pid}" "${web_pid}" >/dev/null 2>&1 || true
    rm -f "${PID_FILE}"
  }
  trap cleanup INT TERM EXIT

  log "ready: backend=http://localhost:${HTTP_PORT} frontend=http://localhost:${WEB_PORT}"
  while kill -0 "${api_pid}" >/dev/null 2>&1 && kill -0 "${web_pid}" >/dev/null 2>&1; do
    sleep 1
  done

  wait "${api_pid}" "${web_pid}"
}

main() {
  if [[ "${1:-}" == "--stop" ]]; then
    stop_local
    exit 0
  fi

  need docker
  need go
  need npm

  choose_ports
  ensure_config
  start_deps
  wait_mysql
  wait_redis
  run_migrations
  ensure_web_deps
  run_servers
}

main "$@"
