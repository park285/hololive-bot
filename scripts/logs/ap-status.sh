#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
LOG_SINCE="${LOG_SINCE:-10m}"
LOG_TAIL="${LOG_TAIL:-400}"

. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"
ap_host_load "$REPO_ROOT" "${1:-}"

remote() {
  "${AP_SSH[@]}" "$@"
}

signals() {
  local container="$1"
  local pattern="$2"
  remote "docker inspect '$container' >/dev/null 2>&1 || { echo '($container not present)'; exit 0; }; hits=\$(docker logs --since '$LOG_SINCE' '$container' 2>&1 | grep -E '$pattern' || true); if [ -n \"\$hits\" ]; then printf '%s\n' \"\$hits\"; else docker logs --tail '$LOG_TAIL' '$container' 2>&1 | grep -E '$pattern' || true; fi"
}

services_list="${AP_SERVICES[*]}"
ports_list="${AP_PORTS[*]}"

echo "== $AP_NAME compose services =="
remote "cd ~/hololive-bot && sudo -n test -r /run/hololive-bot/env && (test -w /var/run/docker.sock || groups | grep -qw docker) && sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f '$AP_COMPOSE_FILE' ps --format 'table {{.Name}}\t{{.Service}}\t{{.Status}}\t{{.Ports}}'"

echo
echo "== Health =="
remote "AP_SERVICES_LIST='$services_list' AP_PORTS_LIST='$ports_list' bash -c '
services=(\$AP_SERVICES_LIST); ports=(\$AP_PORTS_LIST)
for i in \"\${!services[@]}\"; do
  printf \"%s: \" \"\${services[\$i]}\"
  curl -fsS \"http://127.0.0.1:\${ports[\$i]}/health\"
  printf \"\n\"
done'"

echo
echo "== Runtime directory =="
remote 'cd ~/hololive-bot && find . -maxdepth 1 -mindepth 1 -printf "%f\n" | sort'

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
remote 'docker volume ls --format "{{.Name}}" | grep "^hololive-bot_" || true'
