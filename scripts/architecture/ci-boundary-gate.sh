#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

step() {
  echo "[architecture] $1"
  shift
  "$@"
  echo
}

step "shared-go boundary" "${SCRIPT_DIR}/check-shared-go-boundary.sh"
step "shared-go package allowlist" "${SCRIPT_DIR}/check-shared-go-packages.sh"
step "generic Go internal package names" "${SCRIPT_DIR}/check-go-generic-internal-package-names.sh"
step "tracked local artifacts" "${SCRIPT_DIR}/check-tracked-local-artifacts.sh"
step "Go alarm contract values" "${SCRIPT_DIR}/check-go-alarm-contracts.sh"
step "Go trigger route hardcoding" "${SCRIPT_DIR}/check-go-trigger-route-hardcoding.sh"
step "internal route hardcoding" "${SCRIPT_DIR}/check-internal-route-hardcoding.sh"
step "migration manifest" "${SCRIPT_DIR}/check-migration-manifest.sh"
step "SQL ownership" "${SCRIPT_DIR}/check-sql-ownership.sh"
step "DB access policy" "${SCRIPT_DIR}/check-db-access-policy.sh"
step "markdown local paths" "${SCRIPT_DIR}/check-doc-links-no-local-paths.sh"
step "runtime import boundaries" "${SCRIPT_DIR}/check-repository-ownership.sh"
step "notification egress ownership" "${SCRIPT_DIR}/ci-notification-egress-gate.sh"
step "topology consumer parity" bash "${SCRIPT_DIR}/check-topology-parity.sh"

step "shell syntax" bash "${ROOT_DIR}/scripts/ci/shell-syntax-sweep.sh"
step "compose env" "${ROOT_DIR}/scripts/deploy/test-compose-env.sh"
step "health gate" "${ROOT_DIR}/scripts/deploy/lib/health-gate_test.sh"
step "compose security defaults" "${ROOT_DIR}/scripts/deploy/test-compose-security-defaults.sh"
step "PostgreSQL 18 runtime" bash "${ROOT_DIR}/scripts/deploy/test-postgres18-runtime-contract.sh"
step "compose services" "${ROOT_DIR}/scripts/deploy/test-compose-services.sh"
step "three-runtime topology" "${ROOT_DIR}/scripts/deploy/test-three-runtime-topology.sh"
step "compose H3" "${ROOT_DIR}/scripts/deploy/test-compose-h3-contract.sh"
step "live-compat cert mounts" "${ROOT_DIR}/scripts/deploy/test-live-compat-cert-mount-scope.sh"
step "compose up flow" "${ROOT_DIR}/scripts/deploy/test-compose-up-flow.sh"
step "remote log sync" "${ROOT_DIR}/scripts/logs/test-remote-sync-main-logs.sh"
step "PostgreSQL failover" bash "${ROOT_DIR}/scripts/ops/postgres-failover_test.sh"

step "deprecated removal deadlines" "${SCRIPT_DIR}/check-deprecated-deadline.sh"

echo "[architecture] passed"
