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

ensure_logcli() {
  if ! command -v logcli &>/dev/null; then
    echo "ERROR: logcli 미설치" >&2
    echo "설치: go install github.com/grafana/loki/v3/cmd/logcli@latest" >&2
    exit 1
  fi
}

ensure_port_forward() {
  if curl -sf "${LOKI_ADDR}/ready" &>/dev/null; then
    return 0
  fi

  echo "port-forward 시작: svc/loki ${LOKI_PORT}:${LOKI_PORT}" >&2
  kubectl port-forward -n "${NAMESPACE}" svc/loki "${LOKI_PORT}:${LOKI_PORT}" &>/dev/null &
  local pf_pid=$!
  echo "${pf_pid}" > "${PID_DIR}/port-forward.pid"

  for _ in $(seq 1 20); do
    if curl -sf "${LOKI_ADDR}/ready" &>/dev/null; then
      echo "port-forward 준비 완료 (PID ${pf_pid})" >&2
      return 0
    fi
    sleep 0.5
  done

  echo "ERROR: Loki port-forward 연결 실패" >&2
  kill "${pf_pid}" 2>/dev/null || true
  rm -f "${PID_DIR}/port-forward.pid"
  exit 1
}

do_start() {
  ensure_logcli
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

  ensure_port_forward

  for svc in "${!SERVICE_MAP[@]}"; do
    local container="${SERVICE_MAP[${svc}]}"
    local log_file="${LOG_DIR}/${svc}.log"
    local pid_file="${PID_DIR}/${svc}.pid"

    # 이미 실행 중이면 스킵
    if [[ -f "${pid_file}" ]] && kill -0 "$(cat "${pid_file}")" 2>/dev/null; then
      echo "${svc}: 이미 실행 중 (PID $(cat "${pid_file}"))" >&2
      continue
    fi

    local query="{kubernetes_namespace_name=\"${NAMESPACE}\", kubernetes_container_name=\"${container}\"}"

    # logcli tail → jq(message 추출) → sed(ANSI 제거) → 파일 append
    # setsid로 새 프로세스 그룹 생성 → stop 시 PGID 기반 kill로 파이프라인 전체 정리
    setsid bash -c "
      LOKI_ADDR=\"${LOKI_ADDR}\" logcli query \
        --tail \
        --no-labels \
        --output=raw \
        --quiet \
        '${query}' 2>/dev/null \
        | jq -r --unbuffered '.message // .' 2>/dev/null \
        | sed -u 's/\x1b\[[0-9;]*m//g' \
        >> '${log_file}'
    " &
    local tail_pid=$!
    echo "${tail_pid}" > "${pid_file}"
    echo "${svc}: 시작 (PGID ${tail_pid}) → ${log_file}" >&2
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

case "${1:-}" in
  start)  do_start ;;
  stop)   do_stop ;;
  status) do_status ;;
  -h|--help|"") usage ;;
  *) echo "ERROR: 알 수 없는 명령: $1" >&2; usage ;;
esac
