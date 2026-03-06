#!/usr/bin/env bash
# Outbox per-room 모드 canary 로그 요약/헬스체크
# 사용 예:
#   ./scripts/logs/check-outbox-per-room.sh --since 30m
#   ./scripts/logs/check-outbox-per-room.sh --since 1h --warn-failure-rate 0.05
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QUERY_SCRIPT="${SCRIPT_DIR}/query.sh"

SERVICE="stream-ingester"
SINCE="30m"
LIMIT="5000"
WARN_FAILURE_RATE="0.10"
MAX_AGGREGATE_FAILURES="0"
MAX_ENQUEUE_FAILURES="0"
MIN_DELIVERY_CLAIMED="1"

usage() {
  cat <<EOF
Usage: $(basename "$0") [options]

Options:
  --since <duration>            로그 조회 범위 (기본: 30m)
  --limit <n>                   조회 최대 건수 (기본: 5000)
  --service <name>              서비스명 (기본: stream-ingester)
  --warn-failure-rate <float>   경고 임계 실패율 (기본: 0.10)
  --max-aggregate-failures <n>  aggregate_failures 허용 상한 (기본: 0)
  --max-enqueue-failures <n>    enqueue_failures 허용 상한 (기본: 0)
  --min-delivery-claimed <n>    최소 delivery_claimed 기준 (기본: 1)
  -h, --help                    도움말
EOF
  exit 0
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --since) SINCE="$2"; shift 2 ;;
    --limit) LIMIT="$2"; shift 2 ;;
    --service) SERVICE="$2"; shift 2 ;;
    --warn-failure-rate) WARN_FAILURE_RATE="$2"; shift 2 ;;
    --max-aggregate-failures) MAX_AGGREGATE_FAILURES="$2"; shift 2 ;;
    --max-enqueue-failures) MAX_ENQUEUE_FAILURES="$2"; shift 2 ;;
    --min-delivery-claimed) MIN_DELIVERY_CLAIMED="$2"; shift 2 ;;
    -h|--help) usage ;;
    *) echo "ERROR: unknown arg: $1" >&2; usage ;;
  esac
done

if [[ ! -x "${QUERY_SCRIPT}" ]]; then
  echo "ERROR: query script not executable: ${QUERY_SCRIPT}" >&2
  exit 1
fi

RAW_LOGS="$("${QUERY_SCRIPT}" "${SERVICE}" --since "${SINCE}" --limit "${LIMIT}" --grep "Outbox per-room (enqueue completed|dispatch completed)" --quiet)"

if [[ -z "${RAW_LOGS}" ]]; then
  echo "NO_DATA: no per-room logs found (service=${SERVICE}, since=${SINCE})"
  exit 2
fi

SUMMARY="$(printf '%s\n' "${RAW_LOGS}" | awk '
function value(line, key,   n,i,a,p) {
  n=split(line, a, " ")
  for (i=1; i<=n; i++) {
    if (index(a[i], key "=") == 1) {
      p=index(a[i], "=")
      return substr(a[i], p+1) + 0
    }
  }
  return 0
}
{
  if (index($0, "Outbox per-room enqueue completed") > 0) {
    enqueue_count++
    enqueue_outbox_claimed += value($0, "outbox_claimed")
    enqueue_outbox_enqueued += value($0, "outbox_enqueued")
    enqueue_no_subscribers += value($0, "outbox_no_subscribers")
    enqueue_failures += value($0, "enqueue_failures")
    enqueue_target_rooms += value($0, "target_rooms")
  }
  if (index($0, "Outbox per-room dispatch completed") > 0) {
    dispatch_count++
    dispatch_claimed += value($0, "delivery_claimed")
    dispatch_sent += value($0, "delivery_sent")
    dispatch_failed += value($0, "delivery_failed")
    dispatch_outbox_touched += value($0, "outbox_touched")
    dispatch_aggregate_failures += value($0, "aggregate_failures")
  }
}
END {
  printf("enqueue_count=%d\n", enqueue_count)
  printf("enqueue_outbox_claimed=%d\n", enqueue_outbox_claimed)
  printf("enqueue_outbox_enqueued=%d\n", enqueue_outbox_enqueued)
  printf("enqueue_no_subscribers=%d\n", enqueue_no_subscribers)
  printf("enqueue_failures=%d\n", enqueue_failures)
  printf("enqueue_target_rooms=%d\n", enqueue_target_rooms)
  printf("dispatch_count=%d\n", dispatch_count)
  printf("dispatch_claimed=%d\n", dispatch_claimed)
  printf("dispatch_sent=%d\n", dispatch_sent)
  printf("dispatch_failed=%d\n", dispatch_failed)
  printf("dispatch_outbox_touched=%d\n", dispatch_outbox_touched)
  printf("dispatch_aggregate_failures=%d\n", dispatch_aggregate_failures)
}')"

eval "${SUMMARY}"

if [[ "${dispatch_claimed}" -gt 0 ]]; then
  failure_rate="$(awk -v f="${dispatch_failed}" -v c="${dispatch_claimed}" 'BEGIN { printf "%.6f", f/c }')"
else
  failure_rate="0.000000"
fi

echo "=== Outbox Per-Room Canary Summary ==="
echo "service=${SERVICE} since=${SINCE} limit=${LIMIT}"
echo "enqueue_count=${enqueue_count} claimed=${enqueue_outbox_claimed} enqueued=${enqueue_outbox_enqueued} no_subscribers=${enqueue_no_subscribers} enqueue_failures=${enqueue_failures} target_rooms=${enqueue_target_rooms}"
echo "dispatch_count=${dispatch_count} claimed=${dispatch_claimed} sent=${dispatch_sent} failed=${dispatch_failed} outbox_touched=${dispatch_outbox_touched} aggregate_failures=${dispatch_aggregate_failures}"
echo "delivery_failure_rate=${failure_rate} (warn_threshold=${WARN_FAILURE_RATE})"
echo "thresholds: min_delivery_claimed=${MIN_DELIVERY_CLAIMED} max_aggregate_failures=${MAX_AGGREGATE_FAILURES} max_enqueue_failures=${MAX_ENQUEUE_FAILURES}"

WARN=0
if [[ "${dispatch_count}" -eq 0 ]]; then
  echo "WARN: no dispatch summary logs found"
  WARN=1
fi
if [[ "${dispatch_aggregate_failures}" -gt "${MAX_AGGREGATE_FAILURES}" ]]; then
  echo "WARN: aggregate_failures too high (${dispatch_aggregate_failures} > ${MAX_AGGREGATE_FAILURES})"
  WARN=1
fi
if [[ "${enqueue_failures}" -gt "${MAX_ENQUEUE_FAILURES}" ]]; then
  echo "WARN: enqueue_failures too high (${enqueue_failures} > ${MAX_ENQUEUE_FAILURES})"
  WARN=1
fi
if [[ "${dispatch_claimed}" -lt "${MIN_DELIVERY_CLAIMED}" ]]; then
  echo "WARN: insufficient delivery sample (${dispatch_claimed} < ${MIN_DELIVERY_CLAIMED})"
  WARN=1
fi
if awk -v fr="${failure_rate}" -v th="${WARN_FAILURE_RATE}" -v c="${dispatch_claimed}" -v mc="${MIN_DELIVERY_CLAIMED}" 'BEGIN { exit !((c >= mc) && (fr > th)) }'; then
  echo "WARN: delivery failure rate too high (${failure_rate} > ${WARN_FAILURE_RATE}, claimed=${dispatch_claimed})"
  WARN=1
fi

exit "${WARN}"
