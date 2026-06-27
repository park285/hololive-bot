#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
DOC="${ROOT_DIR}/docs/current/architecture/repository-ownership.md"
ALLOWLIST="${ROOT_DIR}/docs/current/architecture/repository-ownership.allowlist"

echo "[CHECK] repository ownership and runtime import boundaries"

missing=0

if [[ ! -f "${DOC}" ]]; then
  echo "[FAIL] missing repository ownership doc: docs/current/architecture/repository-ownership.md"
  exit 1
fi
if [[ ! -f "${ALLOWLIST}" ]]; then
  echo "[FAIL] missing repository ownership allowlist: docs/current/architecture/repository-ownership.allowlist"
  exit 1
fi

required_tokens=(
  major_event_subscriptions
  membernews.digest
  membernews.subscription
  majorevent.subscription
  alarm.dispatch
  YOUTUBE_INGESTION_ENABLED=true
)

for token in "${required_tokens[@]}"; do
  if ! grep -Fq "${token}" "${DOC}"; then
    echo "[FAIL] repository ownership doc missing: ${token}"
    missing=1
  else
    echo "[PASS] repository ownership doc contains: ${token}"
  fi
done

required_allowlist_entries=(
  major_event_subscriptions
  membernews_digest
  membernews_subscription
  alarm_dispatch
  alarm_state
  youtube_outbox
)

for entry in "${required_allowlist_entries[@]}"; do
  if ! grep -Eq "^${entry}\\|owner=[^|]+\\|writers=[^|]+\\|readers=[^|]+" "${ALLOWLIST}"; then
    echo "[FAIL] repository ownership allowlist missing or malformed: ${entry}"
    missing=1
  else
    echo "[PASS] repository ownership allowlist contains: ${entry}"
  fi
done

validate_module_refs() {
  local field="$1"
  local refs="$2"
  local ref

  IFS=',' read -ra ref_list <<< "${refs}"
  for ref in "${ref_list[@]}"; do
    if [[ ! -d "${ROOT_DIR}/hololive/${ref}" ]]; then
      echo "[FAIL] repository ownership allowlist ${field} path missing: hololive/${ref}"
      missing=1
    else
      echo "[PASS] repository ownership allowlist ${field} path exists: hololive/${ref}"
    fi
  done
}

while IFS='|' read -r data_area owner_field writers_field readers_field extra; do
  if [[ -z "${data_area}" || "${data_area}" == \#* ]]; then
    continue
  fi
  if [[ -n "${extra:-}" || "${owner_field}" != owner=* || "${writers_field}" != writers=* || "${readers_field}" != readers=* ]]; then
    echo "[FAIL] repository ownership allowlist malformed row: ${data_area}"
    missing=1
    continue
  fi
  validate_module_refs "writers" "${writers_field#writers=}"
  validate_module_refs "readers" "${readers_field#readers=}"
done < "${ALLOWLIST}"

check_no_imports() {
  local label="$1"
  local path="$2"
  local pattern="$3"
  local hits

  hits="$(rg -n "${pattern}" "${ROOT_DIR}/${path}" -g '*.go' || true)"
  if [[ -n "${hits}" ]]; then
    echo "[FAIL] forbidden imports in ${label}"
    echo "${hits}"
    missing=1
  else
    echo "[PASS] ${label} forbidden imports absent"
  fi
}

check_no_imports "bot runtime" \
  "hololive/hololive-api/internal/planes/bot" \
  'hololive-(alarm-worker|admin-api|llm-sched)/internal'

check_no_imports "shared-go module" \
  "shared-go" \
  'github.com/kapu/hololive-|github.com/park285/llm-kakao-bots/hololive'

check_no_imports "youtube-producer direct YouTube dispatch" \
  "hololive/hololive-youtube-producer" \
  'pkg/service/delivery|delivery\.NewIrisMessageSender|outbox\.NewDispatcher|OutboxDispatcher|YouTube outbox dispatcher started'

major_event_hits="$(
  rg -n 'majorevent.*repository|repository.*majorevent' \
    "${ROOT_DIR}/hololive/hololive-api/internal/planes/bot" \
    "${ROOT_DIR}/hololive/hololive-api/internal/planes/admin" \
    -g '*.go' || true
)"
if [[ -n "${major_event_hits}" ]]; then
  echo "[FAIL] bot/admin-api must not import major event repository/storage directly"
  echo "${major_event_hits}"
  missing=1
else
  echo "[PASS] bot/admin-api major event repository direct access absent"
fi

if [[ "${missing}" -ne 0 ]]; then
  exit 1
fi

echo "[PASS] repository ownership and runtime import boundaries are complete"
