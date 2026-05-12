#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

echo "[CHECK] <gate-name>"

# 1. Locate required files.
# 2. Run validation.
# 3. Print clear PASS/FAIL lines.
# 4. Exit 1 on failure.

echo "[PASS] <gate-name>"
