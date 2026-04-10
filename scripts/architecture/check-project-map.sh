#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
WORK_FILE="${ROOT_DIR}/go.work"
MAP_FILE="${ROOT_DIR}/docs/current/PROJECT_MAP.md"

normalize_path() {
  local raw="${1}"
  raw="${raw#./}"
  raw="${raw%/}"
  printf '%s\n' "${raw}"
}

if [[ ! -f "${WORK_FILE}" ]]; then
  echo "[FAIL] go.work not found: ${WORK_FILE}"
  exit 1
fi

if [[ ! -f "${MAP_FILE}" ]]; then
  echo "[FAIL] current project map not found: ${MAP_FILE}"
  exit 1
fi

map_paths="$(
  awk -F'|' '/^\| `[^`]+` / { path=$4; gsub(/^[ \t]+|[ \t]+$/, "", path); gsub(/`/, "", path); print path }' "${MAP_FILE}" \
    | while IFS= read -r path; do
        normalize_path "${path}"
      done
)"
if [[ -z "${map_paths}" ]]; then
  echo "[FAIL] no module paths found in ${MAP_FILE}"
  exit 1
fi

missing=0
while IFS= read -r module; do
  [[ -z "${module}" ]] && continue
  normalized_module="$(normalize_path "${module}")"
  if [[ -z "${normalized_module}" || "${normalized_module}" == "." ]]; then
    continue
  fi
  if ! grep -Fxq "${normalized_module}" <<< "${map_paths}"; then
    echo "[FAIL] module missing from project map: ${module}"
    missing=1
  else
    echo "[PASS] project map contains module: ${module}"
  fi
done < <(awk '/^use \(/,/^\)/ { if ($1 ~ /^\.\//) print $1 }' "${WORK_FILE}")

for ref in AGENTS.md README.md docs/README.md; do
  file="${ROOT_DIR}/${ref}"
  expected_token="docs/current/PROJECT_MAP.md"
  if [[ "${ref}" == "docs/README.md" ]]; then
    expected_token="current/PROJECT_MAP.md"
  fi

  if [[ ! -f "${file}" ]]; then
    echo "[SKIP] ${ref} not present in repository root"
    continue
  fi

  if ! grep -Fq "${expected_token}" "${file}"; then
    echo "[FAIL] ${ref} does not reference ${expected_token}"
    missing=1
  else
    echo "[PASS] ${ref} references ${expected_token}"
  fi
done

if [[ "${missing}" -ne 0 ]]; then
  exit 1
fi

echo "[PASS] project map governance is consistent"
