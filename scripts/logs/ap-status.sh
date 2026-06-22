#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
LOG_SINCE="${LOG_SINCE:-10m}"
LOG_TAIL="${LOG_TAIL:-400}"

. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"
ap_host_load "$REPO_ROOT" "${1:-}"

signals() {
  local container="$1"
  local pattern="$2"
  ap_remote_bash "$container" "$pattern" "$LOG_SINCE" "$LOG_TAIL" <<'REMOTE'
set -euo pipefail
container="$1"; pattern="$2"; since="$3"; tail_n="$4"
if ! docker inspect "$container" >/dev/null 2>&1; then
  echo "($container not present)"
  exit 0
fi
hits=$(docker logs --since "$since" "$container" 2>&1 | grep -E "$pattern" || true)
if [ -n "$hits" ]; then
  printf '%s\n' "$hits"
else
  docker logs --tail "$tail_n" "$container" 2>&1 | grep -E "$pattern" || true
fi
REMOTE
}

services_list="${AP_SERVICES[*]}"
containers_list="${AP_CONTAINERS[*]}"
ports_list="${AP_PORTS[*]}"

echo "== $AP_NAME compose services =="
ap_remote_bash "$AP_COMPOSE_FILE" "$services_list" <<'REMOTE'
set -euo pipefail
compose_file="$1"
services_list="$2"
read -r -a services <<< "$services_list"
cd ~/hololive-bot
sudo -n test -r /run/hololive-bot/ap-compose.env
test -w /var/run/docker.sock || groups | grep -qw docker
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle \
  ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f "$compose_file" \
  ps --format 'table {{.Name}}\t{{.Service}}\t{{.Status}}\t{{.Ports}}' "${services[@]}"
REMOTE

echo
echo "== Health =="
ap_remote_bash "$services_list" "$containers_list" "$ports_list" <<'REMOTE'
set -euo pipefail
services_list="$1"
containers_list="$2"
ports_list="$3"
read -r -a services <<< "$services_list"
read -r -a containers <<< "$containers_list"
read -r -a ports <<< "$ports_list"
for i in "${!services[@]}"; do
  printf "%s: " "${services[$i]}"
  docker exec "${containers[$i]}" ./bin/healthcheck --body "https://127.0.0.1:${ports[$i]}/health"
  printf "\n"
done
REMOTE

echo
echo "== Runtime directory =="
ap_remote_bash <<'REMOTE'
set -euo pipefail
cd ~/hololive-bot
find . -maxdepth 1 -mindepth 1 -printf "%f\n" | sort
REMOTE

echo
echo "== Recent youtube-producer signals =="
for container in "${AP_CONTAINERS[@]}"; do
  signals "$container" 'Cache store connected|postgres_pool_connected|job_claim|Photo sync service started|Photo sync completed|ERR|pre-send claim|ingestion_lease_lost|panic|permission denied'
done

echo
echo "== Recent alarm-worker egress signals =="
signals hololive-alarm-worker 'YouTube outbox dispatcher started by alarm-worker|Outbox dispatcher started|Outbox per-room enqueue completed|Outbox per-room dispatch completed|ERR|panic|permission denied'

echo
echo "== Unused split-host volumes =="
ap_remote_bash <<'REMOTE'
set -euo pipefail
docker volume ls --format "{{.Name}}" | grep "^hololive-bot_" || true
REMOTE
