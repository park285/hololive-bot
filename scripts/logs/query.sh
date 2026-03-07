#!/usr/bin/env bash
# Docker Compose 로그 시간범위 조회 래퍼
# 사용: ./scripts/logs/query.sh <service> [--since 1h] [--limit 1000] [--grep "pattern"]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./common.sh
source "${SCRIPT_DIR}/common.sh"

usage() {
  cat <<USAGE
Usage: $(basename "$0") <service> [options]

Services: ${!SERVICE_MAP[*]}

Options:
  --since <duration>   조회 범위 (기본: 1h). 예: 30m, 2h, 1d
  --limit <n>          최대 줄 수 (기본: 1000)
  --grep <pattern>     client-side 정규식 필터
  --quiet              진행 로그 숨김
  -h, --help           도움말

Examples:
  $(basename "$0") dispatcher --since 2h --grep "ERROR"
  $(basename "$0") bot --limit 500
  $(basename "$0") ingester --since 1d --grep "failed"
USAGE
  exit 0
}

SERVICE=""
SINCE="1h"
LIMIT="1000"
GREP_PATTERN=""
QUIET="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage ;;
    --since) SINCE="$2"; shift 2 ;;
    --limit) LIMIT="$2"; shift 2 ;;
    --grep) GREP_PATTERN="$2"; shift 2 ;;
    --quiet) QUIET="true"; shift ;;
    *)
      if [[ -z "${SERVICE}" ]]; then
        SERVICE="$1"
        shift
      else
        echo "ERROR: unknown arg: $1" >&2
        exit 1
      fi
      ;;
  esac
done

if [[ -z "${SERVICE}" ]]; then
  echo "ERROR: service is required" >&2
  usage
fi

resolve_compose_cmd
SERVICE_NAME="$(resolve_service "${SERVICE}")"

if [[ "${QUIET}" != "true" ]]; then
  echo "query: service=${SERVICE_NAME} since=${SINCE} limit=${LIMIT} mode=${COMPOSE_MODE}" >&2
fi

OUTPUT="$("${COMPOSE_CMD[@]}" -f "${COMPOSE_FILE}" logs \
  --no-color \
  --no-log-prefix \
  --timestamps \
  --since "${SINCE}" \
  --tail "${LIMIT}" \
  "${SERVICE_NAME}" 2>/dev/null || true)"

if [[ -n "${GREP_PATTERN}" ]]; then
  printf '%s\n' "${OUTPUT}" | grep -E -- "${GREP_PATTERN}" || true
else
  printf '%s\n' "${OUTPUT}"
fi
