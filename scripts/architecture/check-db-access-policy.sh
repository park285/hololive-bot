#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"

fail=0

echo "[db-access-policy] Checking active Go DB framework guardrails"

patterns=(
  'gorm\.io'
  'gorm\.DB'
  'gorm\.Open'
  'GetGormDB'
  'AutoMigrate\('
  'github\.com/uptrace/bun'
  'entgo\.io/ent'
  'github\.com/go-gorm'
)

for pattern in "${patterns[@]}"; do
  if rg -n "$pattern" --glob '*.go' --glob 'go.mod' go.mod hololive admin-dashboard scripts; then
    echo "ERROR: disallowed DB framework or auto-migration token detected in active Go/module surface: $pattern" >&2
    fail=1
  fi
done

exit "$fail"
