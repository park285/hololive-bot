#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PROD_FILE="${ROOT_DIR}/deploy/compose/docker-compose.prod.yml"
ACTIVE_COMPOSE_FILES=(
    "${PROD_FILE}"
    "${ROOT_DIR}/deploy/compose/docker-compose.live-compat.yml"
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

service_block() {
    local service="$1"
    awk -v service="${service}" '
        $0 == "  " service ":" { in_service = 1; next }
        in_service && /^  [A-Za-z0-9_.-]+:[[:space:]]*$/ { exit }
        in_service { print }
    ' "${PROD_FILE}"
}

for file in "${ACTIVE_COMPOSE_FILES[@]}"; do
    [[ -r "${file}" ]] || fail "active Compose file is missing: ${file}"
done

mapfile -t prod_services < <(list_services "${PROD_FILE}")
for expected in hololive-api hololive-alarm-worker youtube-collector; do
    printf '%s\n' "${prod_services[@]}" | grep -Fxq "${expected}" \
        || fail "production Compose is missing runtime service: ${expected}"
done
pass "production Compose defines all three application runtimes"

grep -Eq 'dockerfile:[[:space:]]*hololive/hololive-api/Dockerfile' "${PROD_FILE}" \
    || fail "production Compose does not build the unified hololive-api image"
pass "production image build contract targets hololive-api only"

grep -Fq 'ALARM_INTERNAL_URL: https://hololive-alarm-worker:30007' "${PROD_FILE}" \
    || fail "hololive-api does not target alarm-worker as the alarm provider"
pass "the business API targets alarm-worker as the alarm provider"

# llm plane(30003)은 container 안 healthcheck만 쓰므로 host에 publish하지 않는다.
for port in 30001 30006; do
    grep -Fq "127.0.0.1:${port}:${port}" "${PROD_FILE}" \
        || fail "hololive-api listener ${port} is not published"
done
pass "hololive-api publishes the bot and admin listener ports"

service_block hololive-alarm-worker | grep -Fq 'NOTIFICATION_EGRESS_ROLE: "owner"' \
    || fail "alarm-worker is not configured as proactive egress owner"
service_block hololive-api | grep -Fq 'NOTIFICATION_EGRESS_ROLE: "producer"' \
    || fail "hololive-api is not configured as notification producer"
pass "egress ownership remains isolated to alarm-worker"
