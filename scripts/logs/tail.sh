#!/usr/bin/env bash
# Loki 로그 실시간 tail (logcli 래퍼)
# 사용: ./scripts/logs/tail.sh <service> [--since 1h] [--pod <pod_name>]
set -euo pipefail

# --- 서비스명 → container_name 매핑 ---
declare -A SERVICE_MAP=(
  [bot]="hololive-bot"
  [dispatcher]="dispatcher-go"
  [dispatcher-go]="dispatcher-go"
  [ingester]="stream-ingester"
  [stream-ingester]="stream-ingester"
  [llm]="llm-scheduler"
  [llm-scheduler]="llm-scheduler"
)

LOKI_PORT=3100
LOKI_ADDR="http://127.0.0.1:${LOKI_PORT}"
NAMESPACE="hololive"
PF_PID=""

usage() {
  cat <<EOF
Usage: $(basename "$0") <service> [options]

Services: ${!SERVICE_MAP[*]}

Options:
  --since <duration>   Loki 시간 범위 (기본: 1h). 예: 30m, 2h, 1d
  --pod <pod_name>     특정 pod 지정 (container_name 대신 pod_name 필터)
  -h, --help           도움말

Examples:
  $(basename "$0") bot
  $(basename "$0") dispatcher --since 30m
  $(basename "$0") bot --pod hololive-bot-abc123
EOF
  exit 0
}

cleanup() {
  if [[ -n "${PF_PID}" ]] && kill -0 "${PF_PID}" 2>/dev/null; then
    kill "${PF_PID}" 2>/dev/null || true
    wait "${PF_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

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

# --- logcli 사전 조건 확인 ---
if ! command -v logcli &>/dev/null; then
  echo "ERROR: logcli 미설치" >&2
  echo "설치: go install github.com/grafana/loki/v3/cmd/logcli@latest" >&2
  exit 1
fi
if ! command -v jq &>/dev/null; then
  echo "ERROR: jq 미설치" >&2
  exit 1
fi

# --- 인자 파싱 ---
SERVICE=""
SINCE="1h"
POD=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage ;;
    --since) SINCE="$2"; shift 2 ;;
    --pod) POD="$2"; shift 2 ;;
    *)
      if [[ -z "${SERVICE}" ]]; then
        SERVICE="$1"; shift
      else
        echo "ERROR: 알 수 없는 인자: $1" >&2; exit 1
      fi
      ;;
  esac
done

if [[ -z "${SERVICE}" ]]; then
  echo "ERROR: 서비스명 필수" >&2
  usage
fi

CONTAINER="${SERVICE_MAP[${SERVICE}]:-}"
if [[ -z "${CONTAINER}" ]]; then
  echo "ERROR: 알 수 없는 서비스: ${SERVICE}" >&2
  echo "사용 가능: ${!SERVICE_MAP[*]}" >&2
  exit 1
fi

# --- port-forward 시작 (이미 열려있으면 스킵) ---
if ! curl -sf "${LOKI_ADDR}/ready" &>/dev/null; then
  echo "port-forward 시작: svc/loki ${LOKI_PORT}:${LOKI_PORT}" >&2
  kubectl port-forward -n "${NAMESPACE}" svc/loki "${LOKI_PORT}:${LOKI_PORT}" &>/dev/null &
  PF_PID=$!
  # port-forward 준비 대기 (최대 10초)
  for i in $(seq 1 20); do
    if curl -sf "${LOKI_ADDR}/ready" &>/dev/null; then break; fi
    if [[ $i -eq 20 ]]; then
      echo "ERROR: Loki port-forward 연결 실패" >&2
      exit 1
    fi
    sleep 0.5
  done
  echo "port-forward 준비 완료" >&2
fi

# --- Loki 쿼리 구성 ---
if [[ -n "${POD}" ]]; then
  QUERY="{kubernetes_namespace_name=\"${NAMESPACE}\", kubernetes_pod_name=\"${POD}\"}"
else
  QUERY="{kubernetes_namespace_name=\"${NAMESPACE}\", kubernetes_container_name=\"${CONTAINER}\"}"
fi

echo "tail: ${QUERY} (since ${SINCE})" >&2

# --- logcli tail 실행 ---
LOKI_ADDR="${LOKI_ADDR}" logcli query \
  --tail \
  --since="${SINCE}" \
  --no-labels \
  --output=raw \
  --quiet \
  "${QUERY}" 2>/dev/null | format_loki_stream
