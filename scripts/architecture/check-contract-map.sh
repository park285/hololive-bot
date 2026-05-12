#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
CONTRACT_MAP="${ROOT_DIR}/docs/current/CONTRACT_MAP.md"
CONTRACT_INDEX="${ROOT_DIR}/docs/current/contracts/README.md"
CONTRACT_MANIFEST="${ROOT_DIR}/docs/current/CONTRACT_MANIFEST.txt"
PROJECT_MAP="${ROOT_DIR}/docs/current/PROJECT_MAP.md"

echo "[CHECK] contract map coverage"

required_contracts=(
  membernews
  majorevent
  trigger
  alarm
  settings
  iris-boundary
)

required_contract_ids=(
  membernews.digest
  membernews.subscription
  majorevent.subscription
  trigger.manual
  alarm.http
  alarm.dispatch
  settings.update
  iris.webhook
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
if [[ ! -f "${CONTRACT_MANIFEST}" ]]; then
  echo "[FAIL] missing contract manifest: ${CONTRACT_MANIFEST}"
  exit 1
fi
if [[ ! -f "${PROJECT_MAP}" ]]; then
  echo "[FAIL] missing project map: ${PROJECT_MAP}"
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

declare -A manifest_ids=()

is_external_runtime() {
  case "$1" in
    ""|external|iris|redroid|none)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

check_runtime_token() {
  local runtime="$1"
  local context="$2"

  if is_external_runtime "${runtime}"; then
    echo "[PASS] ${context} uses external runtime token: ${runtime}"
    return
  fi

  if ! grep -Fq "\`${runtime}\`" "${PROJECT_MAP}"; then
    echo "[FAIL] ${context} runtime is not listed in Project Map: ${runtime}"
    missing=1
  else
    echo "[PASS] ${context} runtime is listed in Project Map: ${runtime}"
  fi
}

while IFS='|' read -r contract_id provider consumers transport target package_path doc_path extra; do
  if [[ -z "${contract_id}" || "${contract_id}" == \#* ]]; then
    continue
  fi

  if [[ -n "${extra:-}" ]]; then
    echo "[FAIL] malformed manifest row for ${contract_id}: too many fields"
    missing=1
    continue
  fi
  if [[ -z "${provider}" || -z "${consumers}" || -z "${transport}" || -z "${target}" || -z "${package_path}" || -z "${doc_path}" ]]; then
    echo "[FAIL] malformed manifest row for ${contract_id}: empty required field"
    missing=1
    continue
  fi

  manifest_ids["${contract_id}"]=1
  doc_link="${doc_path#docs/current/}"

  if ! grep -Fq "${contract_id}" "${CONTRACT_MAP}"; then
    echo "[FAIL] contract map missing manifest ID: ${contract_id}"
    missing=1
  else
    echo "[PASS] contract map contains manifest ID: ${contract_id}"
  fi

  if ! grep -Fq "${doc_link}" "${CONTRACT_MAP}"; then
    echo "[FAIL] contract map missing manifest detail doc for ${contract_id}: ${doc_link}"
    missing=1
  else
    echo "[PASS] contract map links manifest detail doc for ${contract_id}: ${doc_link}"
  fi

  if [[ ! -f "${ROOT_DIR}/${doc_path}" ]]; then
    echo "[FAIL] manifest doc missing for ${contract_id}: ${doc_path}"
    missing=1
  else
    echo "[PASS] manifest doc exists for ${contract_id}: ${doc_path}"
  fi

  if [[ "${package_path}" != "external" ]]; then
    package_dir="${ROOT_DIR}/${package_path}"
    if [[ ! -d "${package_dir}" ]]; then
      echo "[FAIL] manifest package missing for ${contract_id}: ${package_path}"
      missing=1
    else
      echo "[PASS] manifest package exists for ${contract_id}: ${package_path}"
    fi
    if ! grep -Fq "${package_path}" "${CONTRACT_MAP}"; then
      echo "[FAIL] contract map missing manifest package for ${contract_id}: ${package_path}"
      missing=1
    else
      echo "[PASS] contract map contains manifest package for ${contract_id}: ${package_path}"
    fi
  else
    echo "[PASS] manifest package is external for ${contract_id}"
  fi

  check_runtime_token "${provider}" "${contract_id} provider"
  IFS=',' read -ra consumer_list <<< "${consumers}"
  for consumer in "${consumer_list[@]}"; do
    check_runtime_token "${consumer}" "${contract_id} consumer"
  done
done < "${CONTRACT_MANIFEST}"

for contract_id in "${required_contract_ids[@]}"; do
  if ! grep -Fq "${contract_id}" "${CONTRACT_MAP}"; then
    echo "[FAIL] contract map missing stable contract ID: ${contract_id}"
    missing=1
  else
    echo "[PASS] contract map contains stable ID: ${contract_id}"
  fi

  if [[ -z "${manifest_ids[${contract_id}]:-}" ]]; then
    echo "[FAIL] contract manifest missing stable contract ID: ${contract_id}"
    missing=1
  else
    echo "[PASS] contract manifest contains stable ID: ${contract_id}"
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
