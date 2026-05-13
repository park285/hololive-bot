#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

echo "[CHECK] notification egress ownership gate"

fail=0

report_hits() {
  local label="$1"
  local hits="$2"

  if [[ -n "${hits}" ]]; then
    echo "[FAIL] ${label}" >&2
    echo "${hits}" >&2
    fail=1
  else
    echo "[PASS] ${label}"
  fi
}

check_forbidden_global_go_hits() {
  local label="$1"
  local pattern="$2"
  shift 2
  local hits

  hits="$(rg -n "${pattern}" "${ROOT_DIR}" -g '*.go' "$@" || true)"
  report_hits "${label}" "${hits}"
}

check_forbidden_scoped_go_hits() {
  local label="$1"
  local pattern="$2"
  local path="$3"
  local hits

  hits="$(rg -n "${pattern}" "${ROOT_DIR}/${path}" -g '*.go' || true)"
  report_hits "${label}" "${hits}"
}

check_forbidden_global_go_hits \
  "Iris proactive sender implementation is alarm-worker internal only" \
  'NewIrisMessageSender|type .*IrisMessageSender' \
  -g '!hololive/hololive-alarm-worker/internal/egress/**' \
  -g '!hololive/hololive-alarm-worker/internal/app/**'

check_forbidden_scoped_go_hits \
  "stream-ingester does not own YouTube outbox dispatch or Iris egress capability" \
  'pkg/service/delivery|delivery\.NewIrisMessageSender|outbox\.NewDispatcher|OutboxDispatcher|YouTube outbox dispatcher started|ProvideIrisClient|iris\.WithBaseURL|iris\.WithBotToken|IrisClient:' \
  "hololive/hololive-stream-ingester"

check_forbidden_scoped_go_hits \
  "llm-sched does not start proactive delivery dispatch or Iris delivery" \
  'DeliveryDispatcher|Delivery outbox dispatcher started|NewIrisMessageSender|ProvideIrisClient|iris\.WithBaseURL|iris\.WithBotToken' \
  "hololive/hololive-llm-sched"

check_forbidden_scoped_go_hits \
  "admin-api does not start proactive delivery dispatch or Iris delivery" \
  'DeliveryDispatcher|NewIrisMessageSender|outbox\.NewDispatcher|ProvideIrisClient|iris\.WithBaseURL|iris\.WithBotToken' \
  "hololive/hololive-admin-api"

compose="${ROOT_DIR}/docker-compose.prod.yml"
for service in stream-ingester youtube-scraper llm-scheduler; do
  block="$(awk -v service="  ${service}:" '
    $0 == service {in_block=1; print; next}
    in_block && $0 ~ /^  [A-Za-z0-9_-]+:/ {exit}
    in_block {print}
  ' "${compose}")"
  if grep -Eq '\*iris-env|IRIS_BOT_TOKEN|IRIS_BASE_URL|IRIS_TRANSPORT|IRIS_H3_' <<< "${block}"; then
    echo "[FAIL] ${service} has Iris egress env in docker-compose.prod.yml" >&2
    fail=1
  else
    echo "[PASS] ${service} has no Iris egress env"
  fi
done

dispatcher_block="$(awk '
  $0 == "  dispatcher-go:" {in_block=1; print; next}
  in_block && $0 ~ /^  [A-Za-z0-9_-]+:/ {exit}
  in_block {print}
' "${compose}")"
if ! grep -Fq 'profiles: ["legacy-dispatcher-go"]' <<< "${dispatcher_block}"; then
  echo "[FAIL] dispatcher-go must be behind the legacy-dispatcher-go profile" >&2
  fail=1
else
  echo "[PASS] dispatcher-go is not in the default compose profile"
fi

alarm_worker_block="$(awk '
  $0 == "  hololive-alarm-worker:" {in_block=1; print; next}
  in_block && $0 ~ /^  [A-Za-z0-9_-]+:/ {exit}
  in_block {print}
' "${compose}")"
if ! grep -Fq 'ALARM_WORKER_EGRESS_LEASE_ENABLED: ${ALARM_WORKER_EGRESS_LEASE_ENABLED:-true}' <<< "${alarm_worker_block}"; then
  echo "[FAIL] alarm-worker must enable the notification egress lease by default" >&2
  fail=1
else
  echo "[PASS] alarm-worker egress lease is enabled by default"
fi
if ! grep -Fq 'DELIVERY_DISPATCHER_ENABLED: ${DELIVERY_DISPATCHER_ENABLED:-true}' <<< "${alarm_worker_block}"; then
  echo "[FAIL] alarm-worker must enable the notification delivery outbox dispatcher by default" >&2
  fail=1
else
  echo "[PASS] alarm-worker notification delivery outbox dispatcher is enabled by default"
fi

if [[ "${fail}" -ne 0 ]]; then
  exit 1
fi

echo "[PASS] notification egress ownership gate"
