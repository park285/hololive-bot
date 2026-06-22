#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COMPOSE_DIR="${ROOT_DIR}/deploy/compose"

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

pass() {
  echo "[PASS] $*"
}

if ! docker compose version >/dev/null 2>&1; then
  echo "[SKIP] docker compose unavailable" >&2
  exit 0
fi

merged="$(cd "${COMPOSE_DIR}" && COMPOSE_FILE=docker-compose.prod.yml docker compose config --no-interpolate --format json 2>/dev/null)" \
  || fail "prod compose failed to render"

python3 - "${merged}" <<'PY'
import json, sys

merged = json.loads(sys.argv[1])
services = merged.get("services", {})

def env_map(svc):
    env = svc.get("environment", {}) or {}
    if isinstance(env, list):
        out = {}
        for item in env:
            key, sep, value = str(item).partition("=")
            out[key] = value if sep else None
        return out
    return env

admin_api = services.get("hololive-admin-api", {})
env = env_map(admin_api)
if env.get("CORS_ENFORCE") != "${CORS_ENFORCE:-true}":
    print("[FAIL] hololive-admin-api CORS_ENFORCE default must be true")
    sys.exit(1)
origins = str(env.get("CORS_ALLOWED_ORIGINS", ""))
if origins in ("", "${CORS_ALLOWED_ORIGINS:-}"):
    print("[FAIL] hololive-admin-api CORS_ALLOWED_ORIGINS default must be explicit")
    sys.exit(1)

dashboard = services.get("admin-dashboard", {})
ports = dashboard.get("ports", []) or []
if not any("127.0.0.1" in str(port) and "30190" in str(port) for port in ports):
    print("[FAIL] admin-dashboard prod default must bind 30190 to loopback")
    sys.exit(1)
PY
pass "prod compose security defaults are explicit"

merged_live="$(cd "${COMPOSE_DIR}" && COMPOSE_FILE=docker-compose.prod.yml:docker-compose.live-compat.yml docker compose config --no-interpolate --format json 2>/dev/null)" \
  || fail "prod+live-compat compose failed to render"

python3 - "${merged_live}" <<'PY'
import json, sys

merged = json.loads(sys.argv[1])
dashboard = merged.get("services", {}).get("admin-dashboard", {})
ports = dashboard.get("ports", []) or []
if not any("127.0.0.1" in str(port) and "30190" in str(port) for port in ports):
    print("[FAIL] admin-dashboard live-compat default must stay loopback; Tailscale exposure is opt-in")
    sys.exit(1)
def env_map(svc):
    env = svc.get("environment", {}) or {}
    if isinstance(env, list):
        out = {}
        for item in env:
            key, sep, value = str(item).partition("=")
            out[key] = value if sep else None
        return out
    return env

env = env_map(dashboard)
origins = str(env.get("ALLOWED_ORIGINS", ""))
if "100.100.1.3:30190" in origins:
    print("[FAIL] admin-dashboard live-compat default origins must not include Tailscale host")
    sys.exit(1)
PY
pass "live-compat dashboard exposure is opt-in by default"
