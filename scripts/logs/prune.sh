#!/usr/bin/env bash
# logs/ 하위의 보조 로그/스냅샷/상태 파일을 정리합니다.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
LOG_ROOT="${REPO_ROOT}/logs"
BACKFILL_DIR="${LOG_ROOT}/backfill"
MIRROR_DIR="${LOG_ROOT}/mirror"
CRON_DIR="${LOG_ROOT}/cron"
CANARY_DIR="${LOG_ROOT}/canary"
PID_DIR="${LOG_ROOT}/runtime/pids"

BACKFILL_RETENTION_DAYS="${BACKFILL_RETENTION_DAYS:-7}"
AUX_RETENTION_DAYS="${AUX_RETENTION_DAYS:-30}"

if [[ -d "${BACKFILL_DIR}" ]]; then
  find "${BACKFILL_DIR}" -type f -name '*.log' -mtime +"${BACKFILL_RETENTION_DAYS}" -delete >/dev/null 2>&1 || true
fi
if [[ -d "${MIRROR_DIR}" ]]; then
  find "${MIRROR_DIR}" -type f -name '*.log.1' -mtime +"${AUX_RETENTION_DAYS}" -delete >/dev/null 2>&1 || true
fi
if [[ -d "${CRON_DIR}" ]]; then
  find "${CRON_DIR}" -type f -name '*.log*' -mtime +"${AUX_RETENTION_DAYS}" -delete >/dev/null 2>&1 || true
fi
if [[ -d "${CANARY_DIR}" ]]; then
  find "${CANARY_DIR}" -type f -name '*.log*' -mtime +"${AUX_RETENTION_DAYS}" -delete >/dev/null 2>&1 || true
fi

if [[ -d "${PID_DIR}" ]]; then
  for pid_file in "${PID_DIR}"/*.pid; do
    [[ -f "${pid_file}" ]] || continue
    pid="$(cat "${pid_file}" 2>/dev/null || true)"
    if [[ -z "${pid}" ]] || ! kill -0 "${pid}" 2>/dev/null; then
      rm -f "${pid_file}"
    fi
  done
fi

echo "prune complete: backfill>${BACKFILL_RETENTION_DAYS}d aux>${AUX_RETENTION_DAYS}d"
