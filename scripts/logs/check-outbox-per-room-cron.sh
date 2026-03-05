#!/usr/bin/env bash
# Outbox per-room canary 점검 cron 래퍼
# 예시:
#   */10 * * * * /home/kapu/gemini/hololive-bot/scripts/logs/check-outbox-per-room-cron.sh >> /home/kapu/gemini/hololive-bot/logs/outbox-per-room-canary-cron.log 2>&1
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
CHECK_SCRIPT="${SCRIPT_DIR}/check-outbox-per-room.sh"
LOG_DIR="${REPO_ROOT}/logs"
SUMMARY_LOG="${LOG_DIR}/outbox-per-room-canary.log"

SINCE="${OUTBOX_CANARY_SINCE:-30m}"
LIMIT="${OUTBOX_CANARY_LIMIT:-5000}"
WARN_FAILURE_RATE="${OUTBOX_CANARY_WARN_FAILURE_RATE:-0.10}"
SERVICE="${OUTBOX_CANARY_SERVICE:-stream-ingester}"

mkdir -p "${LOG_DIR}"

set +e
RESULT="$("${CHECK_SCRIPT}" \
  --service "${SERVICE}" \
  --since "${SINCE}" \
  --limit "${LIMIT}" \
  --warn-failure-rate "${WARN_FAILURE_RATE}" 2>&1)"
EXIT_CODE=$?
set -e

printf '%s [%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "outbox-per-room-canary" "${RESULT}" >> "${SUMMARY_LOG}"

if [[ ${EXIT_CODE} -ne 0 ]]; then
  echo "WARN: outbox per-room canary check returned ${EXIT_CODE}" >&2
fi

exit ${EXIT_CODE}
