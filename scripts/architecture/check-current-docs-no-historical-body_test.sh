#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHECKER="${SCRIPT_DIR}/check-current-docs-no-historical-body.sh"
FIXTURE_ROOT="$(mktemp -d)"
cleanup() {
  rm -rf "${FIXTURE_ROOT}"
}
trap cleanup EXIT

mkdir -p "${FIXTURE_ROOT}/docs/current/handoffs" "${FIXTURE_ROOT}/scripts/ci"
CURRENT_DOC="${FIXTURE_ROOT}/docs/current/contract.md"

printf '# Current contract\n\nArchive: [previous design](../history/previous.md)\n' > "${CURRENT_DOC}"
CHECK_CURRENT_DOCS_ROOT="${FIXTURE_ROOT}" "${CHECKER}" >/dev/null

printf '# Current contract\n\n> **Superseded:** archived\n' > "${CURRENT_DOC}"
if CHECK_CURRENT_DOCS_ROOT="${FIXTURE_ROOT}" "${CHECKER}" >/dev/null 2>&1; then
  echo "[FAIL] Superseded body marker was accepted" >&2
  exit 1
fi

printf '# Current contract\n\n이 문서는 이력으로만 유지합니다.\n' > "${CURRENT_DOC}"
if CHECK_CURRENT_DOCS_ROOT="${FIXTURE_ROOT}" "${CHECKER}" >/dev/null 2>&1; then
  echo "[FAIL] Korean history-only body marker was accepted" >&2
  exit 1
fi

echo "[PASS] current-doc historical body fixtures"
