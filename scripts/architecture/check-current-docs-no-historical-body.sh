#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="${CHECK_CURRENT_DOCS_ROOT:-$(cd "${SCRIPT_DIR}/../.." && pwd)}"
CURRENT_DIR="${ROOT_DIR}/docs/current"

echo "[CHECK] current docs do not contain historical status bodies"

if [[ ! -d "${CURRENT_DIR}" ]]; then
  echo "[FAIL] current docs directory not found: ${CURRENT_DIR}"
  exit 1
fi

tmp_hits="$(mktemp)"
cleanup() {
  rm -f "${tmp_hits}"
}
trap cleanup EXIT

grep -R -n --include='*.md' \
	-E 'CLOSED / HISTORICAL|Historical document\. Do not use as the current source of truth|Historical [0-9]{4}-[0-9]{2}-[0-9]{2} snapshot|Historical snapshot of|> \*\*Superseded:\*\*|중간 구현 기록|이력으로만 유지' \
	"${CURRENT_DIR}" > "${tmp_hits}" || true

if [[ -s "${tmp_hits}" ]]; then
  echo "[FAIL] historical status body marker found under docs/current"
  cat "${tmp_hits}"
  exit 1
fi

if find "${CURRENT_DIR}/handoffs" -type f -print -quit 2>/dev/null | grep -q .; then
  echo "[FAIL] completed handoff body found under docs/current/handoffs" >&2
  exit 1
fi

if find "${ROOT_DIR}/scripts/ci/hardening-evidence" -type f -print -quit 2>/dev/null | grep -q .; then
  echo "[FAIL] historical hardening evidence must live under docs/history" >&2
  exit 1
fi

echo "[PASS] docs/current contains no historical status body markers"
