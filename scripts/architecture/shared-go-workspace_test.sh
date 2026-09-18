#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
for script in check-shared-go-boundary check-shared-go-packages export-go-workspace-import-graph \
    check-error-contracts check-go-trigger-route-hardcoding check-go-generic-internal-package-names check-internal-route-hardcoding; do
    if output="$(SHARED_GO_WORKSPACE_PATH="$tmp/missing" bash "$ROOT_DIR/scripts/architecture/$script.sh" 2>&1)"; then
        echo "[FAIL] $script accepted missing explicit workspace" >&2; exit 1
    fi
    [[ "$output" == *'shared-go'*'not found'* ]] || { echo "[FAIL] $script lacks workspace diagnostic" >&2; exit 1; }
done
echo '[PASS] architecture checks reject missing canonical workspace'
