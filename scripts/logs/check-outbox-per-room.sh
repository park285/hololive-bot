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

usage() {
  cat <<EOF
Usage: $(basename "$0") [options]

Options:
  --since <duration>            로그 조회 범위 (기본: 30m)
  --limit <n>                   조회 최대 건수 (기본: 5000)
  --service <name>              서비스명 (기본: stream-ingester)
  --warn-failure-rate <float>   경고 임계 실패율 (기본: 0.10)
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
    -h|--help) usage ;;
    *) echo "ERROR: unknown arg: $1" >&2; usage ;;
  esac
done

if [[ ! -x "${QUERY_SCRIPT}" ]]; then
  echo "ERROR: query script not executable: ${QUERY_SCRIPT}" >&2
  exit 1
fi

RAW_LOGS="$("${QUERY_SCRIPT}" "${SERVICE}" --since "${SINCE}" --limit "${LIMIT}" --grep "Outbox per-room (enqueue completed|dispatch completed)")"

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

WARN=0
if [[ "${dispatch_count}" -eq 0 ]]; then
  echo "WARN: no dispatch summary logs found"
  WARN=1
fi
if [[ "${dispatch_aggregate_failures}" -gt 0 ]]; then
  echo "WARN: aggregate_failures > 0 (${dispatch_aggregate_failures})"
  WARN=1
fi
if awk -v fr="${failure_rate}" -v th="${WARN_FAILURE_RATE}" 'BEGIN { exit !(fr > th) }'; then
  echo "WARN: delivery failure rate too high (${failure_rate} > ${WARN_FAILURE_RATE})"
  WARN=1
fi

exit "${WARN}"
