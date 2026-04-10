#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

matches="$(
  rg -n \
    "(settlement-go|CommandSettlementStatus|CommandSettlementPaid|CommandSettlementRegister|settlement_status|settlement_paid|settlement_register|SETTLEMENT_ROOM_ID)" \
    "${ROOT_DIR}" \
    --glob '!docs/history/settlement/**' \
    --glob '!hololive/hololive-kakao-bot-go/scripts/migrations/archive/settlement/**' \
    --glob '!hololive/hololive-kakao-bot-go/scripts/migrations/manual/settlement_drop.sql' \
    --glob '!scripts/architecture/check-removed-runtime-references.sh' \
    --glob '!.tasklists/**' \
    --glob '!repo_full_scope_diff_blueprint*.md' \
    --glob '!.worktrees/**' \
    --glob '!**/*.tar.gz' \
    --glob '!**/node_modules/**' \
    || true
)"

if [[ -n "${matches}" ]]; then
  echo "FAIL: removed settlement runtime references detected" >&2
  echo "${matches}" >&2
  exit 1
fi

echo "OK: no removed settlement runtime references detected"
