#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
source "${ROOT_DIR}/scripts/ci/go-workspace-modules.sh"

mapfile -t packages < <(go_workspace_non_admin_package_patterns)
go test "${packages[@]}"
