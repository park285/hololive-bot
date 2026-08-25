#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
args=(--mode hard --format text --component files)
if (( $# > 1 )); then
  echo "usage: $0 [THRESHOLD_FILE]" >&2
  exit 2
fi
if (( $# == 1 )); then
  args+=(--threshold-file "$1")
fi
exec bash "$ROOT_DIR/scripts/ci/check-structure.sh" "${args[@]}"
