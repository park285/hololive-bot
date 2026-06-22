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

assert_sslrootcert_mounted() {
  local label="$1" profiles="$2"
  shift 2
  local compose_file
  compose_file="$(IFS=:; echo "$*")"

  local merged
  merged="$(cd "${COMPOSE_DIR}" && COMPOSE_FILE="${compose_file}" COMPOSE_PROFILES="${profiles}" \
    docker compose config --no-interpolate --format json 2>/dev/null)" \
    || fail "hb07: ${label} merge failed to render"

  COMPOSE_MERGE_LABEL="${label}" python3 - "${merged}" <<'PY'
import json, os, re, sys

merged = json.loads(sys.argv[1])
label = os.environ["COMPOSE_MERGE_LABEL"]
services = merged.get("services", {})

CA_ENV_KEYS = ("POSTGRES_SSLROOTCERT", "PGSSLROOTCERT")


def resolve_path(val):
    if val is None:
        return None
    s = str(val)
    m = re.match(r"^\$\{[^:}]+:-(.*)\}$", s)
    if m:
        return m.group(1)
    if s.startswith("${"):
        return None
    return s


def env_map(svc):
    env = svc.get("environment", {}) or {}
    if isinstance(env, list):
        out = {}
        for item in env:
            k, _, v = str(item).partition("=")
            out[k] = v if "=" in str(item) else None
        return out
    return env


def mount_targets(svc):
    targets = []
    for v in svc.get("volumes", []):
        if isinstance(v, dict):
            t = v.get("target", "")
        else:
            parts = str(v).split(":")
            t = parts[1] if len(parts) > 1 else ""
        if t:
            targets.append(t.rstrip("/"))
    return targets


checked = []
violations = []
for name, svc in services.items():
    env = env_map(svc)
    targets = mount_targets(svc)
    for key in CA_ENV_KEYS:
        ca_path = resolve_path(env.get(key))
        if not ca_path:
            continue
        checked.append((name, key, ca_path))
        if ca_path.rstrip("/") not in targets:
            violations.append((name, key, ca_path))

if not checked:
    print("[FAIL] hb07 (%s): no service declares a postgres CA env (%s); "
          "render or detection regressed" % (label, "/".join(CA_ENV_KEYS)))
    sys.exit(1)

if violations:
    for name, key, ca_path in sorted(violations):
        print("[FAIL] hb07 (%s): service '%s' sets %s=%s but does not bind-mount that file "
              "(unable to read CA file on redeploy — a691472f regression)"
              % (label, name, key, ca_path))
    sys.exit(1)
PY
}

assert_postgres_not_host_networked() {
  local label="$1" profiles="$2"
  shift 2
  local compose_file
  compose_file="$(IFS=:; echo "$*")"

  local merged
  merged="$(cd "${COMPOSE_DIR}" && COMPOSE_FILE="${compose_file}" COMPOSE_PROFILES="${profiles}" \
    docker compose config --no-interpolate --format json 2>/dev/null)" \
    || fail "hb08: ${label} merge failed to render"

  COMPOSE_MERGE_LABEL="${label}" python3 - "${merged}" <<'PY'
import json, os, sys

merged = json.loads(sys.argv[1])
label = os.environ["COMPOSE_MERGE_LABEL"]
services = merged.get("services", {})

violations = []
for name in ("holo-postgres", "hololive-db-migrate"):
    svc = services.get(name, {})
    if svc.get("network_mode") == "host":
        violations.append(name)

if violations:
    print("[FAIL] hb08 (%s): live-compat postgres services use host networking: %s"
          % (label, sorted(violations)))
    sys.exit(1)

pg = services.get("holo-postgres", {})
ports = pg.get("ports", []) or []
published = []
for port in ports:
    if isinstance(port, dict):
        target = str(port.get("target", ""))
        published_port = str(port.get("published", ""))
        host_ip = str(port.get("host_ip", ""))
        if target == "5432" and published_port == "5433":
            published.append(host_ip)
        continue
    parts = str(port).rsplit(":", 2)
    if len(parts) == 3 and parts[1] == "5433" and parts[2] == "5432":
        published.append(parts[0])

if not published:
    print("[FAIL] hb08 (%s): holo-postgres must publish host port 5433 to container port 5432 "
          "without using host network" % label)
    sys.exit(1)

unsafe = [ip for ip in published if ip in ("", "0.0.0.0", "::")]
if unsafe:
    print("[FAIL] hb08 (%s): holo-postgres bind IP must be explicit, not wildcard: %s"
          % (label, unsafe))
    sys.exit(1)
PY
}

assert_no_dir_mount "prod+live-compat" "" \
  docker-compose.prod.yml docker-compose.live-compat.yml

assert_no_dir_mount "main-ap+live-compat" "main-ap" \
  docker-compose.prod.yml docker-compose.live-compat.yml \
  docker-compose.main-ap.yml docker-compose.main-ap.live-compat.yml

pass "hb06: live-compat overlays mount cert files individually, no directory-wide certs mount (a691472f)"

assert_sslrootcert_mounted "prod+live-compat" "" \
  docker-compose.prod.yml docker-compose.live-compat.yml

assert_sslrootcert_mounted "main-ap+live-compat" "main-ap" \
  docker-compose.prod.yml docker-compose.live-compat.yml \
  docker-compose.main-ap.yml docker-compose.main-ap.live-compat.yml

pass "hb07: every verify-full DB service bind-mounts its POSTGRES_SSLROOTCERT/PGSSLROOTCERT file (a691472f)"

assert_postgres_not_host_networked "prod+live-compat" "" \
  docker-compose.prod.yml docker-compose.live-compat.yml

assert_postgres_not_host_networked "main-ap+live-compat" "main-ap" \
  docker-compose.prod.yml docker-compose.live-compat.yml \
  docker-compose.main-ap.yml docker-compose.main-ap.live-compat.yml

pass "hb08: live-compat postgres uses bridge networking with explicit host port binding (2b45b8a7/faa876be)"
