#!/usr/bin/env bash
# Hololive Kakao Bot 로컬 단일 프로세스 보조 명령 단일 진입점
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${PROJECT_ROOT}"
REPO_ROOT="$(cd "${PROJECT_ROOT}/../.." && pwd)"

source "${REPO_ROOT}/scripts/deploy/lib/compose-env.sh"

PID_FILE=".bot.pid"
LOG_DIR="logs"
APP_LOG_NAME="hololive-api.log"
NOHUP_LOG="${LOG_DIR}/nohup.log"

usage() {
  cat <<'EOF'
Usage: ./scripts/bot.sh <command> [options]

Commands:
  start [--no-ready-wait]
  stop
  restart [--build] [--no-ready-wait]
  rebuild [--restart]
  status
  help
EOF
}

validate_container_cli() {
  local container_cli="${1:-docker}"
  case "${container_cli}" in
    docker|podman) ;;
    *)
      echo "[ERROR] Unsupported CONTAINER_CLI: ${container_cli}"
      echo "Allowed values: docker, podman"
      exit 1
      ;;
  esac
}

find_bot_pids() {
  pgrep -f "bin/bot" 2>/dev/null | while read -r pid; do
    local dir=""
    dir="$(readlink -f "/proc/${pid}/cwd" 2>/dev/null || echo "")"
    if [[ "${dir}" == "${PROJECT_ROOT}" ]]; then
      echo "${pid}"
    fi
  done || true
}

load_env_file_literal() {
  local env_file="$1"
  local key=""
  local value=""

  compose_env_validate_file_format "${env_file}"
  while IFS= read -r key; do
    [[ -n "${key}" ]] || continue
    value="$(compose_env_read_value_from_file "${env_file}" "${key}")"
    export "${key}=${value}"
  done < <(compose_env_list_keys_from_file "${env_file}")
}

