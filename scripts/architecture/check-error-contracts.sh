#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
if [[ -d "${ROOT_DIR}/shared-go" ]]; then SHARED_GO_DIR="${ROOT_DIR}/shared-go"; else SHARED_GO_DIR="${ROOT_DIR}/../shared-go"; fi
ERROR_DOC="${ROOT_DIR}/docs/current/ERROR_CONTRACT.md"

echo "[CHECK] error contract coverage"

required_files=(
  docs/current/ERROR_CONTRACT.md
  docs/current/contracts/membernews.md
  docs/current/contracts/trigger.md
  docs/current/contracts/alarm.md
  hololive/hololive-shared/pkg/server/internal/httpserver/response.go
  hololive/hololive-shared/pkg/contracts/common/errors.go
  shared-go/pkg/httputil/response.go
  hololive/hololive-shared/pkg/contracts/trigger/errors.go
)

required_tokens=(
  '{"error":"error_code_or_message"}'
  'notification_in_progress'
  'no_subscribed_members'
  'ErrNotificationInProgress'
  'ErrorResponse'
  'Client Interpretation Rules'
  'Alarm API envelope'
)

missing=0

for rel in "${required_files[@]}"; do
  if [[ ! -f "${ROOT_DIR}/${rel}" ]]; then
    echo "[FAIL] missing required file: ${rel}"
    missing=1
  else
    echo "[PASS] found: ${rel}"
  fi
done

if [[ ! -f "${ERROR_DOC}" ]]; then
  exit 1
fi

for token in "${required_tokens[@]}"; do
  if ! grep -Fq "${token}" "${ERROR_DOC}" \
    && ! grep -Fq "${token}" "${ROOT_DIR}/docs/current/contracts/membernews.md" \
    && ! grep -Fq "${token}" "${ROOT_DIR}/docs/current/contracts/trigger.md" \
    && ! grep -Fq "${token}" "${ROOT_DIR}/docs/current/contracts/alarm.md"; then
    echo "[FAIL] missing error contract token: ${token}"
    missing=1
  else
    echo "[PASS] error contract token covered: ${token}"
  fi
done

if ! grep -Fq 'RespondError' "${ROOT_DIR}/hololive/hololive-shared/pkg/server/internal/httpserver/response.go"; then
  echo "[FAIL] RespondError helper missing from shared server response"
  missing=1
fi
if ! grep -Fq 'CheckStatus' "${SHARED_GO_DIR}/pkg/httputil/response.go"; then
  echo "[FAIL] CheckStatus helper missing from shared httputil response"
  missing=1
fi
if ! grep -Fq 'type APIError struct' "${SHARED_GO_DIR}/pkg/httputil/response.go"; then
  echo "[FAIL] typed APIError missing from shared httputil response"
  missing=1
fi
if ! grep -Fq 'IsStatus' "${SHARED_GO_DIR}/pkg/httputil/response.go"; then
  echo "[FAIL] IsStatus helper missing from shared httputil response"
  missing=1
fi

status_parse_hits="$(
  rg -n 'strings\.Contains\(.*status [0-9]{3}|status 404|status 409' \
    "${ROOT_DIR}/hololive" "${SHARED_GO_DIR}" \
    -g '*.go' -g '!**/*_test.go' || true
)"
if [[ -n "${status_parse_hits}" ]]; then
  echo "[FAIL] production code must use httputil.APIError helpers instead of status substring parsing"
  echo "${status_parse_hits}"
  missing=1
fi

if [[ "${missing}" -ne 0 ]]; then
  exit 1
fi

echo "[PASS] error contract coverage is complete"
