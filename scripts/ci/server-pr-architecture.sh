#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ARCH_DIR="${ROOT_DIR}/scripts/architecture"
cd "${ROOT_DIR}"

run_m0() {
  "${ARCH_DIR}/check-shared-go-boundary.sh"
  "${ARCH_DIR}/check-shared-go-packages.sh"
  "${ARCH_DIR}/check-go-compat-adapters.sh"
  "${ARCH_DIR}/check-go-generic-internal-package-names.sh"
  "${ARCH_DIR}/check-crosscutting-guardrails.sh"
  "${ARCH_DIR}/check-removed-runtime-regressions.sh"
  "${ARCH_DIR}/check-removed-runtime-build-paths.sh"
  "${ARCH_DIR}/check-tracked-local-artifacts.sh"
  "${ARCH_DIR}/export-go-workspace-import-graph.sh" "${ROOT_DIR}/artifacts/architecture/go-workspace-import-graph.txt"
  "${ARCH_DIR}/check-project-map.sh"
}

run_m1() {
  "${ARCH_DIR}/check-go-alarm-contracts.sh"
  "${ARCH_DIR}/check-go-trigger-route-hardcoding.sh"
  "${ARCH_DIR}/check-migration-manifest.sh"
  "${ARCH_DIR}/check-sql-ownership.sh"
  "${ARCH_DIR}/check-db-access-policy.sh"
}

run_m2() {
  "${ARCH_DIR}/check-current-docs-no-historical-body.sh"
  "${ARCH_DIR}/check-current-docs-root-allowlist.sh"
  "${ARCH_DIR}/check-doc-links-no-local-paths.sh"
  "${ARCH_DIR}/check-docs-plan-kit-location.sh"
  "${ARCH_DIR}/check-runbook-coverage.sh"
  "${ARCH_DIR}/check-contract-map.sh"
  "${ARCH_DIR}/check-internal-route-hardcoding.sh"
  "${ARCH_DIR}/check-repository-ownership.sh"
  "${ARCH_DIR}/ci-notification-egress-gate.sh"
  "${ARCH_DIR}/check-error-contracts.sh"
}

run_m4() {
  bash -n \
    scripts/deploy/lib/compose-env.sh \
    scripts/deploy/lib/compose-services.sh \
    scripts/deploy/lib/removed-runtimes.sh \
    scripts/deploy/lib/health-gate.sh \
    scripts/deploy/lib/health-gate_test.sh \
    scripts/deploy/compose.sh \
    scripts/deploy/test-compose-env.sh \
    scripts/deploy/test-compose-security-defaults.sh \
    scripts/deploy/test-compose-services.sh \
    scripts/deploy/test-three-runtime-topology.sh \
    scripts/deploy/test-compose-h3-contract.sh \
    scripts/deploy/test-live-compat-cert-mount-scope.sh \
    scripts/deploy/test-removed-runtimes.sh \
    scripts/logs/remote-sync-main-logs.sh \
    scripts/logs/test-remote-sync-main-logs.sh
  scripts/deploy/test-compose-env.sh
  scripts/deploy/lib/health-gate_test.sh
  scripts/deploy/test-compose-security-defaults.sh
  scripts/deploy/test-compose-services.sh
  scripts/deploy/test-three-runtime-topology.sh
  scripts/deploy/test-compose-h3-contract.sh
  scripts/deploy/test-live-compat-cert-mount-scope.sh
  scripts/deploy/test-removed-runtimes.sh
  scripts/logs/test-remote-sync-main-logs.sh
  "${ARCH_DIR}/check-go-module-loc.sh"
  "${ARCH_DIR}/check-function-budget.sh"
  "${ARCH_DIR}/check-file-loc.sh"
}

run_m6() {
  "${ARCH_DIR}/check-deprecated-deadline.sh"
  "${ARCH_DIR}/check-release-governance-assets.sh"
}

case "${1:-all}" in
  m0) run_m0 ;;
  m1) run_m1 ;;
  m2) run_m2 ;;
  m4) run_m4 ;;
  m6) run_m6 ;;
  all)
    run_m0
    run_m1
    run_m2
    run_m4
    run_m6
    ;;
  *)
    echo "usage: $0 [all|m0|m1|m2|m4|m6]" >&2
    exit 2
    ;;
esac