cmd_start() {
  local container_cli="${CONTAINER_CLI:-docker}"
  local wait_for_ready="true"
  local min_count="${CORE_MEMBER_HASH_SOFT_MIN_COUNT:-50}"
  local timeout_sec="${CORE_MEMBER_HASH_SOFT_TIMEOUT_SECONDS:-45}"
  local old_pid=""
  local running_pids=""
  local required_vars="IRIS_BASE_URL HOLODEX_API_KEY_1 CACHE_HOST"
  local var=""
  local value=""
  local log_size=0
  local backup_name=""
  local iris_port=""
  local bot_pid=""
  local start_ts=0
  local count=0
  local now=0
  local elapsed=0

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --no-ready-wait)
        wait_for_ready="false"
        shift
        ;;
      -h|--help)
        usage
        return 0
        ;;
      *)
        echo "[ERROR] Unknown argument: $1"
        echo "Usage: ./scripts/bot.sh start [--no-ready-wait]"
        exit 1
        ;;
    esac
  done

  validate_container_cli "${container_cli}"

  echo "[START] Starting Hololive KakaoTalk Bot (Go)..."

  if [[ -f "${PID_FILE}" ]]; then
    old_pid="$(cat "${PID_FILE}")"
    if ps -p "${old_pid}" >/dev/null 2>&1; then
      echo "[WARN] Bot is already running (PID: ${old_pid})"
      echo "Use './scripts/bot.sh stop' to stop first"
      exit 1
    fi
    echo "[WARN] Stale PID file found, removing..."
    rm -f "${PID_FILE}"
  fi

  running_pids="$(find_bot_pids)"
  if [[ -n "${running_pids}" ]]; then
    echo "[ERROR] Bot is already running without PID file!"
    echo "Running PIDs: ${running_pids}"
    echo "Use './scripts/bot.sh stop' to stop"
    exit 1
  fi

  if [[ ! -f ".env" ]]; then
    echo "[ERROR] .env file not found!"
    echo "Copy .env.example to .env and configure it"
    exit 1
  fi

  echo "[CHECK] Validating environment variables..."
  load_env_file_literal ".env"
  for var in ${required_vars}; do
    if [[ -z "${!var+x}" ]]; then
      echo "[ERROR] Required variable missing: ${var}"
      exit 1
    fi

    value="${!var:-}"
    if [[ -z "${value}" ]]; then
      echo "[ERROR] Required variable is empty: ${var}"
      exit 1
    fi
  done
  echo "[OK] Environment variables validated"

  min_count="${CORE_MEMBER_HASH_SOFT_MIN_COUNT:-${min_count}}"
  timeout_sec="${CORE_MEMBER_HASH_SOFT_TIMEOUT_SECONDS:-${timeout_sec}}"

  if [[ ! -f "bin/bot" ]]; then
    echo "[BUILD] Binary not found, building..."
    CGO_ENABLED=0 go build -tags go_json -o bin/bot ./cmd/hololive-api || {
      echo "[ERROR] Build failed"
      exit 1
    }
  fi

  mkdir -p "${LOG_DIR}"

  if [[ -f "${NOHUP_LOG}" ]]; then
    log_size="$(stat -c%s "${NOHUP_LOG}" 2>/dev/null || echo 0)"
    if [[ "${log_size}" -gt 10485760 ]]; then
      backup_name="${LOG_DIR}/nohup.log.$(date +%Y%m%d-%H%M%S)"
      mv "${NOHUP_LOG}" "${backup_name}"
      echo "[INFO] Backed up large nohup.log to ${backup_name}"
    fi
  fi

  echo "[CHECK] Checking Redis connection..."
  if ! command -v "${container_cli}" >/dev/null 2>&1; then
    echo "[ERROR] Container CLI not found: ${container_cli}"
    echo "Set CONTAINER_CLI=docker or CONTAINER_CLI=podman"
    exit 1
  fi

  if ! "${container_cli}" ps | grep "holo-valkey" | grep -q "Up"; then
    echo "[WARN] Valkey container (holo-valkey) is not running!"
    echo "Start it with: ${container_cli} start holo-valkey"
    exit 1
  fi

  if ! timeout 3 "${container_cli}" exec holo-valkey valkey-cli ping >/dev/null 2>&1; then
    echo "[WARN] Valkey container is running but not responding"
    exit 1
  fi
  echo "[OK] Valkey connection verified"

  echo "[CHECK] Checking Iris server..."
  iris_port="$(grep "^IRIS_BASE_URL=" .env | cut -d'=' -f2- | grep -oP ':\K\d+' || echo "3000")"
  if ! ss -tuln | grep -q ":${iris_port} "; then
    echo "[WARN] Iris server is not running on port ${iris_port}!"
    echo "Make sure Iris server is started"
    exit 1
  fi
  echo "[OK] Iris server detected on port ${iris_port}"

  echo "[RUN] Starting bot with optimized GC settings..."
  GOGC=60 nohup ./bin/bot > "${NOHUP_LOG}" 2>&1 &
  bot_pid=$!
  echo "${bot_pid}" > "${PID_FILE}"

  echo "Waiting for initialization..."
  sleep 4

  if ps -p "${bot_pid}" >/dev/null 2>&1; then
    echo "[OK] Bot started successfully"
    echo "   PID: ${bot_pid}"
    echo "   Logs:"
    echo "     - Application: ${LOG_DIR}/${APP_LOG_NAME} (slog)"
    echo "     - Process: ${NOHUP_LOG} (stdout/stderr)"
    echo ""
    echo "   Commands:"
    echo "     Status:  ./scripts/bot.sh status"
    echo "     Stop:    ./scripts/bot.sh stop"
    echo "     Restart: ./scripts/bot.sh restart"
  else
    echo "[ERROR] Bot failed to start, check logs:"
    echo "   - ${NOHUP_LOG}"
    echo "   - ${LOG_DIR}/${APP_LOG_NAME}"
    tail -30 "${NOHUP_LOG}" 2>/dev/null || true
    rm -f "${PID_FILE}"
    exit 1
  fi

  if [[ "${wait_for_ready}" == "true" ]]; then
    echo "[CHECK] Waiting for member cache readiness..."
    start_ts="$(date +%s)"
    while true; do
      if "${container_cli}" exec holo-valkey valkey-cli EXISTS hololive:members:ready 2>/dev/null | grep -q "^1$"; then
        echo "[READY] hololive:members:ready flag detected"
        break
      fi

      count="$("${container_cli}" exec holo-valkey valkey-cli HLEN hololive:members 2>/dev/null | tr -d '\r' || echo 0)"
      if [[ "${count}" =~ ^[0-9]+$ ]] && [[ "${count}" -ge "${min_count}" ]]; then
        echo "[READY] hololive:members count >= ${min_count} (=${count})"
        break
      fi

      now="$(date +%s)"
      elapsed=$((now - start_ts))
      if [[ "${elapsed}" -ge "${timeout_sec}" ]]; then
        echo "[WARN] Readiness not reached in ${timeout_sec}s (flag missing, count=${count:-0})"
        break
      fi
      sleep 1
    done
  fi
}

