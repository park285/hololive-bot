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
remote 'cd ~/hololive-bot && sudo env COMPOSE_ENV_FILE=/run/hololive-bot/env docker compose --env-file /run/hololive-bot/env -f docker-compose.prod.yml -f docker-compose.osaka.yml ps --format "table {{.Name}}\t{{.Service}}\t{{.Status}}\t{{.Ports}}"'

echo
echo "== Health =="
remote 'printf "youtube-scraper: "; curl -fsS http://127.0.0.1:30005/health; printf "\nstream-ingester: "; curl -fsS http://127.0.0.1:30004/health; printf "\n"'

echo
echo "== Runtime directory =="
remote 'cd ~/hololive-bot && find . -maxdepth 1 -mindepth 1 -printf "%f\n" | sort'

echo
echo "== Recent youtube-scraper signals =="
signals hololive-youtube-scraper 'Cache store connected|postgres_pool_connected|ingestion_lease_acquired|ERR|pre-send claim|ingestion_lease_lost|panic|permission denied'

echo
echo "== Recent alarm-worker egress signals =="
signals hololive-alarm-worker 'YouTube outbox dispatcher started by alarm-worker|Outbox dispatcher started|Outbox per-room enqueue completed|Outbox per-room dispatch completed|ERR|panic|permission denied'

echo
echo "== Recent stream-ingester signals =="
signals hololive-stream-ingester 'Cache store connected|postgres_pool_connected|ingestion_lease_acquired|ERR|panic|permission denied|photo sync|runtime'

echo
echo "== Unused split-host volumes =="
remote 'docker volume ls --format "{{.Name}}" | grep "^hololive-bot_" || true'
