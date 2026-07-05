#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# 렌더 전용 더미 env — 필수 보간 변수(:?)만 채운다. live 값과 무관.
STUB_COMPOSE_ENV="$(mktemp)"
STUB_AP_COMPOSE_ENV="$(mktemp)"
STUB_APP_ENV="$(mktemp)"
STUB_YOUTUBE_PRODUCER_ENV="$(mktemp)"
STUB_ADMIN_DASHBOARD_ENV="$(mktemp)"
cleanup() {
    rm -f "${STUB_COMPOSE_ENV}" "${STUB_AP_COMPOSE_ENV}" "${STUB_APP_ENV}" "${STUB_YOUTUBE_PRODUCER_ENV}" "${STUB_ADMIN_DASHBOARD_ENV}"
}
trap cleanup EXIT
cat >"${STUB_COMPOSE_ENV}" <<'EOF'
CACHE_PASSWORD=stub
DB_PASSWORD=stub
IRIS_BOT_TOKEN=stub
IRIS_WEBHOOK_TOKEN=stub
LIVE_LOGS_PATH=/srv/hololive-logs-stub
EOF
cat >"${STUB_AP_COMPOSE_ENV}" <<'EOF'
CACHE_PASSWORD=stub
DB_PASSWORD=stub
SEOUL_CACHE_HOST=stub
SEOUL_POSTGRES_HOST=stub
SEOUL_CLIPROXY_BASE_URL=https://cliproxy.invalid
SEOUL_METRICS_BIND_IP=100.100.1.5
EOF
cp "${STUB_COMPOSE_ENV}" "${STUB_APP_ENV}"
cat >>"${STUB_APP_ENV}" <<'EOF'
API_SECRET_KEY=stub
EOF
cat >"${STUB_YOUTUBE_PRODUCER_ENV}" <<'EOF'
API_SECRET_KEY=stub
HOLODEX_API_KEY=stub
HOLODEX_API_KEY_1=stub
HOLODEX_API_KEY_2=stub
HOLODEX_API_KEY_3=stub
HOLODEX_API_KEY_4=stub
HOLODEX_API_KEY_5=stub
SCRAPER_PROXY_ENABLED=false
SCRAPER_PROXY_URL=http://proxy.invalid
YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT=2026-04-10T01:11:12Z
YOUTUBE_ENABLE_QUOTA_BUILDING=true
EOF
cat >"${STUB_ADMIN_DASHBOARD_ENV}" <<'EOF'
ADMIN_PASS_HASH=stub
SESSION_SECRET=stub
VALKEY_URL=:stub@valkey-cache:6379
HOLO_BOT_API_KEY=stub
EOF

PROD_OVERLAYS=(
    -f deploy/compose/docker-compose.prod.yml
    -f deploy/compose/docker-compose.live-compat.yml
)
MAIN_AP_OVERLAYS=(
    "${PROD_OVERLAYS[@]}"
    -f deploy/compose/docker-compose.main-ap.yml
    -f deploy/compose/docker-compose.main-ap.live-compat.yml
)
renderable_ap_compose() {
    printf '%s\n' "$1"
}

render() {
    local profiles="$1"
    local compose_env_file="$2"
    shift
    shift
    COMPOSE_ENV_FILE="${compose_env_file}" \
        HOLOLIVE_API_ENV_FILE="${STUB_APP_ENV}" \
        HOLOLIVE_ALARM_WORKER_ENV_FILE="${STUB_APP_ENV}" \
        HOLOLIVE_YOUTUBE_PRODUCER_ENV_FILE="${STUB_YOUTUBE_PRODUCER_ENV}" \
        ADMIN_DASHBOARD_ENV_FILE="${STUB_ADMIN_DASHBOARD_ENV}" \
        COMPOSE_PROFILES="${profiles}" \
        "${ROOT_DIR}/scripts/deploy/compose.sh" "$@" config --format json
}

main_render="$(render oracle "${STUB_COMPOSE_ENV}" "${PROD_OVERLAYS[@]}")"
ap_render="$(render main-ap "${STUB_COMPOSE_ENV}" "${MAIN_AP_OVERLAYS[@]}")"
osaka_render="$(render oracle "${STUB_AP_COMPOSE_ENV}" -f deploy/compose/docker-compose.prod.yml -f "$(renderable_ap_compose deploy/compose/docker-compose.osaka.yml)")"
osaka2_render="$(render oracle "${STUB_AP_COMPOSE_ENV}" -f deploy/compose/docker-compose.prod.yml -f "$(renderable_ap_compose deploy/compose/docker-compose.osaka2.yml)")"
seoul_render="$(render oracle "${STUB_AP_COMPOSE_ENV}" -f deploy/compose/docker-compose.prod.yml -f "$(renderable_ap_compose deploy/compose/docker-compose.seoul.yml)")"

