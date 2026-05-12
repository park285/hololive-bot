#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
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
  -E 'CLOSED / HISTORICAL|Historical document\. Do not use as the current source of truth' \
  "${CURRENT_DIR}" > "${tmp_hits}" || true

if [[ -s "${tmp_hits}" ]]; then
  echo "[FAIL] historical status body marker found under docs/current"
  cat "${tmp_hits}"
  exit 1
fi

echo "[PASS] docs/current contains no historical status body markers"
