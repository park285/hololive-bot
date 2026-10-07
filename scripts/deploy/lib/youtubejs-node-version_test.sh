#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
. "$ROOT_DIR/scripts/deploy/lib/youtubejs-node-version.sh"

for accepted in v24.21.0 v24.21.1 v24.99.1; do
  node_version_supported "$accepted" || {
    echo "expected accepted Node version: $accepted" >&2
    exit 1
  }
done

for rejected in v22.22.2 v23.0.0 v24.20.9 v25.9.0 v26.0.0 v27.1.0 latest ''; do
  if node_version_supported "$rejected"; then
    echo "expected rejected Node version: $rejected" >&2
    exit 1
  fi
done

echo "YouTube.js Node version checks passed"
