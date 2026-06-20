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

merged="$(cd "${COMPOSE_DIR}" && COMPOSE_FILE=docker-compose.prod.yml:docker-compose.live-compat.yml \
  docker compose config --no-interpolate --format json 2>/dev/null)" \
  || fail "hb06: prod+live-compat merge failed to render"

python3 - "$merged" <<'PY'
import json, sys

merged = json.loads(sys.argv[1])
services = merged.get("services", {})

def cert_sources(svc):
    out = []
    for v in svc.get("volumes", []):
        src = v.get("source", "") if isinstance(v, dict) else str(v).split(":")[0]
        if "certs" in str(src):
            out.append(str(src))
    return out

violations = []
admin = None
for name, svc in services.items():
    for c in cert_sources(svc):
        if c.rstrip("/") == "/run/hololive-bot/certs":
            violations.append(name)
    if name == "hololive-admin-api":
        admin = cert_sources(svc)

if violations:
    print("[FAIL] hb06: directory-wide /run/hololive-bot/certs mount present in: %s (a691472f)" % violations)
    sys.exit(1)

if admin is None:
    print("[FAIL] hb06: hololive-admin-api service not found in merge")
    sys.exit(1)

if any(c.rstrip("/") == "/run/hololive-bot/certs" for c in admin):
    print("[FAIL] hb06: admin-api mounts the whole certs directory (a691472f)")
    sys.exit(1)
PY

pass "hb06: live-compat overlay mounts cert files individually, no directory-wide certs mount (a691472f)"
