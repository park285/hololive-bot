#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "${ROOT_DIR}/scripts/ci/python-runtime.sh"
repo_python_init
COMPOSE_DIR="${ROOT_DIR}/deploy/compose"

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

pass() {
  echo "[PASS] $*"
}

HBA_FILE="${COMPOSE_DIR}/postgres/pg_hba.conf"
if grep -Eq '^host[[:space:]]+all[[:space:]]+all[[:space:]]+all([[:space:]]|$)' "${HBA_FILE}"; then
  fail "PostgreSQL HBA must not permit non-TLS clients from arbitrary addresses"
fi
for source in 172.16.0.0/12 100.100.1.2/32 100.100.1.3/32 100.100.1.5/32 100.100.1.6/32 100.100.1.8/32; do
  grep -Eq "^hostssl[[:space:]]+all[[:space:]]+all[[:space:]]+${source//./\\.}[[:space:]]+scram-sha-256$" "${HBA_FILE}" \
    || fail "PostgreSQL HBA is missing the TLS-only client source ${source}"
done
pass "PostgreSQL HBA permits only TLS-authenticated runtime source ranges"

INGRESS_TEMPLATE="${ROOT_DIR}/deploy/nginx/admin-dashboard-ingress.conf.template"
PUBLIC_SHORTLINK="${ROOT_DIR}/deploy/nginx/holoshi-public-shortlink.conf"
grep -Fq 'limit_conn_zone $shortlink_client zone=shortlink_connections:1m;' "${INGRESS_TEMPLATE}" \
  || fail "central shortlink concurrency must use the forwarded client identity"
grep -Fq 'limit_req zone=holoshi_shortlink_requests' "${PUBLIC_SHORTLINK}" \
  || fail "public shortlink ingress must apply a per-client request limit"
grep -Fq 'limit_conn holoshi_shortlink_connections' "${PUBLIC_SHORTLINK}" \
  || fail "public shortlink ingress must apply a per-client connection limit"
pass "shortlink ingress limits are keyed per client at both proxy hops"

if ! docker compose version >/dev/null 2>&1; then
  echo "[SKIP] docker compose unavailable" >&2
  exit 0
fi

if docker compose \
  --env-file "${COMPOSE_DIR}/build-only.env.sample" \
  -f "${COMPOSE_DIR}/docker-compose.prod.yml" \
  config --quiet >/dev/null 2>&1; then
  pass "build-only compose render does not read live runtime env files"
else
  fail "build-only compose render must use committed runtime env placeholders"
fi

merged="$(cd "${COMPOSE_DIR}" && COMPOSE_FILE=docker-compose.prod.yml docker compose config --no-interpolate --format json 2>/dev/null)" \
  || fail "prod compose failed to render"

"${CI_PYTHON_BIN}" - "${merged}" <<'PY'
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

admin_api = services.get("hololive-api", {})
env = env_map(admin_api)
for service_name in ("hololive-api", "hololive-alarm-worker"):
    service_env = env_map(services.get(service_name, {}))
    for token_name in ("IRIS_WEBHOOK_TOKEN", "IRIS_BOT_TOKEN"):
        if token_name in service_env:
            print(f"[FAIL] {service_name} common environment must not shadow {token_name} from env_file")
            sys.exit(1)
if env.get("CORS_ENFORCE") != "${CORS_ENFORCE:-true}":
    print("[FAIL] hololive-api CORS_ENFORCE default must be true")
    sys.exit(1)
origins = str(env.get("CORS_ALLOWED_ORIGINS", ""))
if origins in ("", "${CORS_ALLOWED_ORIGINS:-}"):
    print("[FAIL] hololive-api CORS_ALLOWED_ORIGINS default must be explicit")
    sys.exit(1)

postgres = services.get("holo-postgres", {})
command = postgres.get("command", []) or []
if "max_connections=60" not in [str(item) for item in command]:
    print("[FAIL] holo-postgres command must pin max_connections=60 with the PG18 memory GUCs")
    sys.exit(1)
postgres_env = env_map(postgres)
if "--data-checksums" not in str(postgres_env.get("POSTGRES_INITDB_ARGS", "")):
    print("[FAIL] holo-postgres must initialize fresh PG18 clusters with data checksums")
    sys.exit(1)

