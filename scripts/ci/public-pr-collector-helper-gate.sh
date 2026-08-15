#!/usr/bin/env bash
set -euo pipefail

if (( $# != 0 )); then
  echo "usage: $0" >&2
  exit 2
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}/hololive/hololive-youtube-collector/youtubejs"

npm ci --ignore-scripts
npm run typecheck
npm test
