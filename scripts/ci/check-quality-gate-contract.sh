#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

if [[ "${QUALITY_GATE_SKIP_WORKFLOW_SECRET_CHECK:-false}" != "true" ]]; then
  bash scripts/ci/check-workflow-secrets.sh
fi

python3 scripts/ci/quality-gate-contract.py "$@"