valkey = services.get("valkey-cache", {})
if valkey.get("user") != "999:1000":
    print("[FAIL] valkey-cache must run as 999:1000 so the unix socket stays gid-1000/0660")
    sys.exit(1)

dashboard = services.get("admin-dashboard", {})
ports = dashboard.get("ports", []) or []
if not any("127.0.0.1" in str(port) and "30190" in str(port) for port in ports):
    print("[FAIL] admin-dashboard prod default must bind 30190 to loopback")
    sys.exit(1)
PY
pass "prod compose security and PostgreSQL budget defaults are explicit"

merged_live="$(cd "${COMPOSE_DIR}" && COMPOSE_FILE=docker-compose.prod.yml:docker-compose.live-compat.yml docker compose config --no-interpolate --format json 2>/dev/null)" \
  || fail "prod+live-compat compose failed to render"

"${CI_PYTHON_BIN}" - "${merged_live}" <<'PY'
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

ingress = merged.get("services", {}).get("admin-dashboard-ingress", {})
health = (ingress.get("healthcheck", {}) or {}).get("test", []) or []
if not health or health[0] != "CMD" or any("HOLOLIVE_BOT_PORT_BIND_IP" in str(item) for item in health):
    print("[FAIL] admin-dashboard-ingress healthcheck must avoid shell interpolation of the bind address")
    sys.exit(1)
PY
pass "live-compat dashboard exposure is opt-in by default"

nginx_image="$("${CI_PYTHON_BIN}" - "${COMPOSE_DIR}/docker-compose.live-compat.yml" <<'PY'
import re, sys

content = open(sys.argv[1], encoding="utf-8").read()
match = re.search(r'^\s*image:\s*\$\{NGINX_IMAGE:-([^}]+)}\s*$', content, re.MULTILINE)
if not match:
    print("[FAIL] pinned admin-dashboard-ingress image default is missing", file=sys.stderr)
    sys.exit(1)
print(match.group(1))
PY
)" || fail "could not resolve pinned admin-dashboard-ingress image"

nginx_test_dir="$(mktemp -d)"
trap 'rm -rf -- "${nginx_test_dir}"' EXIT
. "${ROOT_DIR}/scripts/deploy/lib/public-bind-mounts.sh"
HOLOLIVE_BOT_PORT_BIND_IP=127.0.0.1 \
HOLOLIVE_INGRESS_CONF="${nginx_test_dir}/admin-dashboard-ingress.conf" \
  prepare_admin_dashboard_ingress_bind_mount "${ROOT_DIR}" \
  || fail "could not render admin-dashboard-ingress config from the template"
docker run --rm \
  --network host \
  --read-only \
  --tmpfs /tmp:size=16m \
  --tmpfs /var/cache/nginx:size=16m \
  --tmpfs /var/run:size=1m \
  -v "${nginx_test_dir}/admin-dashboard-ingress.conf:/etc/nginx/admin-dashboard-ingress.conf:ro" \
  "${nginx_image}" \
  nginx -t -c /etc/nginx/admin-dashboard-ingress.conf \
  || fail "admin-dashboard-ingress nginx -t failed"
pass "admin-dashboard-ingress config passes nginx -t with the pinned image"

sed \
  -e 's/listen 443 ssl;/listen 127.0.0.1:30999;/' \
  -e '/listen 443 quic;/d' \
  -e '/include \/etc\/nginx\/tls.conf;/d' \
  -e "/if (\\\$blocked_request)/d" \
  -e '/include \/etc\/nginx\/proxy.conf;/d' \
  "${ROOT_DIR}/deploy/nginx/holoshi-public-shortlink.conf" \
  >"${nginx_test_dir}/holoshi-public-shortlink.test.conf"
printf '%s\n' \
  'worker_processes 1;' \
  'pid /tmp/nginx.pid;' \
  'events { worker_connections 16; }' \
  'http {' \
  '  log_format ingress_json "$status";' \
  '  access_log off;' \
  '  error_log /dev/stderr warn;' \
  '  include /etc/nginx/holoshi-public-shortlink.test.conf;' \
  '}' \
  >"${nginx_test_dir}/holoshi-public-test.conf"
