#!/usr/bin/env bash
# Docker Compose 최근 로그를 일회성 스냅샷 파일로 저장
# 사용: ./scripts/logs/backfill.sh <service> [--since 24h] [--limit 5000] [--output path]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QUERY_SCRIPT="${SCRIPT_DIR}/query.sh"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SNAPSHOT_DIR="${REPO_ROOT}/logs/backfill"
RETENTION_DAYS="${BACKFILL_RETENTION_DAYS:-7}"

usage() {
  cat <<USAGE
Usage: $(basename "$0") <service> [options]

Options:
  --since <duration>   조회 범위 (기본: 24h)
  --limit <n>          최대 줄 수 (기본: 5000)
  --output <path>      출력 파일 경로
  --stdout             파일 저장 대신 stdout 출력
  -h, --help           도움말
USAGE
  exit 0
}

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

if [[ "${STDOUT_ONLY}" == "true" ]]; then
  "${QUERY_SCRIPT}" "${SERVICE}" --since "${SINCE}" --limit "${LIMIT}" --quiet
  exit 0
fi

mkdir -p "${SNAPSHOT_DIR}"
if [[ -z "${OUTPUT}" ]]; then
  OUTPUT="${SNAPSHOT_DIR}/${SERVICE}-$(date +%Y%m%d-%H%M%S).log"
fi

TMP_FILE="$(mktemp)"
trap 'rm -f "${TMP_FILE}"' EXIT INT TERM
"${QUERY_SCRIPT}" "${SERVICE}" --since "${SINCE}" --limit "${LIMIT}" --quiet > "${TMP_FILE}"
mv "${TMP_FILE}" "${OUTPUT}"
find "${SNAPSHOT_DIR}" -type f -name '*.log' -mtime +"${RETENTION_DAYS}" -delete >/dev/null 2>&1 || true
echo "backfill saved: ${OUTPUT}" >&2
