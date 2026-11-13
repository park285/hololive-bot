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

step "tracked local artifacts" "${SCRIPT_DIR}/check-tracked-local-artifacts.sh"
step "SQL ownership" "${SCRIPT_DIR}/check-sql-ownership.sh"
step "runtime import boundaries" "${SCRIPT_DIR}/check-repository-ownership.sh"
step "topology consumer parity" bash "${SCRIPT_DIR}/check-topology-parity.sh"

step "compose env" "${ROOT_DIR}/scripts/deploy/test-compose-env.sh"
step "compose services" "${ROOT_DIR}/scripts/deploy/test-compose-services.sh"
step "QUIC UDP buffer" "${ROOT_DIR}/scripts/deploy/lib/require-quic-udp-buffer_test.sh"
step "remote log sync" "${ROOT_DIR}/scripts/logs/test-remote-sync-main-logs.sh"

echo "[architecture] passed"
