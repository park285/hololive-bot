#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
SSH_KEY="${SSH_KEY:-$REPO_ROOT/KR.key}"
OSAKA_HOST="${OSAKA_HOST:-kapu-iris-osaka-1}"
BACKUP_DIR="${BACKUP_DIR:-}"
MODE="${1:---dry-run}"

case "$MODE" in
  --dry-run|--apply) ;;
  *)
    echo "Usage: $0 [--dry-run|--apply]" >&2
    exit 2
    ;;
esac

if [[ "$MODE" == "--apply" && "${I_APPROVE_OSAKA_ACTIVE_ACTIVE_ROLLBACK:-}" != "true" ]]; then
  echo "Refusing rollback without I_APPROVE_OSAKA_ACTIVE_ACTIVE_ROLLBACK=true" >&2
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

if [[ -z "$BACKUP_DIR" ]]; then
  BACKUP_DIR="$(
    remote "set -euo pipefail
cd ~/hololive-bot
find backups -maxdepth 1 -type d -name 'osaka-active-active-*' 2>/dev/null | sort | tail -n 1" || true
  )"
fi

if [[ -z "$BACKUP_DIR" ]]; then
  echo "No osaka-active-active backup found. Set BACKUP_DIR=backups/osaka-active-active-<timestamp>." >&2
  exit 1
fi

case "$BACKUP_DIR" in
  backups/osaka-active-active-*) ;;
  *)
    echo "Refusing suspicious BACKUP_DIR: $BACKUP_DIR" >&2
    exit 2
    ;;
esac

remote "set -euo pipefail
cd ~/hololive-bot
test -r '$BACKUP_DIR/docker-compose.osaka.yml.prechange'
test -r '$BACKUP_DIR/docker-compose.prod.yml.prechange'
sudo -n test -r /run/hololive-bot/env
test -w /var/run/docker.sock || groups | grep -qw docker
echo backup_dir='$BACKUP_DIR'
echo would_restore='$BACKUP_DIR/docker-compose.prod.yml.prechange'
echo would_restore='$BACKUP_DIR/docker-compose.osaka.yml.prechange'
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f '$BACKUP_DIR/docker-compose.prod.yml.prechange' -f '$BACKUP_DIR/docker-compose.osaka.yml.prechange' config --quiet"

if [[ "$MODE" == "--dry-run" ]]; then
  echo "[DRY-RUN] Rollback preflight passed; no remote files or containers changed."
  exit 0
fi

rollback_started_at="$(
  remote 'date -u +%Y-%m-%dT%H:%M:%SZ'
)"

remote "set -euo pipefail
cd ~/hololive-bot
cp '$BACKUP_DIR/docker-compose.prod.yml.prechange' docker-compose.prod.yml
cp '$BACKUP_DIR/docker-compose.osaka.yml.prechange' docker-compose.osaka.yml
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml config --quiet
docker stop hololive-youtube-producer-a hololive-youtube-producer-b >/dev/null 2>&1 || true
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml up -d --no-deps --force-recreate youtube-producer-a youtube-producer-b
echo rollback_started_at='$rollback_started_at'"

remote "set -euo pipefail
since='$rollback_started_at'
since_epoch=\$(date -u -d \"\$since\" +%s)
for container in hololive-youtube-producer-a hololive-youtube-producer-b; do
  for _ in \$(seq 1 30); do
    status=\$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' \"\$container\")
    [[ \"\$status\" == healthy ]] && break
    sleep 2
  done
  status=\$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' \"\$container\")
  [[ \"\$status\" == healthy ]]
  started_at=\$(docker inspect -f '{{.State.StartedAt}}' \"\$container\")
  started_epoch=\$(date -u -d \"\$started_at\" +%s)
  [[ \"\$started_epoch\" -ge \"\$since_epoch\" ]]
done
curl -fsS http://127.0.0.1:30005/health >/dev/null
curl -fsS http://127.0.0.1:30015/health >/dev/null
for container in hololive-youtube-producer-a hololive-youtube-producer-b; do
  if docker logs --since \"\$since\" \"\$container\" 2>&1 | grep -E 'ERR|panic|permission denied|x509|no such file'; then
    exit 1
  fi
done"

"$REPO_ROOT/scripts/logs/osaka-status.sh"
