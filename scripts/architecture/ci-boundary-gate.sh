#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
GO_GRAPH_OUT="${1:-${ROOT_DIR}/artifacts/architecture/go-workspace-import-graph.txt}"

echo "[CI] Architecture boundary gate start"
echo

echo "[CI] Run M0 gate"
echo "[M0] shared-go boundary check"
"${SCRIPT_DIR}/check-shared-go-boundary.sh"
echo

echo "[M0] shared-go package allowlist check"
"${SCRIPT_DIR}/check-shared-go-packages.sh"
echo

echo "[M0] go compatibility adapter check"
"${SCRIPT_DIR}/check-go-compat-adapters.sh"
echo

echo "[M0] generic Go internal package name check"
"${SCRIPT_DIR}/check-go-generic-internal-package-names.sh"
echo

echo "[M0] cross-cutting boundary guardrail check"
"${SCRIPT_DIR}/check-crosscutting-guardrails.sh"
echo

echo "[M0] removed runtime regression check"
"${SCRIPT_DIR}/check-removed-runtime-regressions.sh"
echo

echo "[M0] tracked local artifact check"
"${SCRIPT_DIR}/check-tracked-local-artifacts.sh"
echo

echo "[M0] go workspace import graph export"
"${SCRIPT_DIR}/export-go-workspace-import-graph.sh" "${GO_GRAPH_OUT}"
echo

echo "[M0] project map consistency check"
"${SCRIPT_DIR}/check-project-map.sh"
echo

echo "[CI] Run M1 contract gate"
echo "[M1] Go alarm contract sanity check"
"${SCRIPT_DIR}/check-go-alarm-contracts.sh"
echo

echo "[M1] Go trigger route hardcoding check"
"${SCRIPT_DIR}/check-go-trigger-route-hardcoding.sh"
echo

echo "[M1] migration manifest check"
"${SCRIPT_DIR}/check-migration-manifest.sh"
echo

echo "[CI] Run M2 document contract gate"
echo "[M2] current docs historical body check"
"${SCRIPT_DIR}/check-current-docs-no-historical-body.sh"
echo

echo "[M2] current docs root allowlist check"
"${SCRIPT_DIR}/check-current-docs-root-allowlist.sh"
echo

echo "[M2] markdown local path check"
"${SCRIPT_DIR}/check-doc-links-no-local-paths.sh"
echo

echo "[M2] legacy docs plan-kit location check"
"${SCRIPT_DIR}/check-docs-plan-kit-location.sh"
echo

echo "[M2] runtime runbook coverage check"
"${SCRIPT_DIR}/check-runbook-coverage.sh"
echo

echo "[M2] contract map coverage check"
"${SCRIPT_DIR}/check-contract-map.sh"
echo

echo "[M2] internal route hardcoding check"
"${SCRIPT_DIR}/check-internal-route-hardcoding.sh"
echo

echo "[M2] repository ownership boundary check"
"${SCRIPT_DIR}/check-repository-ownership.sh"
echo

echo "[M2] notification egress ownership check"
"${SCRIPT_DIR}/ci-notification-egress-gate.sh"
echo

echo "[M2] error contract coverage check"
"${SCRIPT_DIR}/check-error-contracts.sh"
echo

echo "[CI] Run M4 compose env helper gate"
bash -n "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh" \
    "${ROOT_DIR}/scripts/deploy/lib/compose-services.sh" \
    "${ROOT_DIR}/scripts/deploy/lib/removed-runtimes.sh" \
    "${ROOT_DIR}/scripts/deploy/lib/health-gate.sh" \
    "${ROOT_DIR}/scripts/deploy/lib/health-gate_test.sh" \
    "${ROOT_DIR}/scripts/deploy/compose.sh" \
    "${ROOT_DIR}/scripts/deploy/test-compose-env.sh" \
    "${ROOT_DIR}/scripts/deploy/test-compose-security-defaults.sh" \
    "${ROOT_DIR}/scripts/deploy/test-compose-services.sh" \
    "${ROOT_DIR}/scripts/deploy/test-three-runtime-topology.sh" \
    "${ROOT_DIR}/scripts/deploy/test-compose-h3-contract.sh" \
    "${ROOT_DIR}/scripts/deploy/test-live-compat-cert-mount-scope.sh" \
    "${ROOT_DIR}/scripts/deploy/test-removed-runtimes.sh" \
    "${ROOT_DIR}/scripts/logs/remote-sync-main-logs.sh" \
    "${ROOT_DIR}/scripts/logs/test-remote-sync-main-logs.sh"
"${ROOT_DIR}/scripts/deploy/test-compose-env.sh"
"${ROOT_DIR}/scripts/deploy/lib/health-gate_test.sh"
"${ROOT_DIR}/scripts/deploy/test-compose-security-defaults.sh"
"${ROOT_DIR}/scripts/deploy/test-compose-services.sh"
"${ROOT_DIR}/scripts/deploy/test-three-runtime-topology.sh"
"${ROOT_DIR}/scripts/deploy/test-compose-h3-contract.sh"
"${ROOT_DIR}/scripts/deploy/test-live-compat-cert-mount-scope.sh"
"${ROOT_DIR}/scripts/deploy/test-removed-runtimes.sh"
"${ROOT_DIR}/scripts/logs/test-remote-sync-main-logs.sh"
echo

echo "[CI] Run M4 Go module LOC gate"
"${SCRIPT_DIR}/check-go-module-loc.sh"
echo

echo "[CI] Run M4 Go function budget gate"
"${SCRIPT_DIR}/check-function-budget.sh"
echo

echo "[CI] Run M4 multi-language file LOC gate"
"${SCRIPT_DIR}/check-file-loc.sh"
echo

echo "[CI] Run M6 deprecated deadline gate"
echo "[M6] Deprecated removal deadline gate"
"${SCRIPT_DIR}/check-deprecated-deadline.sh"
echo

echo "[M6] Release governance assets gate"
"${SCRIPT_DIR}/check-release-governance-assets.sh"
echo

echo "[CI] Architecture boundary gate passed"
