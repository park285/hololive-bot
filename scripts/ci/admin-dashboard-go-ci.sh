#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"
source "${ROOT_DIR}/scripts/ci/go-tooling.sh"

RUN_RACE_TESTS="${RUN_RACE_TESTS:-true}"
RUN_NILAWAY="${RUN_NILAWAY:-true}"
RUN_DEPENDENCY_HYGIENE="${RUN_DEPENDENCY_HYGIENE:-true}"

run_step() {
  local name="$1"
  shift
  echo "[ADMIN DASHBOARD GO CI] ${name}"
  "$@"
  echo
}

check_go_toolchain() {
  actual="$(go env GOVERSION)"
  case "${actual}" in
    go1.26.*) ;;
    *) echo "expected go1.26.x toolchain, got ${actual}" >&2; exit 1 ;;
  esac
}

run_step "Go toolchain" check_go_toolchain
run_step "Go-only architecture gate" ./scripts/architecture/check-admin-dashboard-go-only.sh
run_step "go work sync" go work sync
run_step "go mod tidy diff" bash -c 'cd admin-dashboard/backend && go mod tidy -diff'
run_step "gofmt" bash -c 'unformatted="$(find admin-dashboard/backend -name "*.go" -not -path "*/vendor/*" -print0 | xargs -0 -r gofmt -l)"; if [[ -n "${unformatted}" ]]; then echo "gofmt required for:" >&2; echo "${unformatted}" >&2; exit 1; fi'
run_step "go vet" bash -c 'cd admin-dashboard/backend && GOFLAGS=-mod=readonly go vet ./...'
run_step "staticcheck" bash -c "cd admin-dashboard/backend && GOFLAGS=-mod=readonly '$(ensure_staticcheck)' ./..."
run_step "golangci-lint" bash -c "cd admin-dashboard/backend && '$(ensure_golangci_lint)' run -c ../../.golangci.yml ./..."
if [[ "${RUN_NILAWAY}" == "true" ]]; then
  run_step "NilAway" bash -c "cd admin-dashboard/backend && GOMEMLIMIT=${NILAWAY_GOMEMLIMIT:-10GiB} GOFLAGS=-mod=readonly '$(ensure_nilaway)' -pretty-print ./..."
else
  echo "[ADMIN DASHBOARD GO CI] Skip NilAway: RUN_NILAWAY=${RUN_NILAWAY}"
  echo
fi
run_step "go build" bash -c 'cd admin-dashboard/backend && GOFLAGS=-mod=readonly go build -trimpath -buildvcs=false ./cmd/admin-dashboard ./cmd/healthcheck'
run_step "go test" bash -c 'cd admin-dashboard/backend && GOFLAGS=-mod=readonly go test -count=1 ./...'

if [[ "${RUN_RACE_TESTS}" == "true" ]]; then
  run_step "go race test" bash -c 'cd admin-dashboard/backend && GOFLAGS=-mod=readonly go test -race -count=1 ./...'
fi

if [[ "${RUN_DEPENDENCY_HYGIENE}" == "true" ]]; then
  run_step "govulncheck" bash -c "cd admin-dashboard/backend && GOFLAGS=-mod=readonly '$(ensure_govulncheck)' ./..."
fi

echo "[ADMIN DASHBOARD GO CI] Passed"
