#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ASSET_LIST_FILE="${ROOT_DIR}/docs/architecture/release-governance-assets.txt"

if [[ ! -f "${ASSET_LIST_FILE}" ]]; then
  echo "[FAIL] release governance asset list not found: ${ASSET_LIST_FILE}"
  exit 1
fi

missing=0
while IFS= read -r asset_path; do
  if [[ -z "${asset_path}" || "${asset_path}" =~ ^[[:space:]]*# ]]; then
    continue
  fi

  IFS='|' read -r rel_path expected_version_marker required_tokens <<< "${asset_path}"
  file="${ROOT_DIR}/${rel_path}"
  if [[ ! -f "${file}" ]]; then
    echo "[FAIL] missing required governance file: ${rel_path}"
    missing=1
    continue
  fi

  echo "[PASS] found: ${rel_path}"

  if [[ -n "${expected_version_marker}" ]] && ! grep -Fq "${expected_version_marker}" "${file}"; then
    echo "[FAIL] ${rel_path}: missing version marker '${expected_version_marker}'"
    missing=1
  elif [[ -n "${expected_version_marker}" ]]; then
    echo "[PASS] ${rel_path}: version marker matched"
  fi

  if [[ -n "${required_tokens}" ]]; then
    IFS='|' read -r -a token_array <<< "${required_tokens}"
    for token in "${token_array[@]}"; do
      if [[ -z "${token}" ]]; then
        continue
      fi
      if ! grep -Fq "${token}" "${file}"; then
        echo "[FAIL] ${rel_path}: missing required token '${token}'"
        missing=1
      else
        echo "[PASS] ${rel_path}: token '${token}'"
      fi
    done
  fi
done < "${ASSET_LIST_FILE}"

if [[ "${missing}" -ne 0 ]]; then
  exit 1
fi

echo "[PASS] release governance assets are in place (source: ${ASSET_LIST_FILE})"