docker run --rm \
  --network host \
  --read-only \
  --tmpfs /tmp:size=16m \
  --tmpfs /var/cache/nginx:size=16m \
  --tmpfs /var/run:size=1m \
  -v "${nginx_test_dir}/holoshi-public-test.conf:/etc/nginx/holoshi-public-test.conf:ro" \
  -v "${nginx_test_dir}/holoshi-public-shortlink.test.conf:/etc/nginx/holoshi-public-shortlink.test.conf:ro" \
  "${nginx_image}" \
  nginx -t -c /etc/nginx/holoshi-public-test.conf \
  || fail "holoshi public ingress nginx -t failed"
pass "holoshi public ingress template passes nginx -t with the pinned image"

merged_main_ap="$(cd "${COMPOSE_DIR}" && COMPOSE_FILE=docker-compose.prod.yml:docker-compose.live-compat.yml:docker-compose.main-ap.yml:docker-compose.main-ap.live-compat.yml COMPOSE_PROFILES=main-ap docker compose config --no-interpolate --format json 2>/dev/null)" \
  || fail "prod+main-ap compose failed to render"

"${CI_PYTHON_BIN}" - "${merged_main_ap}" <<'PY'
import json, sys

merged = json.loads(sys.argv[1])
services = merged.get("services", {})
networks = merged.get("networks", {})

traces = networks.get("observability-traces", {})
if traces.get("external") is not True or traces.get("name") != "observability-traces":
    print("[FAIL] observability-traces must be the explicitly named external network")
    sys.exit(1)

participants = {
    name
    for name, service in services.items()
    if "observability-traces" in (service.get("networks", {}) or {})
}
expected = {"hololive-api", "hololive-alarm-worker", "youtube-collector"}
if participants != expected:
    print(f"[FAIL] observability-traces participants: expected {sorted(expected)}, got {sorted(participants)}")
    sys.exit(1)

forbidden_ports = {4317, 4318, 8888, 13133, 16685, 16686}
for name, service in services.items():
    for port in service.get("ports", []) or []:
        if isinstance(port, dict):
            published = port.get("published")
            target = port.get("target")
            exposed = {int(value) for value in (published, target) if str(value).isdigit()}
        else:
            fields = str(port).split(":")
            exposed = {int(value.split("/")[0]) for value in fields if value.split("/")[0].isdigit()}
        blocked = exposed & forbidden_ports
        if blocked:
            print(f"[FAIL] {name} publishes forbidden Jaeger/OTLP port(s): {sorted(blocked)}")
            sys.exit(1)
PY
pass "central runtimes alone join the external trace network without Jaeger or OTLP host ports"

while read -r service compose_file; do
  merged_ap="$(cd "${COMPOSE_DIR}" && COMPOSE_FILE="docker-compose.prod.yml:${compose_file}" COMPOSE_PROFILES=oracle docker compose config --no-interpolate --format json 2>/dev/null)" \
    || fail "prod+${compose_file} compose failed to render"

  "${CI_PYTHON_BIN}" - "${service}" "${merged_ap}" <<'PY'
import json, sys

service_name = sys.argv[1]
merged = json.loads(sys.argv[2])
service = merged.get("services", {}).get(service_name, {})
if "observability-traces" in merged.get("networks", {}):
    print(f"[FAIL] {service_name} AP topology must not declare the central-only observability-traces network")
    sys.exit(1)
if "observability-traces" in (service.get("networks", {}) or {}):
    print(f"[FAIL] {service_name} must not join observability-traces before secure Tailnet ingress exists")
    sys.exit(1)

forbidden_ports = {4317, 4318, 8888, 13133, 16685, 16686}
for port in service.get("ports", []) or []:
    if isinstance(port, dict):
        published = port.get("published")
        target = port.get("target")
        exposed = {int(value) for value in (published, target) if str(value).isdigit()}
    else:
        fields = str(port).split(":")
        exposed = {int(value.split("/")[0]) for value in fields if value.split("/")[0].isdigit()}
    blocked = exposed & forbidden_ports
    if blocked:
        print(f"[FAIL] {service_name} publishes forbidden Jaeger/OTLP port(s): {sorted(blocked)}")
        sys.exit(1)
PY
done <<'EOF'
youtube-collector-a docker-compose.osaka.yml
youtube-collector-b docker-compose.seoul.yml
youtube-collector-d docker-compose.osaka2.yml
EOF
pass "remote AP producers remain outside the trace network without Jaeger or OTLP host ports"
