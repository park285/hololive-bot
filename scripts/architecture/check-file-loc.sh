#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
THRESHOLD_FILE="${1:-${ROOT_DIR}/docs/architecture/file-loc-thresholds.txt}"

if [[ ! -f "${THRESHOLD_FILE}" ]]; then
  echo "error: threshold file not found: ${THRESHOLD_FILE}" >&2
  exit 1
fi

violations=()
reports=()

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
done < "${THRESHOLD_FILE}"

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
