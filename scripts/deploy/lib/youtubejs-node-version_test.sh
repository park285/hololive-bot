#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
. "$ROOT_DIR/scripts/deploy/lib/youtubejs-node-version.sh"

for accepted in v24.20.0 v24.20.1 v24.99.1; do
  node_version_supported "$accepted" || {
    echo "expected accepted Node version: $accepted" >&2
    exit 1
  }
done

for rejected in v22.22.2 v23.0.0 v24.19.9 v25.9.0 v26.0.0 v27.1.0 latest ''; do
  if node_version_supported "$rejected"; then
    echo "expected rejected Node version: $rejected" >&2
    exit 1
  fi
done

frontend_gate="$ROOT_DIR/scripts/ci/public-pr-frontend-gate.sh"
native_deploy="$ROOT_DIR/scripts/deploy/ap-host-native-deploy.sh"
remote_apply="$ROOT_DIR/scripts/deploy/lib/ap-host-native-remote-apply.sh"
completion_check="$ROOT_DIR/scripts/deploy/ap-completion-check.sh"

grep -Fq 'require_node_version node' "$frontend_gate"
grep -Fq 'npm_config_engine_strict=true corepack npm ci' "$frontend_gate"
grep -Fq 'npm_config_engine_strict=true npm ci --omit=dev' "$native_deploy"
grep -Fq 'require_node_version /usr/bin/node' "$remote_apply"
grep -Fq 'require_node_version node' "$completion_check"
grep -Fq 'node_version_supported \"\$node_version\"' "$completion_check"

echo "YouTube.js Node version checks passed"
