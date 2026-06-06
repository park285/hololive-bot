#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
CHANGE_STARTED_AT="${CHANGE_STARTED_AT:-}"

. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"
ap_host_load "$REPO_ROOT" "${1:-}"

if [[ -n "$CHANGE_STARTED_AT" && ! "$CHANGE_STARTED_AT" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]; then
  echo "CHANGE_STARTED_AT must be UTC RFC3339 seconds, for example 2026-05-19T18:28:03Z" >&2
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
sudo -n test -r /run/hololive-bot/env
test -w /var/run/docker.sock || groups | grep -qw docker
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f '$AP_COMPOSE_FILE' ps $services_list

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
