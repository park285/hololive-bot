#!/usr/bin/env bash
# Loki 로그 → logs/{service}.log 실시간 스트리밍 데몬
# 사용: ./scripts/logs/stream.sh [start|stop|status]
# 4개 서비스(bot, dispatcher-go, stream-ingester, llm-scheduler)의
# logcli tail을 백그라운드로 실행하여 logs/ 디렉토리에 append
set -euo pipefail

export PATH="${HOME}/go/bin:/usr/local/bin:/usr/bin:/bin:${PATH}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
LOG_DIR="${REPO_ROOT}/logs"
PID_DIR="${LOG_DIR}/.pids"

declare -A SERVICE_MAP=(
  [bot]="hololive-bot"
  [dispatcher-go]="dispatcher-go"
  [stream-ingester]="stream-ingester"
  [llm-scheduler]="llm-scheduler"
)

LOKI_PORT=3100
LOKI_ADDR="http://127.0.0.1:${LOKI_PORT}"
NAMESPACE="hololive"

usage() {
  cat <<EOF
Usage: $(basename "$0") <command>

Commands:
  start    logcli tail 백그라운드 시작 (4개 서비스)
  stop     모든 logcli tail 프로세스 종료
  status   실행 중인 프로세스 상태 확인

로그 파일: ${LOG_DIR}/{bot,dispatcher-go,stream-ingester,llm-scheduler}.log
EOF
  exit 0
}

require_command() {
  local name="$1"
  local install_hint="${2:-}"
  if command -v "${name}" &>/dev/null; then
    return 0
  fi

  echo "ERROR: ${name} 미설치" >&2
  if [[ -n "${install_hint}" ]]; then
    echo "설치: ${install_hint}" >&2
  fi
  exit 1
}

ensure_start_dependencies() {
  require_command "logcli" "go install github.com/grafana/loki/v3/cmd/logcli@latest"
  require_command "kubectl"
  require_command "curl"
  require_command "jq"
  require_command "sed"
}

wait_for_loki_ready() {
  local retries="${1:-60}"
  local delay_secs="${2:-1}"
  local i
  for i in $(seq 1 "${retries}"); do
    if curl -sf "${LOKI_ADDR}/ready" &>/dev/null; then
      return 0
    fi
    sleep "${delay_secs}"
  done
  return 1
}

run_port_forward_supervisor() {
  mkdir -p "${LOG_DIR}" "${PID_DIR}"

  while true; do
    if curl -sf "${LOKI_ADDR}/ready" &>/dev/null; then
      sleep 2
      continue
    fi

    echo "port-forward 시작: svc/loki ${LOKI_PORT}:${LOKI_PORT}" >&2
    if kubectl port-forward -n "${NAMESPACE}" svc/loki "${LOKI_PORT}:${LOKI_PORT}" &>/dev/null; then
      echo "port-forward 종료 감지: 재시작 대기" >&2
    else
      echo "port-forward 실패: 재시도 대기" >&2
    fi
    sleep 1
  done
}

