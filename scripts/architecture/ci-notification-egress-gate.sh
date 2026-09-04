#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

echo "[CHECK] notification egress ownership gate"

"${SCRIPT_DIR}/check-youtube-egress-lifecycle-ownership_test.sh"
"${SCRIPT_DIR}/check-youtube-egress-lifecycle-ownership.sh"

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

check_required_scoped_go_hit() {
  local label="$1"
  local pattern="$2"
  local path="$3"

  if rg -q "${pattern}" "${ROOT_DIR}/${path}"; then
    echo "[PASS] ${label}"
  else
    echo "[FAIL] ${label}" >&2
    fail=1
  fi
}

check_forbidden_global_go_hits \
  "Iris proactive sender implementation is alarm-worker internal only" \
  'NewIrisMessageSender|type .*IrisMessageSender' \
  -g '!hololive/hololive-alarm-worker/internal/egress/**' \
  -g '!hololive/hololive-alarm-worker/internal/app/**'

check_forbidden_global_go_hits \
  "retired Karing opt-in env is referenced only by the alarm-worker guard" \
  'YOUTUBE_OUTBOX_KARING_ENABLED|ALARM_DISPATCH_KARING_ENABLED' \
  -g '!hololive/hololive-shared/pkg/config/settings/alarmworker/retired_notification_egress.go'

check_required_scoped_go_hit \
  "alarm dispatch gates Karing on confirmed regular chat" \
  'rooms\.RegularChat\(ctx, roomID\)' \
  "hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_group.go"

check_required_scoped_go_hit \
  "YouTube outbox gates Karing on confirmed regular chat" \
  'sender\.RegularChat\(ctx, roomID\)' \
  "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/send_engine_karing.go"

# Dispatcher symbols are compiler-protected by alarm-worker/internal; shared delivery/Iris symbols require this scoped textual gate.
check_forbidden_scoped_go_hits \
  "youtube-collector does not own YouTube outbox dispatch or Iris egress capability" \
  'pkg/service/delivery|delivery\.NewIrisMessageSender|outbox\.NewDispatcher|OutboxDispatcher|YouTube outbox dispatcher started|ProvideIrisClient|iris\.WithBaseURL|iris\.WithBotToken|IrisClient:' \
  "hololive/hololive-youtube-collector"

check_forbidden_scoped_go_hits \
  "hololive-api llm plane does not start proactive delivery dispatch or Iris delivery" \
  'DeliveryDispatcher|Delivery outbox dispatcher started|NewIrisMessageSender|ProvideIrisClient|iris\.WithBaseURL|iris\.WithBotToken' \
  "hololive/hololive-api/internal/planes/llm"

check_forbidden_scoped_go_hits \
  "hololive-api admin plane does not start proactive delivery dispatch or Iris delivery" \
  'DeliveryDispatcher|NewIrisMessageSender|outbox\.NewDispatcher|ProvideIrisClient|iris\.WithBaseURL|iris\.WithBotToken' \
  "hololive/hololive-api/internal/planes/admin"

compose="${ROOT_DIR}/deploy/compose/docker-compose.prod.yml"
service="youtube-collector"
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

alarm_worker_block="$(awk '
  $0 == "  hololive-alarm-worker:" {in_block=1; print; next}
  in_block && $0 ~ /^  [A-Za-z0-9_-]+:/ {exit}
  in_block {print}
' "${compose}")"
if ! grep -Fq 'NOTIFICATION_SCHEDULER_ROLE: "worker"' <<< "${alarm_worker_block}"; then
  echo "[FAIL] alarm-worker must pin NOTIFICATION_SCHEDULER_ROLE: \"worker\" in docker-compose.prod.yml; the runtime validator also accepts off, so this literal is the deploy-layer guard that keeps the single alarm-worker instance from starting without the alarm checker/scheduler" >&2
  fail=1
else
  echo "[PASS] alarm-worker pins NOTIFICATION_SCHEDULER_ROLE to worker"
fi
if ! grep -Fq 'container_name: hololive-alarm-worker' <<< "${alarm_worker_block}"; then
  echo "[FAIL] alarm-worker must keep container_name: hololive-alarm-worker; the fixed container name is one half of the single-instance guarantee that now carries proactive egress exclusivity in place of the removed Valkey lease" >&2
  fail=1
else
  echo "[PASS] alarm-worker pins container_name"
fi
for port_binding in '"127.0.0.1:30007:30007"' '"127.0.0.1:30007:30007/udp"'; do
  if ! grep -Fq "${port_binding}" <<< "${alarm_worker_block}"; then
    echo "[FAIL] alarm-worker must keep the fixed loopback host port binding ${port_binding}; the fixed host port is the other half of the single-instance guarantee, because a second instance cannot bind the same host port and would otherwise start silently" >&2
    fail=1
  else
    echo "[PASS] alarm-worker keeps fixed loopback host port binding ${port_binding}"
  fi
done
if grep -Eq '^[[:space:]]*replicas:' <<< "${alarm_worker_block}"; then
  echo "[FAIL] alarm-worker must not declare replicas; proactive egress exclusivity relies on exactly one instance. ClaimDue is row-exclusive only, so a second instance can split one canonical (room, minute-bucket) group, produce a different ClientRequestID per fragment, and duplicate-send past Iris idempotency. Resolve the D-002 gate list in docs/current/architecture/alarm-egress-scale-out-decisions-20260730.md before scaling out" >&2
  grep -En '^[[:space:]]*replicas:' <<< "${alarm_worker_block}" >&2
  fail=1
else
  echo "[PASS] alarm-worker declares no replicas"
fi
if ! grep -Fq 'STACK_WORKER_PROFILE_FILE: /run/hololive-bot/worker-profiles/alarm-worker.json' <<< "${alarm_worker_block}"; then
  echo "[FAIL] alarm-worker must use its strict local Stack Worker Profile v1" >&2
  fail=1
else
  echo "[PASS] alarm-worker uses its strict local Stack Worker Profile v1"
fi
for retired in \
  DELIVERY_DISPATCHER_ENABLED \
  ALARM_DISPATCH_CONSUMER_ENABLED \
  YOUTUBE_OUTBOX_DISPATCHER_ENABLED \
  YOUTUBE_OUTBOX_KARING_ENABLED \
  ALARM_DISPATCH_KARING_ENABLED; do
  if grep -Fq "${retired}:" <<< "${alarm_worker_block}"; then
    echo "[FAIL] alarm-worker still declares retired worker enablement ${retired}" >&2
    fail=1
  fi
done

if [[ "${fail}" -ne 0 ]]; then
  exit 1
fi

echo "[PASS] notification egress ownership gate"
