#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
SSH_KEY="${SSH_KEY:-$REPO_ROOT/KR.key}"
OSAKA_HOST="${OSAKA_HOST:-kapu-iris-osaka-1}"
CHANGE_STARTED_AT="${CHANGE_STARTED_AT:-}"

if [[ -n "$CHANGE_STARTED_AT" && ! "$CHANGE_STARTED_AT" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]; then
  echo "CHANGE_STARTED_AT must be UTC RFC3339 seconds, for example 2026-05-19T18:28:03Z" >&2
  exit 2
fi

if [[ ! -r "$SSH_KEY" ]]; then
  echo "SSH key not readable: $SSH_KEY" >&2
  exit 1
fi

SSH_OSAKA=(ssh -F /dev/null -i "$SSH_KEY" -o IdentitiesOnly=yes -o SetEnv=LC_ALL=C -o SetEnv=LANG=C "ubuntu@$OSAKA_HOST")

remote() {
  "${SSH_OSAKA[@]}" "$@"
}

remote "set -euo pipefail
cd ~/hololive-bot
sudo -n test -r /run/hololive-bot/env
test -w /var/run/docker.sock || groups | grep -qw docker
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml ps youtube-producer-a youtube-producer-b

for container in hololive-youtube-producer-a hololive-youtube-producer-b; do
  docker inspect \"\$container\" >/dev/null
  status=\$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' \"\$container\")
  [[ \"\$status\" == healthy ]]
done

ready_a=\$(curl -fsS http://127.0.0.1:30005/ready)
ready_b=\$(curl -fsS http://127.0.0.1:30015/ready)
printf '%s' \"\$ready_a\" | grep -q '\"mode\":\"active-active\"'
printf '%s' \"\$ready_a\" | grep -q '\"valkey_available\":true'
printf '%s' \"\$ready_a\" | grep -q '\"scraping_paused\":false'
printf '%s' \"\$ready_b\" | grep -q '\"mode\":\"active-active\"'
printf '%s' \"\$ready_b\" | grep -q '\"valkey_available\":true'
printf '%s' \"\$ready_b\" | grep -q '\"scraping_paused\":false'

if [[ -n '$CHANGE_STARTED_AT' ]]; then
  since_epoch=\$(date -u -d '$CHANGE_STARTED_AT' +%s)
  for container in hololive-youtube-producer-a hololive-youtube-producer-b; do
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
