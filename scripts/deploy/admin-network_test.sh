#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf -- "$fixture"' EXIT
docker compose -f "${root}/deploy/compose/docker-compose.prod.yml" \
    -f "${root}/deploy/compose/docker-compose.live-compat.yml" \
    config --no-interpolate --no-env-resolution --format json > "$fixture/compose.json"
jq -e '
    def envmap: if type == "array" then map(capture("^(?<key>[^=]+)=(?<value>.*)$")) | from_entries else . end;
    . as $compose |
    .services["hololive-api"] as $api |
    ($api.environment | envmap) as $env |
    ($api.networks | keys) as $api_networks |
    all($api_networks[]; ($compose.networks[.].external // false) == false) and
    ($env.ADMIN_ALLOWED_IPS == "${ADMIN_ALLOWED_IPS:-127.0.0.1/32,::1/128,100.100.1.5/32}") and
    ($env.HOLOLIVE_OTLP_GRPC_ENDPOINT | startswith("${HOLOLIVE_OTLP_GRPC_ENDPOINT:?")) and
    ([$api.ports[] | select(test("(^|:)(30003|30006)(/(tcp|udp))?$"))] |
        length == 4 and all(.[]; startswith("127.0.0.1:"))) and
    ($compose.services["valkey-cache"].ports | length == 1 and
        all(.[]; .host_ip == "127.0.0.1" and .target == 6379 and .protocol == "tcp")) and
    ($compose.services["hololive-alarm-worker"].networks | has("observability-traces"))
' "$fixture/compose.json" >/dev/null
echo 'admin API is isolated from external observability networks and keeps loopback listeners'
