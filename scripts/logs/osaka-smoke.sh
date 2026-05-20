#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
SSH_KEY="${SSH_KEY:-$REPO_ROOT/KR.key}"
OSAKA_HOST="${OSAKA_HOST:-kapu-iris-osaka-1}"
OSAKA_SMOKE_EXTERNAL="${OSAKA_SMOKE_EXTERNAL:-false}"
case "$OSAKA_SMOKE_EXTERNAL" in
  true|false) ;;
  *)
    echo "OSAKA_SMOKE_EXTERNAL must be true or false" >&2
    exit 2
    ;;
esac
SSH_OSAKA=(ssh -F /dev/null -i "$SSH_KEY" -o IdentitiesOnly=yes -o SetEnv=LC_ALL=C -o SetEnv=LANG=C "ubuntu@$OSAKA_HOST")

"${SSH_OSAKA[@]}" "OSAKA_SMOKE_EXTERNAL=$OSAKA_SMOKE_EXTERNAL bash -s" <<'REMOTE'
set -euo pipefail
cd ~/hololive-bot

sudo -n test -r /run/hololive-bot/env
test -w /var/run/docker.sock || groups | grep -qw docker

sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml ps youtube-producer-a youtube-producer-b

ready_a="$(curl -fsS http://127.0.0.1:30005/ready)"
ready_b="$(curl -fsS http://127.0.0.1:30015/ready)"
printf "%s" "$ready_a" | grep -q "\"mode\":\"active-active\""
printf "%s" "$ready_a" | grep -q "\"valkey_available\":true"
printf "%s" "$ready_a" | grep -q "\"scraping_paused\":false"
printf "%s" "$ready_b" | grep -q "\"mode\":\"active-active\""
printf "%s" "$ready_b" | grep -q "\"valkey_available\":true"
printf "%s" "$ready_b" | grep -q "\"scraping_paused\":false"

docker exec hololive-youtube-producer-a ./bin/healthcheck http://127.0.0.1:30005/health
docker exec hololive-youtube-producer-b ./bin/healthcheck http://127.0.0.1:30015/health

if [[ "${OSAKA_SMOKE_EXTERNAL}" == "true" ]]; then
  docker exec hololive-youtube-producer-a ./bin/healthcheck --smoke
  docker exec hololive-youtube-producer-b ./bin/healthcheck --smoke
else
  echo "external smoke skipped (set OSAKA_SMOKE_EXTERNAL=true to run healthcheck --smoke)"
fi

docker inspect hololive-youtube-producer-a hololive-youtube-producer-b --format "{{.Name}} {{json .Config.Healthcheck.Test}} {{.Config.User}} {{.Config.Image}}"
REMOTE
