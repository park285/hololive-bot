#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
SSH_KEY="${SSH_KEY:-$REPO_ROOT/KR.key}"
OSAKA_HOST="${OSAKA_HOST:-kapu-iris-osaka-1}"
LOG_SINCE="${LOG_SINCE:-10m}"
LOG_TAIL="${LOG_TAIL:-400}"
SSH_OSAKA=(ssh -F /dev/null -i "$SSH_KEY" -o IdentitiesOnly=yes -o SetEnv=LC_ALL=C -o SetEnv=LANG=C "ubuntu@$OSAKA_HOST")

remote() {
  "${SSH_OSAKA[@]}" "$@"
}

signals() {
  local container="$1"
  local pattern="$2"
  remote "hits=\$(docker logs --since '$LOG_SINCE' '$container' 2>&1 | grep -E '$pattern' || true); if [ -n \"\$hits\" ]; then printf '%s\n' \"\$hits\"; else docker logs --tail '$LOG_TAIL' '$container' 2>&1 | grep -E '$pattern' || true; fi"
}

echo "== Osaka compose services =="
remote 'cd ~/hololive-bot && sudo -n test -r /run/hololive-bot/env && (test -w /var/run/docker.sock || groups | grep -qw docker) && sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml ps --format "table {{.Name}}\t{{.Service}}\t{{.Status}}\t{{.Ports}}"'

echo
echo "== Health =="
remote 'printf "youtube-producer-a: "; curl -fsS http://127.0.0.1:30005/health; printf "\nyoutube-producer-b: "; curl -fsS http://127.0.0.1:30015/health; printf "\n"'

echo
echo "== Runtime directory =="
remote 'cd ~/hololive-bot && find . -maxdepth 1 -mindepth 1 -printf "%f\n" | sort'

echo
echo "== Recent youtube-producer signals =="
signals hololive-youtube-producer-a 'Cache store connected|postgres_pool_connected|job_claim|Photo sync service started|Photo sync completed|ERR|pre-send claim|ingestion_lease_lost|panic|permission denied'
signals hololive-youtube-producer-b 'Cache store connected|postgres_pool_connected|job_claim|Photo sync service started|Photo sync completed|ERR|pre-send claim|ingestion_lease_lost|panic|permission denied'

echo
echo "== Recent alarm-worker egress signals =="
signals hololive-alarm-worker 'YouTube outbox dispatcher started by alarm-worker|Outbox dispatcher started|Outbox per-room enqueue completed|Outbox per-room dispatch completed|ERR|panic|permission denied'

echo
echo "== Unused split-host volumes =="
remote 'docker volume ls --format "{{.Name}}" | grep "^hololive-bot_" || true'
