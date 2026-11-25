#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

step() {
  echo "[architecture] $1"
  shift
  "$@"
  echo
}

step "tracked local artifacts" "${SCRIPT_DIR}/check-tracked-local-artifacts.sh"
step "SQL ownership" "${SCRIPT_DIR}/check-sql-ownership.sh"
step "topology consumer parity" bash "${SCRIPT_DIR}/check-topology-parity.sh"

echo "[architecture] passed"
