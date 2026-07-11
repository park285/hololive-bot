#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=go-workspace-modules.sh
source "${ROOT_DIR}/scripts/ci/go-workspace-modules.sh"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

expected="$(cd "${ROOT_DIR}" && go work edit -json | python3 -c '
import json, sys
for use in json.load(sys.stdin)["Use"]:
    path = use["DiskPath"]
    normalized = path[2:] if path.startswith("./") else path
    if normalized not in {"", "."}:
        print(normalized)
')"
actual="$(printf '%s\n' "${GO_WORKSPACE_MODULES[@]}")"
[[ "${actual}" == "${expected}" ]]
echo "PASS: actual go.work is the module-list SSOT"

fixture_parent="${tmp_dir}/fixture"
fixture_root="${fixture_parent}/repo"
mkdir -p "${fixture_root}/internal-module" "${fixture_parent}/sibling-module"
printf 'module example.test/root\n\ngo 1.26.5\n' >"${fixture_root}/go.mod"
printf 'module example.test/internal\n\ngo 1.26.5\n' >"${fixture_root}/internal-module/go.mod"
printf 'module example.test/sibling\n\ngo 1.26.5\n' >"${fixture_parent}/sibling-module/go.mod"
(
    cd "${fixture_root}"
    go work init . ./internal-module ../sibling-module
)
fixture_modules="$(load_go_workspace_modules "${fixture_root}")"
[[ "${fixture_modules}" == $'../sibling-module\ninternal-module' ]]
echo "PASS: root exclusion and path normalization"

mkdir -p "${tmp_dir}/outside"
printf 'module example.test/outside\n\ngo 1.26.5\n' >"${tmp_dir}/outside/go.mod"
cat >>"${fixture_root}/go.work" <<EOF

use ../../outside
EOF
if load_go_workspace_modules "${fixture_root}" >"${tmp_dir}/out" 2>"${tmp_dir}/err"; then
    echo "FAIL: workspace boundary escape was accepted" >&2
    exit 1
fi
grep -Fq 'escapes workspace boundary' "${tmp_dir}/err"
echo "PASS: workspace boundary escape is rejected"
