#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "[M1] Go alarm contract sanity check"
"${SCRIPT_DIR}/check-go-rust-alarm-contracts.sh"
echo

echo "[M1] Go trigger route hardcoding check"
"${SCRIPT_DIR}/check-go-trigger-route-hardcoding.sh"
