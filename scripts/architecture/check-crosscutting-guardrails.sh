#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
python3 "${SCRIPT_DIR}/check-crosscutting-guardrails.py" --root "${ROOT_DIR}" --profile "hololive-bot" "$@"
