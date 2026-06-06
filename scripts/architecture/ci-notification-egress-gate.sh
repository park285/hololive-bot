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
  "youtube-producer does not own YouTube outbox dispatch or Iris egress capability" \
  'pkg/service/delivery|delivery\.NewIrisMessageSender|outbox\.NewDispatcher|OutboxDispatcher|YouTube outbox dispatcher started|ProvideIrisClient|iris\.WithBaseURL|iris\.WithBotToken|IrisClient:' \
  "hololive/hololive-youtube-producer"

check_forbidden_scoped_go_hits \
  "llm-sched does not start proactive delivery dispatch or Iris delivery" \
  'DeliveryDispatcher|Delivery outbox dispatcher started|NewIrisMessageSender|ProvideIrisClient|iris\.WithBaseURL|iris\.WithBotToken' \
  "hololive/hololive-llm-sched"

check_forbidden_scoped_go_hits \
  "admin-api does not start proactive delivery dispatch or Iris delivery" \
  'DeliveryDispatcher|NewIrisMessageSender|outbox\.NewDispatcher|ProvideIrisClient|iris\.WithBaseURL|iris\.WithBotToken' \
  "hololive/hololive-admin-api"

compose="${ROOT_DIR}/deploy/compose/docker-compose.prod.yml"
for service in hololive-admin-api youtube-producer llm-scheduler; do
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

admin_api_block="$(awk '
  $0 == "  hololive-admin-api:" {in_block=1; print; next}
  in_block && $0 ~ /^  [A-Za-z0-9_-]+:/ {exit}
  in_block {print}
' "${compose}")"
if grep -Eq '^[[:space:]]+env_file:' <<< "${admin_api_block}"; then
  echo "[FAIL] hololive-admin-api must not import a service-level env_file" >&2
  fail=1
else
  echo "[PASS] hololive-admin-api does not import a service-level env_file"
fi
if grep -Eq 'ADMIN_PASS_BCRYPT|ADMIN_PASS_HASH|ADMIN_SECRET_KEY|SESSION_SECRET' <<< "${admin_api_block}"; then
  echo "[FAIL] hololive-admin-api must not receive dashboard-only secrets" >&2
  fail=1
else
  echo "[PASS] hololive-admin-api has no dashboard-only secrets"
fi

dispatcher_hits="$(rg -n 'dispatcher-go|hololive-dispatcher-go|legacy-dispatcher-go' "${compose}" || true)"
if [[ -n "${dispatcher_hits}" ]]; then
  echo "[FAIL] standalone dispatcher-go must be absent from docker-compose.prod.yml" >&2
  echo "${dispatcher_hits}" >&2
  fail=1
else
  echo "[PASS] standalone dispatcher-go is absent from docker-compose.prod.yml"
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
