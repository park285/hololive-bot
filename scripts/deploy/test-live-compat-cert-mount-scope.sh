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

assert_no_dir_mount() {
  local label="$1" profiles="$2"
  shift 2
  local compose_file
  compose_file="$(IFS=:; echo "$*")"

  local merged
  merged="$(cd "${COMPOSE_DIR}" && COMPOSE_FILE="${compose_file}" COMPOSE_PROFILES="${profiles}" \
    docker compose config --no-interpolate --format json 2>/dev/null)" \
    || fail "hb06: ${label} merge failed to render"

  COMPOSE_MERGE_LABEL="${label}" python3 - "${merged}" <<'PY'
import json, os, sys

merged = json.loads(sys.argv[1])
label = os.environ["COMPOSE_MERGE_LABEL"]
services = merged.get("services", {})

violations = []
for name, svc in services.items():
    for v in svc.get("volumes", []):
        src = v.get("source", "") if isinstance(v, dict) else str(v).split(":")[0]
        if str(src).rstrip("/") == "/run/hololive-bot/certs":
            violations.append(name)

if violations:
    print("[FAIL] hb06 (%s): directory-wide /run/hololive-bot/certs mount in %s (a691472f)"
          % (label, sorted(set(violations))))
    sys.exit(1)
PY
}

assert_no_dir_mount "prod+live-compat" "" \
  docker-compose.prod.yml docker-compose.live-compat.yml

assert_no_dir_mount "main-ap+live-compat" "main-ap" \
  docker-compose.prod.yml docker-compose.live-compat.yml \
  docker-compose.main-ap.yml docker-compose.main-ap.live-compat.yml

pass "hb06: live-compat overlays mount cert files individually, no directory-wide certs mount (a691472f)"
