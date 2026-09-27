#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="${ROOT_DIR:-$(cd "${SCRIPT_DIR}/../.." && pwd)}"
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

# 퇴역 admin-dashboard BFF의 Go 소스는 없으므로 검사 대상에서 뺐다(stack-audit 2026-09-26 T19, DEC-20260926-hololive-retired-rollback-tooling).
targets=(go.mod hololive scripts internal)
while IFS= read -r root_go; do
  targets+=("${root_go}")
done < <(find . -maxdepth 1 -type f -name '*.go' -print)

for pattern in "${patterns[@]}"; do
  if rg -n "$pattern" --glob '*.go' --glob 'go.mod' "${targets[@]}"; then
    echo "ERROR: disallowed DB framework or auto-migration token detected in active Go/module surface: $pattern" >&2
    fail=1
  fi
done

exit "$fail"
