#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "${ROOT_DIR}/scripts/ci/python-runtime.sh"
repo_python_init

fail() {
    echo "[FAIL] $*" >&2
    exit 1
}

pass() {
    echo "[PASS] $*"
}

if ! command -v docker >/dev/null 2>&1 || ! docker compose version >/dev/null 2>&1; then
    fail "docker compose is required for Compose endpoint regression coverage"
fi

endpoint_root="$(mktemp -d)"
for endpoint_name in app alarm-worker youtube-collector admin-dashboard; do
    : >"${endpoint_root}/${endpoint_name}.env"
done
cat >"${endpoint_root}/override.env" <<EOF
CACHE_PASSWORD=fixture
DB_PASSWORD=fixture
HOLO_API_VERSION=fixture
HOLO_ALARM_WORKER_VERSION=fixture
LIVE_LOGS_PATH=/srv/hololive-logs-fixture
HOLOLIVE_API_ENV_FILE=${endpoint_root}/app.env
HOLOLIVE_ALARM_WORKER_ENV_FILE=${endpoint_root}/alarm-worker.env
HOLOLIVE_YOUTUBE_COLLECTOR_ENV_FILE=${endpoint_root}/youtube-collector.env
ADMIN_DASHBOARD_ENV_FILE=${endpoint_root}/admin-dashboard.env
HOLOLIVE_CENTRAL_CACHE_HOST=cache.service.fixture
HOLOLIVE_CENTRAL_POSTGRES_HOST=postgres.service.fixture
HOLOLIVE_CENTRAL_POSTGRES_PORT=15432
EOF
cp "${endpoint_root}/override.env" "${endpoint_root}/default.env"
sed -i '/HOLOLIVE_CENTRAL_/d' "${endpoint_root}/default.env"
cp "${endpoint_root}/override.env" "${endpoint_root}/ap-default.env"
sed -i '/HOLOLIVE_CENTRAL_POSTGRES_PORT/d' "${endpoint_root}/ap-default.env"
prod=(-f deploy/compose/docker-compose.prod.yml)
live=("${prod[@]}" -f deploy/compose/docker-compose.live-compat.yml)
main=("${live[@]}" -f deploy/compose/docker-compose.main-ap.yml -f deploy/compose/docker-compose.main-ap.live-compat.yml)
render() {
    local profile="$1" env_file="$2" output="$3"
    shift 3
    (cd "${ROOT_DIR}" && env -u HOLOLIVE_CENTRAL_CACHE_HOST -u HOLOLIVE_CENTRAL_POSTGRES_HOST -u HOLOLIVE_CENTRAL_POSTGRES_PORT COMPOSE_PROFILES="${profile}" docker compose --env-file "${env_file}" "$@" config --format json >"${output}")
}
quiet() {
    local profile="$1" env_file="$2"
    shift 2
    (cd "${ROOT_DIR}" && env -u HOLOLIVE_CENTRAL_CACHE_HOST -u HOLOLIVE_CENTRAL_POSTGRES_HOST -u HOLOLIVE_CENTRAL_POSTGRES_PORT COMPOSE_PROFILES="${profile}" docker compose --env-file "${env_file}" "$@" config --quiet)
}
render default "${endpoint_root}/default.env" "${endpoint_root}/default.json" "${prod[@]}"
render default "${endpoint_root}/default.env" "${endpoint_root}/default-live.json" "${live[@]}"
render main-ap "${endpoint_root}/default.env" "${endpoint_root}/default-main.json" "${main[@]}"
render oracle "${endpoint_root}/override.env" "${endpoint_root}/central.json" "${prod[@]}"
render oracle "${endpoint_root}/override.env" "${endpoint_root}/live.json" "${live[@]}"
render main-ap "${endpoint_root}/override.env" "${endpoint_root}/main.json" "${main[@]}"
for endpoint_spec in "a deploy/compose/docker-compose.osaka.yml" "b deploy/compose/docker-compose.seoul.yml" "d deploy/compose/docker-compose.osaka2.yml"; do
    set -- ${endpoint_spec}
    render oracle "${endpoint_root}/override.env" "${endpoint_root}/${1}.json" "${prod[@]}" -f "${2}"
    quiet oracle "${endpoint_root}/override.env" "${prod[@]}" -f "${2}"
