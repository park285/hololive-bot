#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
BACKUP_DIR="${BACKUP_DIR:-}"

. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"

AP_HOST_ARG="${1:-}"
MODE="${2:---dry-run}"

case "$MODE" in
  --dry-run|--apply) ;;
  *)
    echo "Usage: $0 <ap-host> [--dry-run|--apply]" >&2
    exit 2
    ;;
esac

ap_host_load "$REPO_ROOT" "$AP_HOST_ARG"

if [[ "$MODE" == "--apply" && "${!AP_APPROVE_ROLLBACK_VAR:-}" != "true" ]]; then
  echo "Refusing rollback without $AP_APPROVE_ROLLBACK_VAR=true" >&2
  exit 2
fi

remote() {
  "${AP_SSH[@]}" "$@"
}

if [[ -z "$BACKUP_DIR" ]]; then
  BACKUP_DIR="$(
    remote "set -euo pipefail
cd ~/hololive-bot
find backups -maxdepth 1 -type d -name '$AP_BACKUP_PREFIX-*' 2>/dev/null | sort | tail -n 1" || true
  )"
fi

if [[ -z "$BACKUP_DIR" ]]; then
  echo "No $AP_BACKUP_PREFIX backup found. Set BACKUP_DIR=backups/$AP_BACKUP_PREFIX-<timestamp>." >&2
  exit 1
fi

case "$BACKUP_DIR" in
  backups/"$AP_BACKUP_PREFIX"-*) ;;
  *)
    echo "Refusing suspicious BACKUP_DIR: $BACKUP_DIR" >&2
    exit 2
    ;;
esac

services_list="${AP_SERVICES[*]}"
containers_list="${AP_CONTAINERS[*]}"
ports_list="${AP_PORTS[*]}"

remote "set -euo pipefail
cd ~/hololive-bot
test -r '$BACKUP_DIR/$AP_COMPOSE_FILE.prechange'
test -r '$BACKUP_DIR/docker-compose.prod.yml.prechange'
sudo -n test -r /run/hololive-bot/env
test -w /var/run/docker.sock || groups | grep -qw docker
echo backup_dir='$BACKUP_DIR'
echo would_restore='$BACKUP_DIR/docker-compose.prod.yml.prechange'
echo would_restore='$BACKUP_DIR/$AP_COMPOSE_FILE.prechange'
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f '$BACKUP_DIR/docker-compose.prod.yml.prechange' -f '$BACKUP_DIR/$AP_COMPOSE_FILE.prechange' config --quiet"

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
cp '$BACKUP_DIR/$AP_COMPOSE_FILE.prechange' '$AP_COMPOSE_FILE'
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f '$AP_COMPOSE_FILE' config --quiet
docker stop $containers_list >/dev/null 2>&1 || true
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f '$AP_COMPOSE_FILE' up -d --no-deps --force-recreate $services_list
echo rollback_started_at='$rollback_started_at'"

remote "set -euo pipefail
since='$rollback_started_at'
since_epoch=\$(date -u -d \"\$since\" +%s)
for container in $containers_list; do
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
for port in $ports_list; do
  curl -fsS \"http://127.0.0.1:\$port/health\" >/dev/null
done
for container in $containers_list; do
  if docker logs --since \"\$since\" \"\$container\" 2>&1 | grep -E 'ERR|panic|permission denied|x509|no such file'; then
    exit 1
  fi
done"

"$REPO_ROOT/scripts/logs/ap-status.sh" "$AP_NAME"
