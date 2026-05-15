#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

source "${SCRIPT_DIR}/go-workspace-modules.sh"

cd "${ROOT_DIR}"

for mod in "${GO_WORKSPACE_MODULES[@]}"; do
    if [[ -f "${mod}/go.mod" ]] && grep -q 'github.com/park285/iris-client-go =>' "${mod}/go.mod"; then
        (cd "${mod}" && go mod edit -dropreplace github.com/park285/iris-client-go)
    fi
done
