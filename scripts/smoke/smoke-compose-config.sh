#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

echo "[SMOKE] docker compose production config"
docker compose -f "${ROOT_DIR}/docker-compose.prod.yml" config --no-interpolate >/dev/null
echo "[PASS] docker compose production config is valid"