cmd_stop() {
  local pids=""
  local pid=""
  local still_running=0
  local i=0

  echo "[STOP] Stopping Hololive KakaoTalk Bot (Go)..."

  if [[ ! -f "${PID_FILE}" ]]; then
    echo "[INFO] No PID file found, searching for process..."
    pids="$(find_bot_pids)"
    if [[ -z "${pids}" ]]; then
      echo "[OK] No bot process found (already stopped)"
      return 0
    fi
    echo "[WARN] Found running process: ${pids}"
    echo "Attempting to stop..."
  else
    pids="$(cat "${PID_FILE}")"
    if ! ps -p "${pids}" >/dev/null 2>&1; then
      echo "[WARN] Process ${pids} not running (stale PID file)"
      rm -f "${PID_FILE}"
      echo "[OK] Cleaned up PID file"
      return 0
    fi
  fi

  echo "Found bot process: ${pids}"
  echo "Sending SIGTERM for graceful shutdown..."
  for pid in ${pids}; do
    kill "${pid}" 2>/dev/null || true
  done

  for i in {1..15}; do
    sleep 1
    still_running=0
    for pid in ${pids}; do
      if ps -p "${pid}" >/dev/null 2>&1; then
        still_running=1
      fi
    done

    if [[ "${still_running}" -eq 0 ]]; then
      echo "[OK] Bot stopped gracefully"
      rm -f "${PID_FILE}"
      return 0
    fi
    echo "Waiting for shutdown... (${i}/15)"
  done

  echo "[WARN] Graceful shutdown timeout, forcing kill..."
  for pid in ${pids}; do
    kill -9 "${pid}" 2>/dev/null || true
  done
  sleep 1

  still_running=0
  for pid in ${pids}; do
    if ps -p "${pid}" >/dev/null 2>&1; then
      still_running=1
      echo "[ERROR] Failed to stop PID: ${pid}"
    fi
  done

  if [[ "${still_running}" -eq 0 ]]; then
    echo "[OK] Bot force killed"
    rm -f "${PID_FILE}"
    return 0
  fi

  echo "[ERROR] Some processes could not be stopped"
  exit 1
}

cmd_restart() {
  local build="false"
  local start_args=()

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --build|-b)
        build="true"
        shift
        ;;
      --no-ready-wait)
        start_args+=("$1")
        shift
        ;;
      -h|--help)
        usage
        return 0
        ;;
      *)
        echo "[ERROR] Unknown argument: $1"
        echo "Usage: ./scripts/bot.sh restart [--build] [--no-ready-wait]"
        exit 1
        ;;
    esac
  done

  echo "[RESTART] Restarting Hololive KakaoTalk Bot (Go)..."
  cmd_stop

  if [[ "${build}" == "true" ]]; then
    echo "[BUILD] Building..."
    CGO_ENABLED=0 go build -tags go_json -o bin/bot ./cmd/hololive-api
  fi

  cmd_start "${start_args[@]}"
}

cmd_rebuild() {
  local restart_after="false"
  local size=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --restart|-r)
        restart_after="true"
        shift
        ;;
      -h|--help)
        usage
        return 0
        ;;
      *)
        echo "[ERROR] Unknown argument: $1"
        echo "Usage: ./scripts/bot.sh rebuild [--restart]"
        exit 1
        ;;
    esac
  done

  echo "[REBUILD] Rebuilding Hololive KakaoTalk Bot (Go)..."
  echo "[CLEAN] Cleaning build cache..."
  go clean -cache

  echo "[BUILD] Building optimized binary (static + stripped + netgo)..."
  time CGO_ENABLED=0 go build -tags netgo,go_json -ldflags="-s -w" -o bin/bot ./cmd/hololive-api

  if [[ -f "bin/bot" ]]; then
    size="$(du -h bin/bot | cut -f1)"
    echo "[OK] Build successful"
    echo "   Binary: bin/bot (${size})"
  else
    echo "[ERROR] Build failed"
    exit 1
  fi

  if [[ "${restart_after}" == "true" ]]; then
    echo ""
    cmd_restart
  fi
}

