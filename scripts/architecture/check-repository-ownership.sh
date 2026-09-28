#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
echo "[CHECK] runtime import boundaries"

missing=0

check_no_imports() {
  local label="$1"
  local path="$2"
  local pattern="$3"
  local hits

  if [[ ! -d "${ROOT_DIR}/${path}" ]]; then
    echo "[FAIL] ${label} path missing: ${path}"
    missing=1
    return
  fi

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
  "../shared-go" \
  'github.com/kapu/hololive-|github.com/park285/llm-kakao-bots/hololive'

# Dispatcher symbols are compiler-protected by alarm-worker/internal; shared delivery symbols still need this ownership gate.
check_no_imports "youtube-collector direct YouTube dispatch" \
  "hololive/hololive-youtube-collector" \
  'pkg/service/delivery|delivery\.NewIrisMessageSender|outbox\.NewDispatcher|OutboxDispatcher|YouTube outbox dispatcher started'

check_no_imports "youtube-collector write-capable alarm repository" \
  "hololive/hololive-youtube-collector" \
  'hololive-shared/pkg/service/alarm"|alarm\.NewRepository'

collector_adapter_dirs=(
  holodexcollector
  officialcollector
  youtubejscollector
  collectorruntime
)
for dir in "${collector_adapter_dirs[@]}"; do
  path="${ROOT_DIR}/hololive/hololive-youtube-collector/internal/runtime/${dir}"
  if [[ ! -d "${path}" ]]; then
    echo "[FAIL] collector adapter path missing: ${dir}"
    missing=1
    continue
  fi
  collector_persist_hits="$(
    rg -n 'poller/runtime/batchrepo|internal/runtime/pollers|PersistCommunityPosts' \
      "${path}" \
      -g '*.go' -g '!*_test.go' || true
  )"
  if [[ -n "${collector_persist_hits}" ]]; then
    echo "[FAIL] ${dir} must not import canonical persist helpers"
    echo "${collector_persist_hits}"
    missing=1
  else
    echo "[PASS] ${dir} persist helpers absent"
  fi
done

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
