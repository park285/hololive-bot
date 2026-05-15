#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

matches="$(
  rg -n \
    "(settlement-go|CommandSettlementStatus|CommandSettlementPaid|CommandSettlementRegister|settlement_status|settlement_paid|settlement_register|SETTLEMENT_ROOM_ID|dispatcher-go|hololive-dispatcher-go|legacy-dispatcher-go|smoke-dispatcher|DISPATCHER_PORT|30020)" \
    "${ROOT_DIR}" \
    --glob '!docs/history/settlement/**' \
    --glob '!docs/history/**' \
    --glob '!docs/superpowers/**' \
    --glob '!hololive/hololive-kakao-bot-go/scripts/migrations/archive/settlement/**' \
    --glob '!hololive/hololive-kakao-bot-go/scripts/migrations/manual/settlement_drop.sql' \
    --glob '!scripts/architecture/check-removed-runtime-references.sh' \
    --glob '!scripts/architecture/ci-notification-egress-gate.sh' \
    --glob '!scripts/deploy/test-compose-services.sh' \
    --glob '!scripts/deploy/lib/removed-runtimes.sh' \
    --glob '!scripts/deploy/test-removed-runtimes.sh' \
    --glob '!docs/current/architecture/repo-refactor-audit.md' \
    --glob '!hololive/hololive-shared/pkg/config/repo_security_contract_test.go' \
    --glob '!.tasklists/**' \
    --glob '!repo_full_scope_diff_blueprint*.md' \
    --glob '!.worktrees/**' \
    --glob '!**/*.tar.gz' \
    --glob '!**/node_modules/**' \
    || true
)"

if [[ -n "${matches}" ]]; then
  echo "FAIL: removed runtime references detected" >&2
  echo "${matches}" >&2
  exit 1
fi

echo "OK: no removed runtime references detected"
