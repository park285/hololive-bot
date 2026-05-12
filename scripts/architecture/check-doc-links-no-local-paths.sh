#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

echo "[CHECK] markdown docs do not contain local machine paths"

targets=(
  "${ROOT_DIR}/README.md"
  "${ROOT_DIR}/docs"
)
patterns=(
  "/root/work"
  "/mnt/data"
  "file://"
  "C:\\"
)

tmp_hits="$(mktemp)"
cleanup() {
  rm -f "${tmp_hits}"
}
trap cleanup EXIT

scan_pattern() {
  local pattern="$1"

  if command -v rg >/dev/null 2>&1; then
    rg -n --fixed-strings -g '*.md' -- "${pattern}" "${targets[@]}" >> "${tmp_hits}" || true
  else
    grep -R -n --include='*.md' -F -- "${pattern}" "${targets[@]}" >> "${tmp_hits}" || true
  fi
}

for pattern in "${patterns[@]}"; do
  scan_pattern "${pattern}"
done

if [[ -s "${tmp_hits}" ]]; then
  echo "[FAIL] local machine path found in markdown docs"
  cat "${tmp_hits}"
  exit 1
fi

echo "[PASS] markdown docs contain no local machine paths"
