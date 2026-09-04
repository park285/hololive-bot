#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
FRONTEND_DIR="${ROOT_DIR}/admin-dashboard/frontend"
NODE_VERSION_LIB="${ROOT_DIR}/scripts/deploy/lib/youtubejs-node-version.sh"

. "${NODE_VERSION_LIB}"

[[ -f "${FRONTEND_DIR}/package-lock.json" ]] || {
  echo "frontend package-lock.json is required" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "required command not found: $1" >&2
    exit 1
  }
}

require_command node
require_command corepack

require_node_version node
echo "[public-pr] Node.js $(node --version), Corepack-managed npm available"

cd "${FRONTEND_DIR}"

echo "[public-pr] corepack npm ci"
npm_config_engine_strict=true corepack npm ci

echo "[public-pr] generate API client"
corepack npm run generate:api

generated_status="$(git -C "${ROOT_DIR}" status --porcelain -- \
  admin-dashboard/backend/docs/swagger.json \
  admin-dashboard/frontend/src/api/generated)"
if [[ -n "${generated_status}" ]]; then
  git -C "${ROOT_DIR}" diff -- \
    admin-dashboard/backend/docs/swagger.json \
    admin-dashboard/frontend/src/api/generated || true
  printf '%s\n' "${generated_status}" >&2
  echo "generated OpenAPI artifacts are stale; run corepack npm run generate:api and commit the result" >&2
  exit 1
fi

echo "[public-pr] frontend tests"
corepack npm test

echo "[public-pr] frontend lint"
corepack npm run lint

echo "[public-pr] frontend build"
corepack npm run build
