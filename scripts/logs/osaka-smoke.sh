#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
SSH_KEY="${SSH_KEY:-$REPO_ROOT/KR.key}"
OSAKA_HOST="${OSAKA_HOST:-kapu-iris-osaka-1}"
SSH_OSAKA=(ssh -F /dev/null -i "$SSH_KEY" -o IdentitiesOnly=yes -o SetEnv=LC_ALL=C -o SetEnv=LANG=C "ubuntu@$OSAKA_HOST")

"${SSH_OSAKA[@]}" 'set -euo pipefail
cd ~/hololive-bot

sudo env COMPOSE_ENV_FILE=/run/hololive-bot/env docker compose --env-file /run/hololive-bot/env -f docker-compose.prod.yml -f docker-compose.osaka.yml ps youtube-scraper stream-ingester

docker exec hololive-youtube-scraper ./bin/healthcheck http://127.0.0.1:30005/health
docker exec hololive-stream-ingester ./bin/healthcheck http://127.0.0.1:30004/health

docker exec hololive-youtube-scraper ./bin/healthcheck --smoke
docker exec hololive-stream-ingester ./bin/healthcheck --smoke

docker inspect hololive-youtube-scraper hololive-stream-ingester --format "{{.Name}} {{json .Config.Healthcheck.Test}} {{.Config.User}} {{.Config.Image}}"
'