cmd_status() {
  local container_cli="${CONTAINER_CLI:-docker}"
  local bot_pid=""
  local uptime=""
  local mem=""
  local cpu=""
  local fallback_pids=""
  local cache_port=""
  local ready_flag=""
  local member_count=""
  local iris_port=""
  local log_size=""
  local log_lines=""
  local nohup_size=""
  local nohup_lines=""

  validate_container_cli "${container_cli}"

  echo "[STATUS] Hololive KakaoTalk Bot (Go) Status"
  echo "========================================"

  if [[ -f "${PID_FILE}" ]]; then
    bot_pid="$(cat "${PID_FILE}")"
    if ps -p "${bot_pid}" >/dev/null 2>&1; then
      uptime="$(ps -o etime= -p "${bot_pid}" | tr -d ' ')"
      mem="$(ps -o rss= -p "${bot_pid}" | awk '{printf "%.1f MB", $1/1024}')"
      cpu="$(ps -o %cpu= -p "${bot_pid}" | tr -d ' ')"
      echo "Bot Status: [RUNNING]"
      echo "  PID: ${bot_pid}"
      echo "  Uptime: ${uptime}"
      echo "  Memory: ${mem}"
      echo "  CPU: ${cpu}%"
    else
      echo "Bot Status: [STOPPED] (stale PID file)"
      echo "  Stale PID: ${bot_pid}"
    fi
  else
    fallback_pids="$(find_bot_pids)"
    if [[ -n "${fallback_pids}" ]]; then
      echo "Bot Status: [WARN] RUNNING (no PID file)"
      echo "  PIDs: ${fallback_pids}"
      echo "  Warning: Use ./scripts/bot.sh start to manage with PID file"
    else
      echo "Bot Status: [NOT RUNNING]"
    fi
  fi

  echo ""
  echo "Dependencies:"
  echo "-------------"

  if command -v "${container_cli}" >/dev/null 2>&1 && "${container_cli}" ps | grep "holo-valkey" | grep -q "Up"; then
    if timeout 2 "${container_cli}" exec holo-valkey valkey-cli ping >/dev/null 2>&1; then
      cache_port="$(grep "^CACHE_PORT=" .env 2>/dev/null | cut -d'=' -f2- || echo "6379")"
      echo "Redis: [CONNECTED] (host port ${cache_port} -> container port 6379)"
      ready_flag="$("${container_cli}" exec holo-valkey valkey-cli EXISTS hololive:members:ready 2>/dev/null | tr -d '\r' || echo 0)"
      member_count="$("${container_cli}" exec holo-valkey valkey-cli HLEN hololive:members 2>/dev/null | tr -d '\r' || echo 0)"
      if [[ "${ready_flag}" == "1" ]]; then
        echo "Ready: [SET] hololive:members:ready"
      else
        echo "Ready: [NOT SET] hololive:members:ready"
      fi
      echo "Members: ${member_count}"
    else
      echo "Redis: [WARN] CONTAINER UP but not responding"
    fi
  else
    echo "Redis: [NOT RUNNING]"
  fi

  iris_port="$(grep "^IRIS_BASE_URL=" .env 2>/dev/null | cut -d'=' -f2- | grep -oP ':\K\d+' || echo "3000")"
  if ss -tuln | grep -q ":${iris_port} "; then
    echo "Iris: [LISTENING] (port ${iris_port})"
  else
    echo "Iris: [NOT LISTENING] (port ${iris_port})"
  fi

  echo ""
  echo "Logs:"
  echo "-----"
  if [[ -f "${LOG_DIR}/${APP_LOG_NAME}" ]]; then
    log_size="$(du -h "${LOG_DIR}/${APP_LOG_NAME}" 2>/dev/null | cut -f1)"
    log_lines="$(wc -l < "${LOG_DIR}/${APP_LOG_NAME}" 2>/dev/null)"
    echo "Application: ${LOG_DIR}/${APP_LOG_NAME} (${log_size}, ${log_lines} lines)"
  fi
  if [[ -f "${NOHUP_LOG}" ]]; then
    nohup_size="$(du -h "${NOHUP_LOG}" 2>/dev/null | cut -f1)"
    nohup_lines="$(wc -l < "${NOHUP_LOG}" 2>/dev/null)"
    echo "Process: ${NOHUP_LOG} (${nohup_size}, ${nohup_lines} lines)"
  fi

  echo ""
  echo "Commands:"
  echo "---------"
  echo "Start:   ./scripts/bot.sh start"
  echo "Stop:    ./scripts/bot.sh stop"
  echo "Restart: ./scripts/bot.sh restart [--build]"
  echo "Rebuild: ./scripts/bot.sh rebuild"
  echo "Status:  ./scripts/bot.sh status"
}

main() {
  local command="${1:-help}"

  case "${command}" in
    start)
      shift
      cmd_start "$@"
      ;;
    stop)
      shift
      cmd_stop "$@"
      ;;
    restart)
      shift
      cmd_restart "$@"
      ;;
    rebuild)
      shift
      cmd_rebuild "$@"
      ;;
    status)
      shift
      cmd_status "$@"
      ;;
    help|-h|--help)
      usage
      ;;
    *)
      echo "[ERROR] Unknown command: ${command}"
      usage
      exit 1
      ;;
  esac
}

main "$@"