done
render oracle "${endpoint_root}/ap-default.env" "${endpoint_root}/ap-default.json" "${prod[@]}" -f deploy/compose/docker-compose.osaka.yml
cp "${endpoint_root}/override.env" "${endpoint_root}/ap-nocache.env"
sed -i '/HOLOLIVE_CENTRAL_CACHE_HOST/d' "${endpoint_root}/ap-nocache.env"
quiet oracle "${endpoint_root}/ap-nocache.env" "${prod[@]}" -f deploy/compose/docker-compose.osaka.yml
quiet default "${endpoint_root}/default.env" "${prod[@]}"
quiet oracle "${endpoint_root}/override.env" "${prod[@]}"
quiet main-ap "${endpoint_root}/override.env" "${main[@]}"

"${CI_PYTHON_BIN}" - "${endpoint_root}/default.json" "${endpoint_root}/default-live.json" "${endpoint_root}/default-main.json" "${endpoint_root}/central.json" "${endpoint_root}/live.json" "${endpoint_root}/main.json" "${endpoint_root}/a.json" "${endpoint_root}/b.json" "${endpoint_root}/d.json" "${endpoint_root}/ap-default.json" <<'PY'
import json
import sys
def load(path):
    with open(path, encoding="utf-8") as handle:
        return json.load(handle)["services"]
def env(services, name):
    return services[name].get("environment") or {}
def check(services, name, host, port, keys, label):
    values = env(services, name)
    if (values.get(keys[0]), values.get(keys[1])) != (host, port):
        raise SystemExit(f"[FAIL] {label}: got {values.get(keys[0])}:{values.get(keys[1])}")
    print(f"[PASS] {label}")
def check_dns(services, name, label):
    resolvers = services[name].get("dns") or []
    if not resolvers or resolvers[0] != "100.100.100.100":
        raise SystemExit(f"[FAIL] {label}: got {resolvers}")
    print(f"[PASS] {label}")
def check_no_cache(services, name, label):
    values = env(services, name)
    cache_keys = sorted(key for key in values if key.startswith("CACHE_"))
    if cache_keys:
        raise SystemExit(f"[FAIL] {label}: CACHE env {cache_keys}")
    depends = services[name].get("depends_on") or {}
    if "valkey-cache" in depends:
        raise SystemExit(f"[FAIL] {label}: depends_on valkey-cache")
    for volume in services[name].get("volumes") or []:
        if isinstance(volume, dict):
            source = str(volume.get("source") or "")
            target = str(volume.get("target") or "")
        else:
            source = str(volume)
            target = str(volume)
        if source == "valkey-cache-socket" or target == "/var/run/valkey" or "valkey" in source or "valkey" in target:
            raise SystemExit(f"[FAIL] {label}: Valkey mount {source}->{target}")
    print(f"[PASS] {label}")
def check_no_scraper_poll(services, name, label):
    values = env(services, name)
    scraper_keys = sorted(key for key in values if key.startswith("SCRAPER_POLL_") or key == "SCRAPER_FETCHER_ENGINE")
    if scraper_keys:
        raise SystemExit(f"[FAIL] {label}: scraper env {scraper_keys}")
    print(f"[PASS] {label}")
default, live_default, main_default, central, live, main, osaka, seoul, osaka2, ap_default = map(load, sys.argv[1:])
for args in [
    (default, "hololive-api", "holo-postgres", "5432", ("POSTGRES_HOST", "POSTGRES_PORT"), "central API default"),
    (default, "hololive-alarm-worker", "holo-postgres", "5432", ("POSTGRES_HOST", "POSTGRES_PORT"), "central worker default"),
    (default, "youtube-collector", "holo-postgres", "5432", ("POSTGRES_HOST", "POSTGRES_PORT"), "central collector default"),
    (default, "hololive-db-migrate", "holo-postgres", "5432", ("PGHOST", "PGPORT"), "central migrate default"),
    (live_default, "hololive-api", "holo-postgres", "5432", ("POSTGRES_HOST", "POSTGRES_PORT"), "live API default"),
    (live_default, "youtube-collector", "holo-postgres", "5432", ("POSTGRES_HOST", "POSTGRES_PORT"), "live collector default"),
    (live_default, "hololive-db-migrate", "holo-postgres", "5432", ("PGHOST", "PGPORT"), "live migrate default"),
    (main_default, "youtube-collector", "holo-postgres", "5432", ("POSTGRES_HOST", "POSTGRES_PORT"), "main collector-c default"),
]:
    check(*args)