MAIN_RENDER="${main_render}" AP_RENDER="${ap_render}" \
    OSAKA_RENDER="${osaka_render}" OSAKA2_RENDER="${osaka2_render}" SEOUL_RENDER="${seoul_render}" python3 - <<'PY'
import json
import os
import sys

failures = []


def check(label, ok):
    if ok:
        print(f"[PASS] {label}")
    else:
        failures.append(label)
        print(f"[FAIL] {label}", file=sys.stderr)


def healthcheck_url(svc):
    test = (svc.get("healthcheck") or {}).get("test") or []
    return test[-1] if test else ""


def has_udp_published(svc, target_port):
    for p in svc.get("ports") or []:
        if str(p.get("target")) == str(target_port) and p.get("protocol") == "udp":
            return True
    return False


main = json.loads(os.environ["MAIN_RENDER"])["services"]
ap = json.loads(os.environ["AP_RENDER"])["services"]

H3_HEALTH = {
    "hololive-api": (
        [
            "https://127.0.0.1:30001/health",
            "https://127.0.0.1:30003/internal/ready",
            "https://127.0.0.1:30006/health",
        ],
        30001,
    ),
    "hololive-alarm-worker": (["https://127.0.0.1:30007/health"], None),
    "youtube-producer": (["https://127.0.0.1:30005/health"], None),
}

for name, (urls, udp_port) in H3_HEALTH.items():
    svc = main.get(name)
    check(f"{name} present in oracle render", svc is not None)
    if svc is None:
        continue
    test = (svc.get("healthcheck") or {}).get("test") or []
    for url in urls:
        check(f"{name} healthcheck includes {url}", url in test)
    if udp_port is not None:
        check(f"{name} publishes {udp_port}/udp", has_udp_published(svc, udp_port))

for name in ("hololive-api", "hololive-alarm-worker"):
    env = (main.get(name) or {}).get("environment") or {}
    check(f"{name} receives API_SECRET_KEY from scoped env_file", env.get("API_SECRET_KEY") == "stub")

admin_env = (main.get("admin-dashboard") or {}).get("environment") or {}
check("admin-dashboard receives ADMIN_PASS_HASH from scoped env_file", admin_env.get("ADMIN_PASS_HASH") == "stub")

def h3_addr_aligned(svc, port):
    return (svc.get("environment") or {}).get("HOLOLIVE_H3_ADDR") == f":{port}"


def metrics_addr_aligned(svc):
    return (svc.get("environment") or {}).get("HOLOLIVE_METRICS_ADDR") == ":30095"


def has_tcp_published(svc, target_port, published_port, host_ip=None):
    for p in svc.get("ports") or []:
        if (
            str(p.get("target")) == str(target_port)
            and str(p.get("published")) == str(published_port)
            and p.get("protocol", "tcp") == "tcp"
            and (host_ip is None or p.get("host_ip") == host_ip)
        ):
            return True
    return False


pc = ap.get("youtube-producer-c")
check("youtube-producer-c present in main-ap render", pc is not None)
if pc is not None:
    check(
        "youtube-producer-c healthcheck is https://127.0.0.1:30025/health",
        healthcheck_url(pc) == "https://127.0.0.1:30025/health",
    )
    check("youtube-producer-c publishes 30025/udp", has_udp_published(pc, 30025))
    check("youtube-producer-c HOLOLIVE_H3_ADDR is :30025", h3_addr_aligned(pc, 30025))
    check("youtube-producer-c HOLOLIVE_METRICS_ADDR is :30095", metrics_addr_aligned(pc))
    check("youtube-producer-c publishes metrics on 30095/tcp", has_tcp_published(pc, 30095, 30095))
    env = pc.get("environment") or {}
    check(
        "youtube-producer-c receives community shorts cutover from scoped producer env",
        env.get("YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT") == "2026-04-10T01:11:12Z",
    )
    for iris_key in ("IRIS_WEBHOOK_TOKEN", "IRIS_BOT_TOKEN"):
        check(f"youtube-producer-c does not receive {iris_key}", iris_key not in env)


def has_bind_target(svc, target):
    return any(v.get("target") == target for v in svc.get("volumes") or [])


AP_PRODUCERS = (
    ("OSAKA_RENDER", "youtube-producer-a", 30005, "100.100.1.6"),
    ("OSAKA2_RENDER", "youtube-producer-d", 30035, "100.100.1.2"),
    ("SEOUL_RENDER", "youtube-producer-b", 30015, "100.100.1.5"),
)

