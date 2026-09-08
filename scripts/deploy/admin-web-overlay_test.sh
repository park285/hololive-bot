#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
fixture_dir="$(mktemp -d)"
trap 'rm -rf -- "$fixture_dir"' EXIT
cat > "$fixture_dir/compose.yml" <<'YAML'
services:
  hololive-api:
    image: example.invalid/hololive-api:fixture
    ports:
      - "127.0.0.1:30006:30006"
      - "127.0.0.1:30006:30006/udp"
YAML
overlay="$repo_root/deploy/compose/docker-compose.admin-web.yml"
unset HOLOLIVE_ADMIN_API_PORT_BIND_IP
if docker compose -f "$fixture_dir/compose.yml" -f "$overlay" config --quiet > "$fixture_dir/missing.log" 2>&1; then
  echo 'admin-web overlay accepted a missing bind address' >&2
  exit 1
fi
export HOLOLIVE_ADMIN_API_PORT_BIND_IP=100.64.0.10
docker compose -f "$fixture_dir/compose.yml" -f "$overlay" config --format json > "$fixture_dir/config.json"
jq -e '.services["hololive-api"].ports | length == 3 and
  any(.[]; .host_ip == "127.0.0.1" and .protocol == "tcp" and .target == 30006) and
  any(.[]; .host_ip == "127.0.0.1" and .protocol == "udp" and .target == 30006) and
  any(.[]; .host_ip == "100.64.0.10" and .protocol == "udp" and .target == 30006) and
  all(.[]; .host_ip != "0.0.0.0")' "$fixture_dir/config.json" > /dev/null
echo 'admin-web overlay preserves loopback and adds only the configured H3 port'
