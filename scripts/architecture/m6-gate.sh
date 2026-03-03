#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "[M6] Deprecated removal deadline gate"
"${SCRIPT_DIR}/check-rust-deprecated-deadline.sh"

echo "[M6] Release governance assets gate"
"${SCRIPT_DIR}/check-release-governance-assets.sh"
