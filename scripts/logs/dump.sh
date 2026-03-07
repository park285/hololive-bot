#!/usr/bin/env bash
# Docker Compose 로그를 logs/mirror/ 디렉토리에 주기적 append dump
# cron: 0 */2 * * * ENABLE_LOG_MIRROR=1 /home/kapu/gemini/hololive-bot/scripts/logs/dump.sh >> /home/kapu/gemini/hololive-bot/logs/cron/dump.log 2>&1
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QUERY_SCRIPT="${SCRIPT_DIR}/query.sh"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
MIRROR_DIR="${REPO_ROOT}/logs/mirror"

SERVICES=(bot dispatcher-go stream-ingester llm-scheduler)
SINCE="${DUMP_SINCE:-2h}"
LIMIT="${DUMP_LIMIT:-10000}"
ROTATE_BYTES=$((100 * 1024 * 1024))
RETENTION_DAYS="${DUMP_RETENTION_DAYS:-30}"
ENABLE_LOG_MIRROR="${ENABLE_LOG_MIRROR:-0}"

if [[ "${ENABLE_LOG_MIRROR}" != "1" ]]; then
  echo "log mirror disabled: set ENABLE_LOG_MIRROR=1 to enable" >&2
  exit 0
fi

mkdir -p "${MIRROR_DIR}"
DUMP_COUNT=0

for svc in "${SERVICES[@]}"; do
  log_file="${MIRROR_DIR}/${svc}.log"

  if [[ -f "${log_file}" ]]; then
    file_size=$(stat -c%s "${log_file}" 2>/dev/null || echo 0)
    if [[ ${file_size} -gt ${ROTATE_BYTES} ]]; then
      mv -f "${log_file}" "${log_file}.1"
      echo "$(date '+%Y-%m-%d %H:%M:%S') rotation: ${svc}.log -> ${svc}.log.1 (${file_size} bytes)" >&2
    fi
  fi

  tmp_file="$(mktemp)"
  trap 'rm -f "${tmp_file}"' EXIT INT TERM
  "${QUERY_SCRIPT}" "${svc}" --since "${SINCE}" --limit "${LIMIT}" --quiet > "${tmp_file}"
  line_count=$(wc -l < "${tmp_file}" | xargs)
  if [[ ${line_count} -gt 0 ]]; then
    cat "${tmp_file}" >> "${log_file}"
  fi
  rm -f "${tmp_file}"
  trap - EXIT INT TERM
  DUMP_COUNT=$((DUMP_COUNT + line_count))
  echo "$(date '+%Y-%m-%d %H:%M:%S') ${svc}: ${line_count} lines" >&2
done

find "${MIRROR_DIR}" -name '*.log.1' -mtime +"${RETENTION_DAYS}" -delete >/dev/null 2>&1 || true
echo "$(date '+%Y-%m-%d %H:%M:%S') dump complete: total ${DUMP_COUNT} lines" >&2
