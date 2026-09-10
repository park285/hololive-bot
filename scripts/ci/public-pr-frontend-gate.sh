#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
FRONTEND_DIR="${ROOT_DIR}/admin-dashboard/frontend"
NODE_VERSION_LIB="${ROOT_DIR}/scripts/deploy/lib/youtubejs-node-version.sh"

# shellcheck source=scripts/deploy/lib/youtubejs-node-version.sh
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

# GitHub의 일회성 runner에도 로컬 browser gate와 같은 세 엔진·user cgroup 실행 조건을 준비한다.
if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
  corepack npm exec -- playwright install --with-deps firefox webkit
  sudo -n loginctl enable-linger "$(id -un)"
  sudo -n systemctl start "user@$(id -u).service"
  XDG_RUNTIME_DIR="/run/user/$(id -u)"
  export XDG_RUNTIME_DIR
  export DBUS_SESSION_BUS_ADDRESS="unix:path=${XDG_RUNTIME_DIR}/bus"
  [[ -S "${XDG_RUNTIME_DIR}/bus" && -x /usr/bin/google-chrome ]] || {
    echo "browser gate requires a user systemd bus and Google Chrome" >&2
    exit 1
  }
fi

echo "[public-pr] generate API client"
corepack npm run generate:api
node "${ROOT_DIR}/scripts/architecture/generate-admin-docker-policy.mjs" --check

generated_status="$(git -C "${ROOT_DIR}" status --porcelain -- \
  admin-dashboard/backend/internal/contract/operations_generated.go \
  admin-dashboard/frontend/src/api/generated)"
if [[ -n "${generated_status}" ]]; then
  git -C "${ROOT_DIR}" diff -- \
    admin-dashboard/backend/internal/contract/operations_generated.go \
    admin-dashboard/frontend/src/api/generated || true
  printf '%s\n' "${generated_status}" >&2
  echo "generated OpenAPI artifacts are stale; run corepack npm run generate:api and commit the result" >&2
  exit 1
fi

echo "[public-pr] frontend tests"
corepack npm test
corepack npm run test:contract

echo "[public-pr] frontend lint"
corepack npm run lint

echo "[public-pr] frontend build"
corepack npm run build

echo "[public-pr] source and production bundle retirement"
bash "${ROOT_DIR}/scripts/architecture/check-admin-retirement.sh" --source-bundle
node "${ROOT_DIR}/scripts/architecture/check-admin-feature-parity.mjs"
