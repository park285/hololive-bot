#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# 렌더 전용 더미 env — 필수 보간 변수(:?)만 채운다. live 값과 무관.
STUB_ENV="$(mktemp)"
trap 'rm -f "${STUB_ENV}"' EXIT
cat >"${STUB_ENV}" <<'EOF'
ADMIN_PASS_BCRYPT=stub
CACHE_PASSWORD=stub
DB_PASSWORD=stub
IRIS_BOT_TOKEN=stub
IRIS_WEBHOOK_TOKEN=stub
SESSION_SECRET=stub
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

render() {
    local profiles="$1"
    shift
    COMPOSE_ENV_FILE="${STUB_ENV}" COMPOSE_PROFILES="${profiles}" \
        "${ROOT_DIR}/scripts/deploy/compose.sh" "$@" config --format json
}

main_render="$(render oracle "${PROD_OVERLAYS[@]}")"
ap_render="$(render main-ap "${MAIN_AP_OVERLAYS[@]}")"

MAIN_RENDER="${main_render}" AP_RENDER="${ap_render}" python3 - <<'PY'
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
    "hololive-bot": ("https://127.0.0.1:30001/health", 30001),
    "hololive-admin-api": ("https://127.0.0.1:30006/health", None),
    "hololive-alarm-worker": ("https://127.0.0.1:30007/health", None),
    "youtube-producer": ("https://127.0.0.1:30005/health", None),
    "llm-scheduler": ("https://127.0.0.1:30003/health", None),
}

for name, (url, udp_port) in H3_HEALTH.items():
    svc = main.get(name)
    check(f"{name} present in oracle render", svc is not None)
    if svc is None:
        continue
    check(f"{name} healthcheck is {url}", healthcheck_url(svc) == url)
    if udp_port is not None:
        check(f"{name} publishes {udp_port}/udp", has_udp_published(svc, udp_port))

pc = ap.get("youtube-producer-c")
check("youtube-producer-c present in main-ap render", pc is not None)
if pc is not None:
    check(
        "youtube-producer-c healthcheck is https://127.0.0.1:30025/health",
        healthcheck_url(pc) == "https://127.0.0.1:30025/health",
    )
    check("youtube-producer-c publishes 30025/udp", has_udp_published(pc, 30025))

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