expected = ("postgres.service.fixture", "15432")
for args in [
    (central, "hololive-api", *expected, ("POSTGRES_HOST", "POSTGRES_PORT"), "central API override"),
    (central, "hololive-alarm-worker", *expected, ("POSTGRES_HOST", "POSTGRES_PORT"), "central worker override"),
    (central, "youtube-collector", *expected, ("POSTGRES_HOST", "POSTGRES_PORT"), "central collector override"),
    (central, "hololive-db-migrate", *expected, ("PGHOST", "PGPORT"), "central migrate override"),
    (live, "hololive-api", *expected, ("POSTGRES_HOST", "POSTGRES_PORT"), "live API override"),
    (live, "youtube-collector", *expected, ("POSTGRES_HOST", "POSTGRES_PORT"), "live collector override"),
    (live, "hololive-db-migrate", *expected, ("PGHOST", "PGPORT"), "live migrate override"),
    (main, "youtube-collector", *expected, ("POSTGRES_HOST", "POSTGRES_PORT"), "main collector-c override"),
    (osaka, "youtube-collector-a", *expected, ("POSTGRES_HOST", "POSTGRES_PORT"), "Osaka producer-a override"),
    (seoul, "youtube-collector-b", *expected, ("POSTGRES_HOST", "POSTGRES_PORT"), "Seoul producer-b override"),
    (osaka2, "youtube-collector-d", *expected, ("POSTGRES_HOST", "POSTGRES_PORT"), "Osaka2 producer-d override"),
]:
    check(*args)
for services, name, label in [
    (central, "hololive-api", "central API uses tailnet DNS"),
    (central, "hololive-alarm-worker", "central worker uses tailnet DNS"),
    (central, "youtube-collector", "central collector uses tailnet DNS"),
    (central, "hololive-db-migrate", "central migrate uses tailnet DNS"),
    (main, "youtube-collector", "main collector-c uses tailnet DNS"),
    (osaka, "youtube-collector-a", "Osaka producer-a uses tailnet DNS"),
    (seoul, "youtube-collector-b", "Seoul producer-b uses tailnet DNS"),
    (osaka2, "youtube-collector-d", "Osaka2 producer-d uses tailnet DNS"),
]:
    check_dns(services, name, label)
check(ap_default, "youtube-collector-a", "postgres.service.fixture", "5433", ("POSTGRES_HOST", "POSTGRES_PORT"), "AP port default")
for services, name, label in [
    (default, "youtube-collector", "central collector default has no Valkey"),
    (live_default, "youtube-collector", "live collector default has no Valkey"),
    (main_default, "youtube-collector", "main collector-c default has no Valkey"),
    (central, "youtube-collector", "central collector override has no Valkey"),
    (live, "youtube-collector", "live collector override has no Valkey"),
    (main, "youtube-collector", "main collector-c override has no Valkey"),
    (osaka, "youtube-collector-a", "Osaka collector-a has no Valkey"),
    (seoul, "youtube-collector-b", "Seoul collector-b has no Valkey"),
    (osaka2, "youtube-collector-d", "Osaka2 collector-d has no Valkey"),
    (ap_default, "youtube-collector-a", "AP port-default collector-a has no Valkey"),
]:
    check_no_cache(services, name, label)
    check_no_scraper_poll(services, name, label.replace("no Valkey", "has no SCRAPER_POLL_/SCRAPER_FETCHER_ENGINE"))
postgres = env(central, "holo-postgres")
healthcheck = str((central["holo-postgres"].get("healthcheck") or {}).get("test") or [])
if postgres.get("PGPORT") != "5432" or "-p 5432" not in healthcheck:
    raise SystemExit("[FAIL] PostgreSQL internals must remain on container port 5432")
print("[PASS] PostgreSQL internals remain on container port 5432")
PY
pass "central and AP PostgreSQL endpoints share the overridden stable route"
rm -rf "${endpoint_root}"
