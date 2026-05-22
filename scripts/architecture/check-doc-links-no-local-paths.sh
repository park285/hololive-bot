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

# host-migration 문서는 /root/work → /home/kapu/work 전환 절차 자체를 다루므로
# 해당 경로 출현이 본질적이다. 다른 docs 가 이 패턴을 그대로 베끼지 못하도록
# 명시 파일만 면제한다.
exempt_paths=(
  "docs/agent-workflows/plans/2026-05-21-host-migration-root-to-kapu.md"
  "docs/current/runbooks/host-migration-root-to-kapu.md"
)

tmp_hits="$(mktemp)"
cleanup() {
  rm -f "${tmp_hits}"
}
trap cleanup EXIT

is_exempt_hit() {
  local line="$1"
  local relpath="${line#${ROOT_DIR}/}"
  local file="${relpath%%:*}"
  local exempt
  for exempt in "${exempt_paths[@]}"; do
    if [[ "${file}" == "${exempt}" ]]; then
      return 0
    fi
  done
  return 1
}

scan_pattern() {
  local pattern="$1"
  local raw_hits
  raw_hits="$(mktemp)"

  if command -v rg >/dev/null 2>&1; then
    rg -n --fixed-strings -g '*.md' -- "${pattern}" "${targets[@]}" >> "${raw_hits}" || true
  else
    grep -R -n --include='*.md' -F -- "${pattern}" "${targets[@]}" >> "${raw_hits}" || true
  fi

  while IFS= read -r line; do
    [[ -z "${line}" ]] && continue
    if ! is_exempt_hit "${line}"; then
      printf '%s\n' "${line}" >> "${tmp_hits}"
    fi
  done < "${raw_hits}"

  rm -f "${raw_hits}"
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
