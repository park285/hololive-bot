#!/usr/bin/env bash
set -euo pipefail

if (( $# != 0 )); then
  echo "usage: $0" >&2
  exit 2
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
bash "${ROOT_DIR}/scripts/ci/public-pr-x-spaces-helper-install.sh"
cd "${ROOT_DIR}/hololive/hololive-alarm-worker/xspaces"

npm test
