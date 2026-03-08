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

echo "[M0] go workspace import graph export"
"${SCRIPT_DIR}/export-go-workspace-import-graph.sh" "${GO_GRAPH_OUT}"
echo

echo "[CI] Run M1 contract gate"
echo "[M1] Go alarm contract sanity check"
"${SCRIPT_DIR}/check-go-alarm-contracts.sh"
echo

echo "[M1] Go trigger route hardcoding check"
"${SCRIPT_DIR}/check-go-trigger-route-hardcoding.sh"
echo

echo "[CI] Run M4 Go module LOC gate"
"${SCRIPT_DIR}/check-go-module-loc.sh"
echo

echo "[CI] Run M6 deprecated deadline gate"
echo "[M6] Deprecated removal deadline gate"
"${SCRIPT_DIR}/check-deprecated-deadline.sh"
echo

echo "[M6] Release governance assets gate"
"${SCRIPT_DIR}/check-release-governance-assets.sh"
echo

echo "[CI] Architecture boundary gate passed"
