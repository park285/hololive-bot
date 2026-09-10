#!/usr/bin/env bash
set -euo pipefail
if [[ $# -ne 0 ]]; then
  echo 'usage: bash scripts/architecture/check-admin-contract.sh' >&2
  exit 2
fi
ADMIN_CONTRACT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ADMIN_CONTRACT_ROOT"
node scripts/architecture/generate-admin-docker-policy.mjs --check
go test -count=1 ./admin-dashboard/backend/internal/httpapi -run '^TestContract(RouteInventory|RoutingResponses|Generation)'
(cd admin-dashboard/frontend && corepack npm run test:contract)
echo 'PASS: registered routes, operation access denials, generated schema fixtures and generation reproducibility'
