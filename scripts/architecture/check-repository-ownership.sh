#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
DOC="${ROOT_DIR}/docs/current/architecture/repository-ownership.md"

echo "[CHECK] repository ownership and runtime import boundaries"

missing=0

if [[ ! -f "${DOC}" ]]; then
  echo "[FAIL] missing repository ownership doc: docs/current/architecture/repository-ownership.md"
  exit 1
fi

required_tokens=(
  major_event_subscriptions
  membernews.digest
  membernews.subscription
  majorevent.subscription
  alarm.dispatch
  YOUTUBE_INGESTION_ENABLED=false
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
  "hololive/hololive-kakao-bot-go" \
  'hololive-(alarm-worker|admin-api|llm-sched)/internal'

check_no_imports "dispatcher-go runtime" \
  "hololive/hololive-dispatcher-go" \
  'hololive-(llm-sched|admin-api|kakao-bot-go)/internal'

check_no_imports "shared-go module" \
  "shared-go" \
  'github.com/kapu/hololive-|github.com/park285/llm-kakao-bots/hololive'

major_event_hits="$(
  rg -n 'majorevent.*repository|repository.*majorevent' \
    "${ROOT_DIR}/hololive/hololive-kakao-bot-go" \
    "${ROOT_DIR}/hololive/hololive-admin-api" \
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
