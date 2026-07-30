#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
FRONTEND_DIR="${ROOT_DIR}/admin-dashboard/frontend"

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

node - <<'NODE'
const [major, minor, patch] = process.versions.node.split('.').map(Number)
const supported = major > 22 || (major === 22 && (minor > 22 || (minor === 22 && patch >= 2)))
if (!supported) {
  console.error(`unsupported Node.js ${process.versions.node}; expected >=22.22.2`)
  process.exit(1)
}
console.log(`[public-pr] Node.js ${process.versions.node}, Corepack-managed npm available`)
NODE

cd "${FRONTEND_DIR}"

echo "[public-pr] corepack npm ci"
corepack npm ci

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