format_loki_stream() {
  jq -Rr '
    . as $line
    | ($line | fromjson?) as $json
    | if $json == null then
        $line
      elif ($json | type) == "object" then
        ($json.message // $json.log // $json.msg // $line)
      else
        ($json | tostring)
      end
  ' | sed -u 's/\x1b\[[0-9;]*m//g'
}

run_service_worker() {
  local svc="$1"
  local container="${SERVICE_MAP[${svc}]:-}"
  local log_file="${LOG_DIR}/${svc}.log"
  local query

  if [[ -z "${container}" ]]; then
    echo "ERROR: 알 수 없는 서비스 워커: ${svc}" >&2
    exit 1
  fi

  mkdir -p "${LOG_DIR}" "${PID_DIR}"
  query="{kubernetes_namespace_name=\"${NAMESPACE}\", kubernetes_container_name=\"${container}\"}"

  while true; do
    if ! wait_for_loki_ready 60 1; then
      echo "$(date '+%Y-%m-%d %H:%M:%S') ${svc}: Loki 준비 대기 시간 초과, 재시도" >> "${log_file}"
      sleep 1
      continue
    fi

    if ! LOKI_ADDR="${LOKI_ADDR}" logcli query \
      --tail \
      --no-labels \
      --output=raw \
      --quiet \
      "${query}" 2>/dev/null \
      | format_loki_stream >> "${log_file}"; then
      echo "$(date '+%Y-%m-%d %H:%M:%S') ${svc}: 로그 스트림 재연결" >> "${log_file}"
      sleep 1
      continue
    fi

    sleep 1
  done
}

do_start() {
  ensure_start_dependencies
  mkdir -p "${LOG_DIR}" "${PID_DIR}"

  # 이미 실행 중인 프로세스 확인
  local running=0
  for svc in "${!SERVICE_MAP[@]}"; do
    local pid_file="${PID_DIR}/${svc}.pid"
    if [[ -f "${pid_file}" ]] && kill -0 "$(cat "${pid_file}")" 2>/dev/null; then
      running=$((running + 1))
    fi
  done
  if [[ ${running} -eq ${#SERVICE_MAP[@]} ]]; then
    echo "모든 서비스가 이미 실행 중입니다. 'status'로 확인하세요." >&2
    exit 0
  fi

  local pf_pid_file="${PID_DIR}/port-forward.pid"
  if [[ ! -f "${pf_pid_file}" ]] || ! kill -0 "$(cat "${pf_pid_file}")" 2>/dev/null; then
    setsid "${BASH_SOURCE[0]}" _port-forward-supervisor &>/dev/null &
    local pf_pid=$!
    echo "${pf_pid}" > "${pf_pid_file}"
    if ! wait_for_loki_ready 20 0.5; then
      echo "ERROR: Loki port-forward 연결 실패" >&2
      kill -- -"${pf_pid}" 2>/dev/null || kill "${pf_pid}" 2>/dev/null || true
      rm -f "${pf_pid_file}"
      exit 1
    fi
    echo "port-forward 준비 완료 (PID ${pf_pid})" >&2
  else
    echo "port-forward: 이미 실행 중 (PID $(cat "${pf_pid_file}"))" >&2
  fi

  for svc in "${!SERVICE_MAP[@]}"; do
    local pid_file="${PID_DIR}/${svc}.pid"

    # 이미 실행 중이면 스킵
    if [[ -f "${pid_file}" ]] && kill -0 "$(cat "${pid_file}")" 2>/dev/null; then
      echo "${svc}: 이미 실행 중 (PID $(cat "${pid_file}"))" >&2
      continue
    fi

    setsid "${BASH_SOURCE[0]}" _service-worker "${svc}" &>/dev/null &
    local worker_pid=$!
    echo "${worker_pid}" > "${pid_file}"
    echo "${svc}: 시작 (PGID ${worker_pid}) → ${LOG_DIR}/${svc}.log" >&2
  done

  echo "스트리밍 시작 완료. 'tail -f ${LOG_DIR}/*.log'로 확인 가능" >&2
}

do_stop() {
  if [[ ! -d "${PID_DIR}" ]]; then
    echo "실행 중인 프로세스 없음" >&2
    exit 0
  fi

  for pid_file in "${PID_DIR}"/*.pid; do
    [[ -f "${pid_file}" ]] || continue
    local name
    name="$(basename "${pid_file}" .pid)"
    local pid
    pid="$(cat "${pid_file}")"
    if kill -0 "${pid}" 2>/dev/null; then
      # setsid로 생성된 프로세스 그룹 전체 kill (logcli + jq + sed)
      kill -- -"${pid}" 2>/dev/null || kill "${pid}" 2>/dev/null || true
      echo "${name}: 종료 (PGID ${pid})" >&2
    else
      echo "${name}: 이미 종료됨 (PGID ${pid})" >&2
    fi
    rm -f "${pid_file}"
  done

  echo "스트리밍 중지 완료" >&2
}

do_status() {
  if [[ ! -d "${PID_DIR}" ]]; then
    echo "실행 중인 프로세스 없음" >&2
    exit 0
  fi

  local any_running=false
  for svc in "${!SERVICE_MAP[@]}"; do
    local pid_file="${PID_DIR}/${svc}.pid"
    if [[ -f "${pid_file}" ]]; then
      local pid
      pid="$(cat "${pid_file}")"
      if kill -0 "${pid}" 2>/dev/null; then
        local log_file="${LOG_DIR}/${svc}.log"
        local size="0"
        [[ -f "${log_file}" ]] && size="$(stat -c%s "${log_file}" 2>/dev/null || echo 0)"
        echo "${svc}: running (PGID ${pid}, log ${size} bytes)" >&2
        any_running=true
      else
        echo "${svc}: dead (PID ${pid})" >&2
        rm -f "${pid_file}"
      fi
    else
      echo "${svc}: stopped" >&2
    fi
  done

  # port-forward 상태
  local pf_pid_file="${PID_DIR}/port-forward.pid"
  if [[ -f "${pf_pid_file}" ]] && kill -0 "$(cat "${pf_pid_file}")" 2>/dev/null; then
    echo "port-forward: running (PID $(cat "${pf_pid_file}"))" >&2
  fi

  if ! ${any_running}; then
    echo "실행 중인 스트리머 없음" >&2
  fi
}

do_daemon() {
  ensure_start_dependencies
  mkdir -p "${LOG_DIR}" "${PID_DIR}"

  local -a child_pids=()

  cleanup_daemon() {
    local pid
    for pid in "${child_pids[@]:-}"; do
      if kill -0 "${pid}" 2>/dev/null; then
        kill "${pid}" 2>/dev/null || true
        wait "${pid}" 2>/dev/null || true
      fi
    done
    rm -f "${PID_DIR}/"*.pid 2>/dev/null || true
  }

  trap cleanup_daemon EXIT INT TERM

  run_port_forward_supervisor &
  local pf_pid=$!
  child_pids+=("${pf_pid}")
  echo "${pf_pid}" > "${PID_DIR}/port-forward.pid"

  if ! wait_for_loki_ready 20 0.5; then
    echo "ERROR: Loki port-forward 연결 실패" >&2
    exit 1
  fi

  local svc
  for svc in "${!SERVICE_MAP[@]}"; do
    run_service_worker "${svc}" &
    local worker_pid=$!
    child_pids+=("${worker_pid}")
    echo "${worker_pid}" > "${PID_DIR}/${svc}.pid"
    echo "${svc}: daemon worker 시작 (PID ${worker_pid}) → ${LOG_DIR}/${svc}.log" >&2
  done

  while true; do
    if ! wait -n; then
      echo "ERROR: 로그 스트림 worker 비정상 종료 감지" >&2
      exit 1
    fi
  done
}

case "${1:-}" in
  start)  do_start ;;
  stop)   do_stop ;;
  status) do_status ;;
  daemon) do_daemon ;;
  _port-forward-supervisor) run_port_forward_supervisor ;;
  _service-worker)
    [[ $# -eq 2 ]] || { echo "ERROR: 서비스 워커 인자 필요" >&2; exit 1; }
    run_service_worker "$2"
    ;;
  -h|--help|"") usage ;;
  *) echo "ERROR: 알 수 없는 명령: $1" >&2; usage ;;
esac
