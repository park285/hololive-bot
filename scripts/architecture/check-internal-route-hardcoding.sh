#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

echo "[CHECK] internal route hardcoding"

tmp_hits="$(mktemp)"
cleanup() {
  rm -f "${tmp_hits}"
}
trap cleanup EXIT

check_route() {
  local label="$1"
  local route="$2"
  local allowed_regex="$3"

  grep -R -n --include='*.go' "\"${route}" "${ROOT_DIR}/hololive" "${ROOT_DIR}/shared-go" 2>/dev/null \
    | grep -Ev "${allowed_regex}" \
    >> "${tmp_hits}" || true

  if grep -q "${route}" "${tmp_hits}"; then
    echo "[FAIL] hardcoded ${label} route outside allowed files"
    return 1
  fi
  echo "[PASS] ${label} route hardcoding is constrained"
  return 0
}

missing=0

check_route "membernews" "/internal/membernews" \
  'hololive-shared/pkg/contracts/membernews/routes.go|_test\.go' || missing=1

check_route "majorevent" "/internal/majorevent" \
  'hololive-shared/pkg/contracts/majorevent/routes.go|_test\.go' || missing=1

check_route "trigger" "/internal/trigger" \
  'hololive-shared/pkg/contracts/trigger/routes.go|_test\.go' || missing=1

check_route "alarm" "/internal/alarm" \
  'hololive-shared/pkg/service/alarm/(api|client|api_test|client_test|client_additional_test)\.go|hololive-admin-api/internal/server/.*_test\.go|hololive-kakao-bot-go/internal/app/.*_test\.go|hololive-kakao-bot-go/internal/command/.*_test\.go' || missing=1

if [[ -s "${tmp_hits}" ]]; then
  cat "${tmp_hits}"
fi

if [[ "${missing}" -ne 0 ]]; then
  exit 1
fi

echo "[PASS] internal route hardcoding gate passed"
