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
require_command npm

node - <<'NODE'
const [major, minor] = process.versions.node.split('.').map(Number)
const supported = major > 22 || (major === 22 && minor >= 12) || (major === 20 && minor >= 19)
if (!supported) {
  console.error(`unsupported Node.js ${process.versions.node}; expected >=20.19 or >=22.12`)
  process.exit(1)
}
console.log(`[public-pr] Node.js ${process.versions.node}, npm runtime available`)
NODE

cd "${FRONTEND_DIR}"

echo "[public-pr] npm ci"
npm ci

echo "[public-pr] generate API client"
npm run generate:api

generated_status="$(git -C "${ROOT_DIR}" status --porcelain -- \
  admin-dashboard/backend/docs/swagger.json \
  admin-dashboard/frontend/src/api/generated)"
if [[ -n "${generated_status}" ]]; then
  git -C "${ROOT_DIR}" diff -- \
    admin-dashboard/backend/docs/swagger.json \
    admin-dashboard/frontend/src/api/generated || true
  printf '%s\n' "${generated_status}" >&2
  echo "generated OpenAPI artifacts are stale; run npm run generate:api and commit the result" >&2
  exit 1
fi

echo "[public-pr] frontend tests"
npm test

echo "[public-pr] frontend lint"
npm run lint

echo "[public-pr] frontend build"
npm run build
