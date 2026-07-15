#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.26.5+auto}"

run_policy_gates() {
  echo "[server-pr] workflow policy regression"
  bash scripts/ci/check-workflow-secrets_test.sh

  echo "[server-pr] shell syntax"
  while IFS= read -r script; do
    bash -n "${script}"
  done < <(find scripts -type f -name '*.sh' | sort)
}

run_go_workspace() {
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
}

run_frontend() {
  echo "[server-pr] admin frontend test/lint/build"
  (
    cd admin-dashboard/frontend
    npm ci --no-audit --no-fund
    npm run generate:api
    npm test
    npm run lint
    npm run build
  )
}

mode="${1:-all}"
case "${mode}" in
  policy)
    run_policy_gates
    ;;
  go)
    run_go_workspace
    ;;
  frontend)
    run_frontend
    ;;
  all)
    run_policy_gates
    run_go_workspace
    run_frontend
    ;;
  *)
    echo "usage: $0 [all|policy|go|frontend]" >&2
    exit 2
    ;;
esac
