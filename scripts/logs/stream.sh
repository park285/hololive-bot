#!/usr/bin/env bash
# Docker Compose 로그 → logs/mirror/{service}.log 실시간 스트리밍 데몬
# 사용: ./scripts/logs/stream.sh [start|stop|status|daemon]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
LOG_ROOT="${REPO_ROOT}/logs"
MIRROR_DIR="${LOG_ROOT}/mirror"
PID_DIR="${LOG_ROOT}/runtime/pids"
TAIL_SCRIPT="${SCRIPT_DIR}/tail.sh"
SERVICES=(bot dispatcher-go stream-ingester llm-scheduler)
STREAM_SINCE_DEFAULT="${STREAM_SINCE:-5m}"

usage() {
  cat <<USAGE
Usage: $(basename "$0") <command>

Commands:
  start    compose log tail 백그라운드 시작
  stop     모든 tail 워커 종료
  status   실행 중인 워커 상태 확인
  daemon   systemd용 supervisor 루프
USAGE
  exit 0
}

run_service_worker() {
  local svc="$1"
  local log_file="${MIRROR_DIR}/${svc}.log"
  local since="${STREAM_SINCE_DEFAULT}"
  mkdir -p "${MIRROR_DIR}" "${PID_DIR}"

  while true; do
    if ! "${TAIL_SCRIPT}" "${svc}" --since "${since}" --tail 50 >> "${log_file}" 2>&1; then
      echo "$(date '+%Y-%m-%d %H:%M:%S') ${svc}: log stream reconnect" >> "${log_file}"
      sleep 1
    fi
    since="1m"
  done
}

do_start() {
  mkdir -p "${MIRROR_DIR}" "${PID_DIR}"
  for svc in "${SERVICES[@]}"; do
    pid_file="${PID_DIR}/${svc}.pid"
    if [[ -f "${pid_file}" ]] && kill -0 "$(cat "${pid_file}")" 2>/dev/null; then
      echo "${svc}: already running (PGID $(cat "${pid_file}"))" >&2
      continue
    fi
    setsid "${BASH_SOURCE[0]}" _service-worker "${svc}" >/dev/null 2>&1 &
    worker_pid=$!
    echo "${worker_pid}" > "${pid_file}"
    echo "${svc}: started (PGID ${worker_pid}) -> ${MIRROR_DIR}/${svc}.log" >&2
  done
}

do_stop() {
  if [[ ! -d "${PID_DIR}" ]]; then
    echo "no running workers" >&2
    exit 0
  fi

  for pid_file in "${PID_DIR}"/*.pid; do
    [[ -f "${pid_file}" ]] || continue
    name="$(basename "${pid_file}" .pid)"
    pid="$(cat "${pid_file}")"
    if kill -0 "${pid}" 2>/dev/null; then
      kill -- -"${pid}" 2>/dev/null || kill "${pid}" 2>/dev/null || true
      echo "${name}: stopped (PGID ${pid})" >&2
    fi
    rm -f "${pid_file}"
  done
}

do_status() {
  if [[ ! -d "${PID_DIR}" ]]; then
    echo "no worker running"
    exit 0
  fi

  for svc in "${SERVICES[@]}"; do
    pid_file="${PID_DIR}/${svc}.pid"
    if [[ -f "${pid_file}" ]] && kill -0 "$(cat "${pid_file}")" 2>/dev/null; then
      echo "${svc}: running (PGID $(cat "${pid_file}"))"
    else
      echo "${svc}: stopped"
    fi
  done
}

do_daemon() {
  mkdir -p "${MIRROR_DIR}" "${PID_DIR}"
  while true; do
    "${BASH_SOURCE[0]}" start >/dev/null 2>&1 || true
    sleep 10
  done
}

case "${1:-}" in
  start) do_start ;;
  stop) do_stop ;;
  status) do_status ;;
  daemon) do_daemon ;;
  _service-worker)
    shift
    run_service_worker "$1"
    ;;
  *) usage ;;
esac
