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

ap_remote_bash "$AP_SMOKE_EXTERNAL" "$AP_COMPOSE_FILE" "$services_list" "$containers_list" "$ports_list" <<'REMOTE'
set -euo pipefail
ap_smoke_external="$1"
ap_compose_file="$2"
services_list="$3"
containers_list="$4"
ports_list="$5"
cd ~/hololive-bot

sudo -n test -r /run/hololive-bot/ap-compose.env
test -w /var/run/docker.sock || groups | grep -qw docker

read -r -a services <<< "$services_list"
read -r -a containers <<< "$containers_list"
read -r -a ports <<< "$ports_list"

idx=0
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle \
  ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f "$ap_compose_file" ps "${services[@]}"

for container in "${containers[@]}"; do
  ready="$(docker exec "$container" ./bin/healthcheck --body "https://127.0.0.1:${ports[$idx]}/ready")"
  printf "%s" "$ready" | grep -q "\"status\":\"ready\""
  docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$container" | grep -qx 'YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED=true'
  docker exec "$container" ./bin/healthcheck "https://127.0.0.1:${ports[$idx]}/health"
  idx=$((idx + 1))
done

if [[ "${ap_smoke_external}" == "true" ]]; then
  for container in "${containers[@]}"; do
    docker exec "$container" ./bin/healthcheck --smoke
  done
else
  echo "external smoke skipped (set AP_SMOKE_EXTERNAL=true to run healthcheck --smoke)"
fi

docker inspect "${containers[@]}" --format "{{.Name}} {{json .Config.Healthcheck.Test}} {{.Config.User}} {{.Config.Image}}"
REMOTE
