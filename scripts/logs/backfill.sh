#!/usr/bin/env bash
# Loki 최근 로그를 일회성 스냅샷 파일로 저장
# 사용: ./scripts/logs/backfill.sh <service> [--since 24h] [--limit 5000] [--output path]
set -euo pipefail

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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SNAPSHOT_DIR="${REPO_ROOT}/logs/backfill"

usage() {
  cat <<EOF
Usage: $(basename "$0") <service> [options]

Services: ${!SERVICE_MAP[*]}

Options:
  --since <duration>   Loki 시간 범위 (기본: 24h)
  --limit <n>          최대 결과 수 (기본: 5000)
  --output <path>      출력 파일 경로 (기본: logs/backfill/<service>-<timestamp>.log)
  --stdout             파일 저장 대신 stdout 출력
  -h, --help           도움말

Examples:
  $(basename "$0") bot
  $(basename "$0") bot --since 6h
  $(basename "$0") ingester --since 2h --stdout
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

ensure_port_forward() {
  if curl -sf "${LOKI_ADDR}/ready" &>/dev/null; then
    return 0
  fi

  echo "port-forward 시작: svc/loki ${LOKI_PORT}:${LOKI_PORT}" >&2
  kubectl port-forward -n "${NAMESPACE}" svc/loki "${LOKI_PORT}:${LOKI_PORT}" &>/dev/null &
  PF_PID=$!

  for i in $(seq 1 20); do
    if curl -sf "${LOKI_ADDR}/ready" &>/dev/null; then
      echo "port-forward 준비 완료" >&2
      return 0
    fi
    if [[ $i -eq 20 ]]; then
      echo "ERROR: Loki port-forward 연결 실패" >&2
      exit 1
    fi
    sleep 0.5
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

require_command "logcli" "go install github.com/grafana/loki/v3/cmd/logcli@latest"
require_command "kubectl"
require_command "curl"
require_command "jq"
require_command "sed"

SERVICE=""
SINCE="24h"
LIMIT="5000"
OUTPUT=""
STDOUT_ONLY="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage ;;
    --since) SINCE="$2"; shift 2 ;;
    --limit) LIMIT="$2"; shift 2 ;;
    --output) OUTPUT="$2"; shift 2 ;;
    --stdout) STDOUT_ONLY="true"; shift ;;
    *)
      if [[ -z "${SERVICE}" ]]; then
        SERVICE="$1"
        shift
      else
        echo "ERROR: 알 수 없는 인자: $1" >&2
        exit 1
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

ensure_port_forward

QUERY="{kubernetes_namespace_name=\"${NAMESPACE}\", kubernetes_container_name=\"${CONTAINER}\"}"

if [[ "${STDOUT_ONLY}" == "true" ]]; then
  LOKI_ADDR="${LOKI_ADDR}" logcli query \
    --quiet \
    --since="${SINCE}" \
    --limit="${LIMIT}" \
    --output=raw \
    "${QUERY}" 2>/dev/null | format_loki_stream
  exit 0
fi

mkdir -p "${SNAPSHOT_DIR}"
if [[ -z "${OUTPUT}" ]]; then
  OUTPUT="${SNAPSHOT_DIR}/${SERVICE}-$(date +%Y%m%d-%H%M%S).log"
fi

TMP_FILE="$(mktemp)"
trap 'rm -f "${TMP_FILE}"; cleanup' EXIT INT TERM

LOKI_ADDR="${LOKI_ADDR}" logcli query \
  --quiet \
  --since="${SINCE}" \
  --limit="${LIMIT}" \
  --output=raw \
  "${QUERY}" 2>/dev/null | format_loki_stream > "${TMP_FILE}"

mv "${TMP_FILE}" "${OUTPUT}"
echo "backfill saved: ${OUTPUT}" >&2
