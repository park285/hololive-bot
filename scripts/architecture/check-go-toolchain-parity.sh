#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

expected_module_version="1.27.1"
expected_selector_version="1.27"
module_files=(
  "go.mod"
  "admin-dashboard/backend/go.mod"
  "hololive/hololive-alarm-worker/go.mod"
  "hololive/hololive-api/go.mod"
  "hololive/hololive-dbtest/go.mod"
  "hololive/hololive-shared/go.mod"
  "hololive/hololive-youtube-collector/go.mod"
)

read_go_directive() {
  awk '$1 == "go" { print $2; exit }' "$1"
}

for relative_path in "${module_files[@]}"; do
  actual="$(read_go_directive "${ROOT_DIR}/${relative_path}")"
  if [[ "${actual}" != "${expected_module_version}" ]]; then
    echo "[FAIL] ${relative_path} go directive is ${actual:-missing}; expected ${expected_module_version}" >&2
    exit 1
  fi
  echo "[PASS] ${relative_path} uses Go ${expected_module_version}"
done

work_version="$(read_go_directive "${ROOT_DIR}/go.work")"
if [[ "${work_version}" != "${expected_module_version}" ]]; then
  echo "[FAIL] go.work directive is ${work_version:-missing}; expected ${expected_module_version}" >&2
  exit 1
fi
echo "[PASS] go.work uses Go ${expected_module_version}"

go_version_selector="$(tr -d '[:space:]' < "${ROOT_DIR}/.go-version")"
if [[ "${go_version_selector}" != "${expected_selector_version}" ]]; then
  echo "[FAIL] .go-version is ${go_version_selector:-missing}; expected ${expected_selector_version}" >&2
  exit 1
fi
echo "[PASS] .go-version selects Go ${expected_selector_version}"

tool_versions_selector="$(awk '$1 == "golang" { print $2; exit }' "${ROOT_DIR}/.tool-versions")"
if [[ "${tool_versions_selector}" != "${expected_selector_version}" ]]; then
  echo "[FAIL] .tool-versions golang selector is ${tool_versions_selector:-missing}; expected ${expected_selector_version}" >&2
  exit 1
fi
echo "[PASS] .tool-versions selects Go ${expected_selector_version}"

echo "[PASS] Go toolchain selectors and workspace directives are aligned"
