#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
THRESHOLD_FILE="${1:-${ROOT_DIR}/docs/architecture/file-loc-thresholds.txt}"
GO_THRESHOLD_FILE="${ROOT_DIR}/docs/architecture/go-module-loc-thresholds.txt"
DEFAULT_MAX_LINES="${DEFAULT_MAX_LINES:-400}"

if [[ ! -f "${THRESHOLD_FILE}" ]]; then
  echo "error: threshold file not found: ${THRESHOLD_FILE}" >&2
  exit 1
fi

declare -A configured=()
violations=()
reports=()

load_threshold_file() {
  local threshold_file="$1"

  while IFS= read -r line || [[ -n "${line}" ]]; do
    trimmed="$(echo "${line}" | sed 's/[[:space:]]*$//')"
    if [[ -z "${trimmed}" || "${trimmed}" =~ ^[[:space:]]*# ]]; then
      continue
    fi

    path="${trimmed%%:*}"
    max="${trimmed##*:}"
    path="$(echo "${path}" | xargs)"
    max="$(echo "${max}" | xargs)"

    if [[ -z "${path}" || -z "${max}" || ! "${max}" =~ ^[0-9]+$ ]]; then
      echo "error: invalid threshold line: ${line}" >&2
      exit 1
    fi

    if [[ -n "${configured[${path}]:-}" ]]; then
      violations+=("duplicate-threshold:${path}")
    fi
    configured["${path}"]="${max}"
  done < "${threshold_file}"
}

load_threshold_file "${THRESHOLD_FILE}"
if [[ -f "${GO_THRESHOLD_FILE}" ]]; then
  load_threshold_file "${GO_THRESHOLD_FILE}"
fi

for path in "${!configured[@]}"; do
  max="${configured[${path}]}"
  abs_path="${ROOT_DIR}/${path}"
  if [[ ! -f "${abs_path}" ]]; then
    violations+=("missing:${path}")
    continue
  fi

  count="$(wc -l < "${abs_path}" | tr -d '[:space:]')"
  reports+=("${path}:${count}/${max}")
  if (( count > max )); then
    violations+=("exceeded:${path}:${count}>${max}")
  fi
done

while IFS= read -r file; do
  rel="${file#${ROOT_DIR}/}"

  case "${rel}" in
    .tmp/*|\
    .worktrees/*|\
    artifacts/*|\
    logs/*|\
    */node_modules/*|\
    */dist/*|\
    */coverage/*|\
    */target/*|\
    */generated/*|\
    *.d.ts)
      continue
      ;;
  esac

  if [[ -n "${configured[${rel}]:-}" ]]; then
    continue
  fi

  count="$(wc -l < "${file}" | tr -d '[:space:]')"
  if (( count > DEFAULT_MAX_LINES )); then
    violations+=("missing-threshold:${rel}:${count}>${DEFAULT_MAX_LINES}")
  fi
done < <(
  find "${ROOT_DIR}" \
    \( -path "${ROOT_DIR}/.git" \
    -o -path "${ROOT_DIR}/.worktrees" \
    -o -path "${ROOT_DIR}/artifacts" \
    -o -path "${ROOT_DIR}/logs" \
    -o -path "${ROOT_DIR}/node_modules" \
    -o -path "${ROOT_DIR}/coverage" \
    -o -path "${ROOT_DIR}/dist" \
    -o -path "${ROOT_DIR}/target" \
    -o -path "${ROOT_DIR}/.tasklists" \
    -o -path "${ROOT_DIR}/.runlogs" \
    -o -path "${ROOT_DIR}/.codex" \
    -o -path "${ROOT_DIR}/.claude" \
    -o -path "${ROOT_DIR}/.serena" \
    -o -path "${ROOT_DIR}/.gemini" \
    -o -path "${ROOT_DIR}/.tmp" \) -prune -o \
    -type f \( -name '*.go' -o -name '*.rs' -o -name '*.ts' -o -name '*.tsx' -o -name '*.sh' \) \
    ! -name '*_test.go' \
    ! -name '*.test.ts' \
    ! -name '*.test.tsx' \
    ! -name '*.test.mjs' \
    -print
)

if (( ${#violations[@]} > 0 )); then
  echo "FAIL: file LOC threshold violations detected" >&2
  for item in "${violations[@]}"; do
    echo " - ${item}" >&2
  done
  echo
  echo "threshold file: ${THRESHOLD_FILE}" >&2
  exit 1
fi

echo "OK: file LOC thresholds are within limits (${#reports[@]} files)"
for item in "${reports[@]}"; do
  echo " - ${item}"
done
