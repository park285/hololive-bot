#!/usr/bin/env bash
# Docker Compose 로그 실시간 tail 래퍼
# 사용: ./scripts/logs/tail.sh <service> [--since 1h] [--tail 200]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./common.sh
source "${SCRIPT_DIR}/common.sh"

usage() {
  cat <<USAGE
Usage: $(basename "$0") <service> [options]

Services: ${!SERVICE_MAP[*]}

Options:
  --since <duration>   조회 범위 (기본: 1h)
  --tail <n>          시작 시 보여줄 최대 줄 수 (기본: 200)
  -h, --help          도움말
USAGE
  exit 0
}

SERVICE=""
SINCE="1h"
TAIL_LINES="200"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage ;;
    --since) SINCE="$2"; shift 2 ;;
    --tail) TAIL_LINES="$2"; shift 2 ;;
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

echo "tail: service=${SERVICE_NAME} since=${SINCE} tail=${TAIL_LINES} mode=${COMPOSE_MODE}" >&2
exec "${COMPOSE_CMD[@]}" -f "${COMPOSE_FILE}" logs \
  --follow \
  --no-color \
  --no-log-prefix \
  --timestamps \
  --since "${SINCE}" \
  --tail "${TAIL_LINES}" \
  "${SERVICE_NAME}"
