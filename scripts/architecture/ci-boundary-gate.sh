#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
GO_GRAPH_OUT="${1:-${ROOT_DIR}/artifacts/architecture/go-workspace-import-graph.txt}"

echo "[CI] Architecture boundary gate start"
echo

echo "[CI] Run M0 gate"
"${SCRIPT_DIR}/m0-gate.sh" "${GO_GRAPH_OUT}"
echo

echo "[CI] Run M1 contract gate"
"${SCRIPT_DIR}/m1-contract-gate.sh"
echo

echo "[CI] Run M4 Go module LOC gate"
"${SCRIPT_DIR}/check-go-module-loc.sh"
echo

echo "[CI] Run M6 deprecated deadline gate"
"${SCRIPT_DIR}/m6-gate.sh"
echo

echo "[CI] Run runtime cross-dependency gate"
"${SCRIPT_DIR}/check-rust-cross-deps.sh"
echo

echo "[CI] Architecture boundary gate passed"
