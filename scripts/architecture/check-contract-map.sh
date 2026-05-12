#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
CONTRACT_MAP="${ROOT_DIR}/docs/current/CONTRACT_MAP.md"
CONTRACT_INDEX="${ROOT_DIR}/docs/current/contracts/README.md"

echo "[CHECK] contract map coverage"

required_contracts=(
  membernews
  majorevent
  trigger
  alarm
  settings
  iris-boundary
)

required_packages=(
  hololive/hololive-shared/pkg/contracts/membernews
  hololive/hololive-shared/pkg/contracts/majorevent
  hololive/hololive-shared/pkg/contracts/trigger
  hololive/hololive-shared/pkg/contracts/alarm
  hololive/hololive-shared/pkg/contracts/settings
)

missing=0

if [[ ! -f "${CONTRACT_MAP}" ]]; then
  echo "[FAIL] missing contract map: ${CONTRACT_MAP}"
  exit 1
fi
if [[ ! -f "${CONTRACT_INDEX}" ]]; then
  echo "[FAIL] missing contract index: ${CONTRACT_INDEX}"
  exit 1
fi

for contract in "${required_contracts[@]}"; do
  detail_file="${ROOT_DIR}/docs/current/contracts/${contract}.md"
  if [[ ! -f "${detail_file}" ]]; then
    echo "[FAIL] missing contract doc: docs/current/contracts/${contract}.md"
    missing=1
  else
    echo "[PASS] found contract doc: docs/current/contracts/${contract}.md"
  fi
  if ! grep -Fq "${contract}" "${CONTRACT_MAP}"; then
    echo "[FAIL] contract map missing contract token: ${contract}"
    missing=1
  else
    echo "[PASS] contract map contains: ${contract}"
  fi
  if ! grep -Fq "${contract}.md" "${CONTRACT_INDEX}"; then
    echo "[FAIL] contract index missing: ${contract}.md"
    missing=1
  else
    echo "[PASS] contract index links: ${contract}.md"
  fi
done

for package_path in "${required_packages[@]}"; do
  package_dir="${ROOT_DIR}/${package_path}"
  if [[ ! -d "${package_dir}" ]]; then
    echo "[FAIL] missing contract package directory: ${package_path}"
    missing=1
  else
    echo "[PASS] found contract package: ${package_path}"
  fi
  if ! grep -Fq "${package_path}" "${CONTRACT_MAP}"; then
    echo "[FAIL] contract map missing package path: ${package_path}"
    missing=1
  else
    echo "[PASS] contract map contains package path: ${package_path}"
  fi
done

if [[ "${missing}" -ne 0 ]]; then
  exit 1
fi

echo "[PASS] contract map coverage is complete"
