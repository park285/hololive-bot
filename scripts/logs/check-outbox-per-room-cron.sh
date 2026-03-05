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
MAX_AGGREGATE_FAILURES="${OUTBOX_CANARY_MAX_AGGREGATE_FAILURES:-0}"
MAX_ENQUEUE_FAILURES="${OUTBOX_CANARY_MAX_ENQUEUE_FAILURES:-0}"
MIN_DELIVERY_CLAIMED="${OUTBOX_CANARY_MIN_DELIVERY_CLAIMED:-10}"
ALLOW_NO_DATA="${OUTBOX_CANARY_ALLOW_NO_DATA:-true}"

mkdir -p "${LOG_DIR}"

set +e
RESULT="$("${CHECK_SCRIPT}" \
  --service "${SERVICE}" \
  --since "${SINCE}" \
  --limit "${LIMIT}" \
  --warn-failure-rate "${WARN_FAILURE_RATE}" \
  --max-aggregate-failures "${MAX_AGGREGATE_FAILURES}" \
  --max-enqueue-failures "${MAX_ENQUEUE_FAILURES}" \
  --min-delivery-claimed "${MIN_DELIVERY_CLAIMED}" 2>&1)"
EXIT_CODE=$?
set -e

if [[ "${EXIT_CODE}" -eq 2 && "${ALLOW_NO_DATA}" == "true" ]]; then
  RESULT="NO_DATA allowed: ${RESULT}"
  EXIT_CODE=0
fi

printf '%s [%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "outbox-per-room-canary" "${RESULT}" >> "${SUMMARY_LOG}"

if [[ ${EXIT_CODE} -ne 0 ]]; then
  echo "WARN: outbox per-room canary check returned ${EXIT_CODE}" >&2
fi

exit ${EXIT_CODE}