for render_env, name, port, metrics_host_ip in AP_PRODUCERS:
    services = json.loads(os.environ[render_env])["services"]
    svc = services.get(name)
    check(f"{name} present in {render_env}", svc is not None)
    if svc is None:
        continue
    url = f"https://127.0.0.1:{port}/health"
    check(f"{name} healthcheck is {url}", healthcheck_url(svc) == url)
    check(f"{name} publishes {port}/udp", has_udp_published(svc, port))
    check(f"{name} HOLOLIVE_H3_ADDR is :{port}", h3_addr_aligned(svc, port))
    check(f"{name} HOLOLIVE_METRICS_ADDR is :30095", metrics_addr_aligned(svc))
    check(
        f"{name} publishes metrics on {metrics_host_ip}:30095/tcp",
        has_tcp_published(svc, 30095, 30095, metrics_host_ip),
    )
    for cert_path in (
        "/run/hololive-bot/certs/hololive-h3.crt",
        "/run/hololive-bot/certs/hololive-h3.key",
    ):
        check(f"{name} mounts {cert_path}", has_bind_target(svc, cert_path))
    env = svc.get("environment") or {}
    for iris_key in ("IRIS_WEBHOOK_TOKEN", "IRIS_BOT_TOKEN"):
        check(f"{name} does not receive {iris_key}", iris_key not in env)

H2C_URL_PATTERNS = ("http://llm-scheduler", "http://hololive-admin-api")

for render_name, services in (("oracle", main), ("main-ap", ap)):
    offenders = [
        f"{name}.{key}={value}"
        for name, svc in services.items()
        for key, value in (svc.get("environment") or {}).items()
        if isinstance(value, str) and any(p in value for p in H2C_URL_PATTERNS)
    ]
    check(f"no h2c internal URLs in {render_name} render", not offenders)
    for offender in offenders:
        print(f"  offender: {offender}", file=sys.stderr)

if failures:
    print(f"[FAIL] h3 compose contract: {len(failures)} failure(s)", file=sys.stderr)
    sys.exit(1)
print("[PASS] h3 compose contract")
PY

AP_VERIFY_SCRIPTS=(
    scripts/deploy/ap-deploy.sh
    scripts/deploy/ap-completion-check.sh
    scripts/deploy/ap-rollback.sh
    scripts/logs/ap-smoke.sh
)
if grep -nE 'http://127\.0\.0\.1' "${AP_VERIFY_SCRIPTS[@]/#/${ROOT_DIR}/}"; then
    echo "[FAIL] AP verify scripts still probe over TCP http://" >&2
    exit 1
fi
echo "[PASS] AP verify scripts are h3-only"

if ! grep -Eq 'bin/healthcheck.*https://127\.0\.0\.1[^"]*/health' "${ROOT_DIR}/scripts/deploy/ap-rollback.sh"; then
    echo "[FAIL] ap-rollback.sh must verify AP rollback health via H3 ./bin/healthcheck" >&2
    exit 1
fi
echo "[PASS] ap-rollback.sh verifies rollback health via H3 healthcheck"

SMOKE_SCRIPT="${ROOT_DIR}/scripts/smoke/smoke-runtime-health.sh"
if grep -nE '(bot|admin-api|llm-scheduler|alarm-worker|alarm-worker-ready|youtube-producer-c)[^|]*\|http://127\.0\.0\.1:300(01|03|06|07|25)' "${SMOKE_SCRIPT}"; then
    echo "[FAIL] runtime smoke probes must use H3 healthcheck" >&2
    exit 1
fi
grep -Fq 'bot|https://127.0.0.1:30001/health|compose-healthcheck:hololive-api' "${SMOKE_SCRIPT}"
grep -Fq 'admin-api|https://127.0.0.1:30006/health|compose-healthcheck:hololive-api' "${SMOKE_SCRIPT}"
grep -Fq 'llm-scheduler|https://127.0.0.1:30003/health|compose-healthcheck:hololive-api' "${SMOKE_SCRIPT}"
grep -Fq 'alarm-worker|https://127.0.0.1:30007/health|compose-healthcheck:hololive-alarm-worker' "${SMOKE_SCRIPT}"
grep -Fq 'alarm-worker-ready|https://127.0.0.1:30007/ready|compose-healthcheck:hololive-alarm-worker' "${SMOKE_SCRIPT}"
grep -Fq 'youtube-producer-c|https://127.0.0.1:30025/health|compose-healthcheck:youtube-producer-c:main-ap' "${SMOKE_SCRIPT}"
echo "[PASS] runtime smoke probes are h3-only"
