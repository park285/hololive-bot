#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
AP_SMOKE_EXTERNAL="${AP_SMOKE_EXTERNAL:-false}"
case "$AP_SMOKE_EXTERNAL" in
  true|false) ;;
  *)
    echo "AP_SMOKE_EXTERNAL must be true or false" >&2
    exit 2
    ;;
esac

. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"
ap_host_load "$REPO_ROOT" "${1:-}"

services_list="${AP_SERVICES[*]}"
containers_list="${AP_CONTAINERS[*]}"
ports_list="${AP_PORTS[*]}"

"${AP_SSH[@]}" "AP_SMOKE_EXTERNAL='$AP_SMOKE_EXTERNAL' AP_COMPOSE_FILE='$AP_COMPOSE_FILE' AP_SERVICES_LIST='$services_list' AP_CONTAINERS_LIST='$containers_list' AP_PORTS_LIST='$ports_list' bash -s" <<'REMOTE'
set -euo pipefail
cd ~/hololive-bot

sudo -n test -r /run/hololive-bot/ap-compose.env
test -w /var/run/docker.sock || groups | grep -qw docker

sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f "$AP_COMPOSE_FILE" ps $AP_SERVICES_LIST

ports=($AP_PORTS_LIST)
idx=0
for container in $AP_CONTAINERS_LIST; do
  ready="$(docker exec "$container" ./bin/healthcheck --body "https://127.0.0.1:${ports[$idx]}/ready")"
  printf "%s" "$ready" | grep -q "\"mode\":\"active-active\""
  printf "%s" "$ready" | grep -q "\"valkey_available\":true"
  printf "%s" "$ready" | grep -q "\"scraping_paused\":false"
  docker exec "$container" ./bin/healthcheck "https://127.0.0.1:${ports[$idx]}/health"
  idx=$((idx + 1))
done

if [[ "${AP_SMOKE_EXTERNAL}" == "true" ]]; then
  for container in $AP_CONTAINERS_LIST; do
    docker exec "$container" ./bin/healthcheck --smoke
  done
else
  echo "external smoke skipped (set AP_SMOKE_EXTERNAL=true to run healthcheck --smoke)"
fi

docker inspect $AP_CONTAINERS_LIST --format "{{.Name}} {{json .Config.Healthcheck.Test}} {{.Config.User}} {{.Config.Image}}"
REMOTE
