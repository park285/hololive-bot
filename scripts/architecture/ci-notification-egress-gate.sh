#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

echo "[CHECK] notification egress ownership gate"

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

check_forbidden_global_go_hits \
  "Iris proactive sender implementation is alarm-worker internal only" \
  'NewIrisMessageSender|type .*IrisMessageSender' \
  -g '!hololive/hololive-alarm-worker/internal/egress/**' \
  -g '!hololive/hololive-alarm-worker/internal/app/**'

# dispatcher 접근은 Go internal 경계가 제한하며, 공유 delivery·Iris 심볼의 소유권은 여기서 검사한다.
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

# 배포는 prod와 live-compat을 병합해 쓰므로 병합 결과로 판정한다. environment·ports 표기는 짧은·긴 문법을 모두 받는다.
compose_json="$(cd "${ROOT_DIR}/deploy/compose" && docker compose -f docker-compose.prod.yml -f docker-compose.live-compat.yml \
  config --no-interpolate --no-env-resolution --format json)"

compose_check() {
  local pass_message="$1"
  local fail_message="$2"
  local filter="$3"

  if jq -e "
    def envmap: if type == \"array\" then map(capture(\"^(?<k>[^=]+)=?(?<v>.*)\$\") | {(.k): .v}) | add // {}
      elif type == \"object\" then . else {} end;
    def portspec: if type == \"string\" then .
      else \"\(.host_ip):\(.published):\(.target)\" + (if .protocol == \"udp\" then \"/udp\" else \"\" end) end;
    ${filter}
  " <<< "${compose_json}" >/dev/null; then
    echo "[PASS] ${pass_message}"
  else
    echo "[FAIL] ${fail_message}" >&2
    fail=1
  fi
}

compose_check "youtube-collector has no Iris egress env" \
  "youtube-collector has Iris egress env in the merged production compose" \
  '.services["youtube-collector"].environment | envmap | keys | any(test("^IRIS_(BOT_TOKEN|BASE_URL|TRANSPORT|H3_)")) | not'

compose_check "alarm-worker pins NOTIFICATION_SCHEDULER_ROLE to worker" \
  "alarm-worker must pin NOTIFICATION_SCHEDULER_ROLE: \"worker\" in the merged production compose; the runtime validator also accepts off, so this value is the deploy-layer guard that keeps the single alarm-worker instance from starting without the alarm checker/scheduler" \
  '(.services["hololive-alarm-worker"].environment | envmap | .NOTIFICATION_SCHEDULER_ROLE) == "worker"'

compose_check "alarm-worker pins container_name" \
  "alarm-worker must keep container_name: hololive-alarm-worker; the fixed container name is one half of the single-instance guarantee that now carries proactive egress exclusivity in place of the removed Valkey lease" \
  '.services["hololive-alarm-worker"].container_name == "hololive-alarm-worker"'

for port_binding in '127.0.0.1:30007:30007' '127.0.0.1:30007:30007/udp'; do
  compose_check "alarm-worker keeps fixed loopback host port binding ${port_binding}" \
    "alarm-worker must keep the fixed loopback host port binding ${port_binding}; the fixed host port is the other half of the single-instance guarantee, because a second instance cannot bind the same host port and would otherwise start silently" \
    "[.services[\"hololive-alarm-worker\"].ports[]? | portspec] | index(\"${port_binding}\") != null"
done

compose_check "alarm-worker declares no replicas" \
  "alarm-worker must not declare replicas or scale; proactive egress exclusivity relies on exactly one instance. ClaimDue is row-exclusive only, so a second instance can split one canonical (room, minute-bucket) group, produce a different ClientRequestID per fragment, and duplicate-send past Iris idempotency. Resolve the D-002 gate list in docs/current/architecture/alarm-egress-scale-out-decisions-20260730.md before scaling out" \
  '.services["hololive-alarm-worker"] | (.deploy.replicas // null) == null and (.scale // null) == null'

if [[ "${fail}" -ne 0 ]]; then
  exit 1
fi

echo "[PASS] notification egress ownership gate"
