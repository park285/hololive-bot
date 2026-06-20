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
  *[!A-Za-z0-9._/-]*)
    echo "Refusing BACKUP_DIR with unsafe characters: $BACKUP_DIR" >&2
    exit 2
    ;;
  backups/"$AP_BACKUP_PREFIX"-*) ;;
  *)
    echo "Refusing suspicious BACKUP_DIR: $BACKUP_DIR" >&2
    exit 2
    ;;
esac

services_list="${AP_SERVICES[*]}"
containers_list="${AP_CONTAINERS[*]}"
ports_list="${AP_PORTS[*]}"
PROD_COMPOSE_FILE="deploy/compose/docker-compose.prod.yml"
PROD_COMPOSE_LEGACY_FILE="docker-compose.prod.yml"
PROD_BACKUP_FILE="$BACKUP_DIR/$PROD_COMPOSE_FILE.prechange"
PROD_BACKUP_LEGACY_FILE="$BACKUP_DIR/$PROD_COMPOSE_LEGACY_FILE.prechange"
AP_BACKUP_FILE="$BACKUP_DIR/$AP_COMPOSE_FILE.prechange"
AP_BACKUP_LEGACY_FILE="$BACKUP_DIR/$(basename "$AP_COMPOSE_FILE").prechange"

remote "set -euo pipefail
cd ~/hololive-bot
prod_backup_file='$PROD_BACKUP_FILE'
if [[ ! -r \"\$prod_backup_file\" && -r '$PROD_BACKUP_LEGACY_FILE' ]]; then
  prod_backup_file='$PROD_BACKUP_LEGACY_FILE'
fi
ap_backup_file='$AP_BACKUP_FILE'
if [[ ! -r \"\$ap_backup_file\" && -r '$AP_BACKUP_LEGACY_FILE' ]]; then
  ap_backup_file='$AP_BACKUP_LEGACY_FILE'
fi
test -r \"\$prod_backup_file\"
test -r \"\$ap_backup_file\"
sudo -n test -r /run/hololive-bot/ap-compose.env
sudo -n test -r /run/hololive-bot/youtube-producer.env
test -w /var/run/docker.sock || groups | grep -qw docker
echo backup_dir='$BACKUP_DIR'
echo would_restore=\"\$prod_backup_file\"
echo would_restore=\"\$ap_backup_file\"
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f \"\$prod_backup_file\" -f \"\$ap_backup_file\" config --quiet"

if [[ "$MODE" == "--dry-run" ]]; then
  echo "[DRY-RUN] Rollback preflight passed; no remote files or containers changed."
  exit 0
fi

rollback_started_at="$(
  remote 'date -u +%Y-%m-%dT%H:%M:%SZ'
)"

remote "set -euo pipefail
cd ~/hololive-bot
prod_backup_file='$PROD_BACKUP_FILE'
if [[ ! -r \"\$prod_backup_file\" && -r '$PROD_BACKUP_LEGACY_FILE' ]]; then
  prod_backup_file='$PROD_BACKUP_LEGACY_FILE'
fi
ap_backup_file='$AP_BACKUP_FILE'
if [[ ! -r \"\$ap_backup_file\" && -r '$AP_BACKUP_LEGACY_FILE' ]]; then
  ap_backup_file='$AP_BACKUP_LEGACY_FILE'
fi
mkdir -p \"\$(dirname '$PROD_COMPOSE_FILE')\" \"\$(dirname '$AP_COMPOSE_FILE')\"
cp \"\$prod_backup_file\" '$PROD_COMPOSE_FILE'
cp \"\$ap_backup_file\" '$AP_COMPOSE_FILE'
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f '$PROD_COMPOSE_FILE' -f '$AP_COMPOSE_FILE' config --quiet
docker stop $containers_list >/dev/null 2>&1 || true
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f '$PROD_COMPOSE_FILE' -f '$AP_COMPOSE_FILE' up -d --no-deps --force-recreate $services_list
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
