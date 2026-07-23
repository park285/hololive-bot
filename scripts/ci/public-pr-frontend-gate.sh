#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
FRONTEND_DIR="${ROOT_DIR}/admin-dashboard/frontend"

[[ -f "${FRONTEND_DIR}/package-lock.json" ]] || {
  echo "frontend package-lock.json is required" >&2
  exit 1
}

cd "${FRONTEND_DIR}"

echo "[public-pr] npm ci"
npm ci

echo "[public-pr] generate API client"
npm run generate:api

if ! git -C "${ROOT_DIR}" diff --exit-code -- \
  admin-dashboard/backend/docs/swagger.json \
  admin-dashboard/frontend/src/api/generated; then
  echo "generated OpenAPI artifacts are stale; run npm run generate:api and commit the result" >&2
  exit 1
fi

echo "[public-pr] frontend tests"
npm test

echo "[public-pr] frontend lint"
npm run lint

echo "[public-pr] frontend build"
npm run build
