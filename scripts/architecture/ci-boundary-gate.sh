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

echo "[M0] removed runtime reference check"
"${SCRIPT_DIR}/check-removed-runtime-references.sh"
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
echo "[M2] current docs historical marker check"
"${SCRIPT_DIR}/check-current-docs-no-historical.sh"
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

echo "[M2] error contract coverage check"
"${SCRIPT_DIR}/check-error-contracts.sh"
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
