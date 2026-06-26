#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PROD_FILE="${ROOT_DIR}/deploy/compose/docker-compose.prod.yml"
ACTIVE_COMPOSE_FILES=(
    "${PROD_FILE}"
    "${ROOT_DIR}/deploy/compose/docker-compose.live-compat.yml"
    "${ROOT_DIR}/deploy/compose/docker-compose.main-ap.yml"
    "${ROOT_DIR}/deploy/compose/docker-compose.main-ap.live-compat.yml"
    "${ROOT_DIR}/deploy/compose/docker-compose.osaka.yml"
    "${ROOT_DIR}/deploy/compose/docker-compose.osaka2.yml"
    "${ROOT_DIR}/deploy/compose/docker-compose.seoul.yml"
    "${ROOT_DIR}/deploy/compose/docker-compose.remote-cache.yml"
)

fail() {
    echo "[FAIL] $*" >&2
    exit 1
}

pass() {
    echo "[PASS] $*"
}

list_services() {
    awk '
        $0 == "services:" { in_services = 1; next }
        in_services && /^[^[:space:]]/ { exit }
        in_services && /^  [A-Za-z0-9_.-]+:[[:space:]]*$/ {
            line = $0
            sub(/^  /, "", line)
            sub(/:[[:space:]]*$/, "", line)
            print line
        }
    ' "$1"
}

for file in "${ACTIVE_COMPOSE_FILES[@]}"; do
    [[ -r "${file}" ]] || fail "active Compose file is missing: ${file}"
done

mapfile -t prod_services < <(list_services "${PROD_FILE}")
for expected in hololive-api hololive-alarm-worker youtube-producer; do
    printf '%s\n' "${prod_services[@]}" | grep -Fxq "${expected}" \
        || fail "production Compose is missing runtime service: ${expected}"
done
pass "production Compose defines all three application runtimes"

for removed in hololive-bot hololive-admin-api llm-scheduler; do
    for file in "${ACTIVE_COMPOSE_FILES[@]}"; do
        if list_services "${file}" | grep -Fxq "${removed}"; then
            fail "active Compose file still defines retired service ${removed}: ${file}"
        fi
    done
done
pass "active Compose files do not reintroduce retired services"

if grep -En 'container_name:[[:space:]]*(hololive-kakao-bot-go|hololive-admin-api|hololive-llm-scheduler)([[:space:]]|$)' "${ACTIVE_COMPOSE_FILES[@]}"; then
    fail "active Compose files still declare retired runtime containers"
fi
pass "active Compose files do not declare retired runtime containers"

if grep -En '^[[:space:]]*-[[:space:]]*(hololive-bot|hololive-admin-api|llm-scheduler)[[:space:]]*$' "${ACTIVE_COMPOSE_FILES[@]}"; then
    fail "active Compose files still declare retired network aliases"
fi
pass "active Compose files do not declare retired network aliases"

if grep -Eq 'dockerfile:[[:space:]]*hololive/(hololive-kakao-bot-go|hololive-admin-api|hololive-llm-sched)/Dockerfile' "${PROD_FILE}"; then
    fail "production Compose still builds a retired runtime image"
fi
grep -Eq 'dockerfile:[[:space:]]*hololive/hololive-api/Dockerfile' "${PROD_FILE}" \
    || fail "production Compose does not build the unified hololive-api image"
pass "production image build contract targets hololive-api only"

grep -Fq 'ALARM_INTERNAL_URL: https://hololive-alarm-worker:30007' "${PROD_FILE}" \
    || fail "hololive-api does not target alarm-worker as the alarm provider"
grep -Fq 'HOLO_ADMIN_API_URL: https://hololive-api:30006' "${PROD_FILE}" \
    || fail "admin-dashboard does not target the unified admin plane"
grep -Fq 'HOLO_BOT_URL: https://hololive-api:30006' "${PROD_FILE}" \
    || fail "admin-dashboard secondary API URL does not target hololive-api"
pass "provider and dashboard URLs target the three-runtime topology"

for port in 30001 30003 30006; do
    grep -Fq "127.0.0.1:${port}:${port}" "${PROD_FILE}" \
        || fail "hololive-api compatibility listener ${port} is not published"
done
pass "hololive-api preserves the three listener ports in one service"

grep -Fq 'NOTIFICATION_EGRESS_ROLE: "owner"' "${PROD_FILE}" \
    || fail "alarm-worker is not configured as proactive egress owner"
grep -Fq 'YOUTUBE_OUTBOX_DISPATCHER_ENABLED: "false"' "${PROD_FILE}" \
    || fail "youtube-producer is not configured as producer-only"
pass "egress ownership remains isolated to alarm-worker"
