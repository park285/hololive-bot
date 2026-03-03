#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
GO_GRAPH_OUT="${1:-${ROOT_DIR}/artifacts/architecture/go-workspace-import-graph.txt}"

echo "[M0] rust service->infra gate"
"${SCRIPT_DIR}/check-rust-service-infra.sh"
echo

echo "[M0] admin↔kakao duplicate gate"
"${SCRIPT_DIR}/check-admin-kakao-duplicates.sh"
echo

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

echo "[M0] admin↔kakao duplicate report"
"${SCRIPT_DIR}/report-admin-kakao-duplicates.sh"
