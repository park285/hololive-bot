#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

STATICCHECK_VERSION="${STATICCHECK_VERSION:-2026.1}"
GOVULNCHECK_VERSION="${GOVULNCHECK_VERSION:-v1.3.0}"
RUN_RACE_TESTS="${RUN_RACE_TESTS:-false}"
RUN_DEPENDENCY_HYGIENE="${RUN_DEPENDENCY_HYGIENE:-true}"

run_step() {
  local name="$1"
  shift
  echo "[ADMIN DASHBOARD GO CI] ${name}"
  "$@"
  echo
}

go_bin_tool() {
  local tool="$1"
  if command -v "${tool}" >/dev/null 2>&1; then
    command -v "${tool}"
    return 0
  fi
  local gobin
  gobin="$(go env GOBIN)"
  if [[ -n "${gobin}" && -x "${gobin}/${tool}" ]]; then
    printf '%s/%s\n' "${gobin}" "${tool}"
    return 0
  fi
  local gopath
  gopath="$(go env GOPATH)"
  if [[ -n "${gopath}" && -x "${gopath}/bin/${tool}" ]]; then
    printf '%s/bin/%s\n' "${gopath}" "${tool}"
    return 0
  fi
  return 1
}

go_tool_install_path() {
  local tool="$1"
  local gobin
  gobin="$(go env GOBIN)"
  if [[ -n "${gobin}" ]]; then
    printf '%s/%s\n' "${gobin}" "${tool}"
    return 0
  fi
  printf '%s/bin/%s\n' "$(go env GOPATH)" "${tool}"
}

ensure_staticcheck() {
  local bin
  bin="$(go_bin_tool staticcheck || true)"
  if [[ -z "${bin}" ]] || [[ "$("${bin}" -version 2>/dev/null || true)" != *"staticcheck ${STATICCHECK_VERSION}"* ]]; then
    go install "honnef.co/go/tools/cmd/staticcheck@${STATICCHECK_VERSION}"
    bin="$(go_tool_install_path staticcheck)"
  fi
  printf '%s\n' "${bin}"
}

ensure_govulncheck() {
  local bin
  bin="$(go_bin_tool govulncheck || true)"
  if [[ -z "${bin}" ]] || [[ "$("${bin}" -version 2>/dev/null || true)" != *"govulncheck@${GOVULNCHECK_VERSION}"* ]]; then
    go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"
    bin="$(go_tool_install_path govulncheck)"
  fi
  printf '%s\n' "${bin}"
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
run_step "gofmt" bash -c 'test -z "$(gofmt -l $(find admin-dashboard/backend -name "*.go" -not -path "*/vendor/*"))"'
run_step "go vet" bash -c 'cd admin-dashboard/backend && GOFLAGS=-mod=readonly go vet ./...'
run_step "staticcheck" bash -c "cd admin-dashboard/backend && GOFLAGS=-mod=readonly '$(ensure_staticcheck)' ./..."
run_step "go build" bash -c 'cd admin-dashboard/backend && GOFLAGS=-mod=readonly go build -trimpath -buildvcs=false ./cmd/admin-dashboard ./cmd/healthcheck'
run_step "go test" bash -c 'cd admin-dashboard/backend && GOFLAGS=-mod=readonly go test -count=1 ./...'

if [[ "${RUN_RACE_TESTS}" == "true" ]]; then
  run_step "go race test" bash -c 'cd admin-dashboard/backend && GOFLAGS=-mod=readonly go test -race -count=1 ./...'
fi

if [[ "${RUN_DEPENDENCY_HYGIENE}" == "true" ]]; then
  run_step "govulncheck" bash -c "cd admin-dashboard/backend && GOFLAGS=-mod=readonly '$(ensure_govulncheck)' ./..."
fi

echo "[ADMIN DASHBOARD GO CI] Passed"
