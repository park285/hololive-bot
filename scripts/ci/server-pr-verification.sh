#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.26.5+auto}"

echo "[server-pr] workflow policy regression"
bash scripts/ci/check-workflow-secrets_test.sh

echo "[server-pr] architecture boundary gate"
bash scripts/architecture/ci-boundary-gate.sh

echo "[server-pr] shell syntax"
while IFS= read -r script; do
  bash -n "${script}"
done < <(find scripts -type f -name '*.sh' | sort)

echo "[server-pr] full Go workspace tests and vet"
source scripts/ci/go-workspace-modules.sh
mapfile -t packages < <(
  {
    printf './...\n'
    go_workspace_package_patterns | sed 's#^\./\.\./#../#'
  } | awk 'NF'
)
printf 'workspace packages:\n%s\n' "${packages[*]}"
go test -count=1 "${packages[@]}"
go vet "${packages[@]}"

echo "[server-pr] admin frontend test/lint/build"
(
  cd admin-dashboard/frontend
  npm ci --no-audit --no-fund
  npm run generate:api
  npm test
  npm run lint
  npm run build
)
