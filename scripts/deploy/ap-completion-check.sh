#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
CHANGE_STARTED_AT="${CHANGE_STARTED_AT:-}"
AP_REQUIRED_UDP_BUFFER_BYTES="${AP_REQUIRED_UDP_BUFFER_BYTES:-7500000}"

. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"
ap_host_load "$REPO_ROOT" "${1:-}"

if [[ -n "$CHANGE_STARTED_AT" && ! "$CHANGE_STARTED_AT" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]; then
  echo "CHANGE_STARTED_AT must be UTC RFC3339 seconds, for example 2026-05-19T18:28:03Z" >&2
  exit 2
fi
if [[ ! "$AP_REQUIRED_UDP_BUFFER_BYTES" =~ ^[0-9]+$ ]]; then
  echo "AP_REQUIRED_UDP_BUFFER_BYTES must be an integer" >&2
  exit 2
fi

remote() {
  "${AP_SSH[@]}" "$@"
}

services_list="${AP_SERVICES[*]}"
containers_list="${AP_CONTAINERS[*]}"
ports_list="${AP_PORTS[*]}"

remote "set -euo pipefail
cd ~/hololive-bot
required_udp_buffer_bytes='$AP_REQUIRED_UDP_BUFFER_BYTES'
rmem_max=\$(sysctl -n net.core.rmem_max 2>/dev/null || echo 0)
wmem_max=\$(sysctl -n net.core.wmem_max 2>/dev/null || echo 0)
if (( rmem_max < required_udp_buffer_bytes || wmem_max < required_udp_buffer_bytes )); then
  echo \"AP QUIC UDP buffers too small on $AP_NAME: net.core.rmem_max=\$rmem_max net.core.wmem_max=\$wmem_max required>=\$required_udp_buffer_bytes\" >&2
  exit 1
fi
sudo -n test -r /run/hololive-bot/ap-compose.env
sudo -n test -r /run/hololive-bot/youtube-producer.env
test -w /var/run/docker.sock || groups | grep -qw docker
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f '$AP_COMPOSE_FILE' ps $services_list

for container in $containers_list; do
  docker inspect \"\$container\" >/dev/null
  status=\$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' \"\$container\")
  [[ \"\$status\" == healthy ]]
done

ports=($ports_list)
idx=0
for container in $containers_list; do
  ready=\$(docker exec \"\$container\" ./bin/healthcheck --body \"https://127.0.0.1:\${ports[\$idx]}/ready\")
  printf '%s' \"\$ready\" | grep -q '\"mode\":\"active-active\"'
  printf '%s' \"\$ready\" | grep -q '\"valkey_available\":true'
  printf '%s' \"\$ready\" | grep -q '\"scraping_paused\":false'
  idx=\$((idx + 1))
done

if [[ -n '$CHANGE_STARTED_AT' ]]; then
  since_epoch=\$(date -u -d '$CHANGE_STARTED_AT' +%s)
  for container in $containers_list; do
    started_at=\$(docker inspect -f '{{.State.StartedAt}}' \"\$container\")
    started_epoch=\$(date -u -d \"\$started_at\" +%s)
    [[ \"\$started_epoch\" -ge \"\$since_epoch\" ]]
    if docker logs --since '$CHANGE_STARTED_AT' \"\$container\" 2>&1 | grep -E 'ERR|panic|permission denied|x509|no such file'; then
      exit 1
    fi
  done
fi

echo 'active-active completion check passed'
"
