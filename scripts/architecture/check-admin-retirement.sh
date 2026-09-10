#!/usr/bin/env bash
set -euo pipefail
ADMIN_RETIREMENT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ADMIN_RETIREMENT_ROOT"
node scripts/architecture/check-admin-retirement.mjs "$@"
cd admin-dashboard/backend
go test -count=1 ./internal/httpapi -run '^TestContractRouteInventory$'
go test -count=1 ./internal/session -run '^(TestIncompleteStoredSessionCannotAuthenticateOrMutate|TestLegacyTokenCannotAcquireFamilyAuthority|TestFamilyEvictionCannotRecreateMutationAuthority)$'
echo 'PASS: registered routes and missing-field/family-eviction retirement regressions'
