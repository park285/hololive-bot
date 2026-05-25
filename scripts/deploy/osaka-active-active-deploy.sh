#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
SSH_KEY="${SSH_KEY:-$REPO_ROOT/KR.key}"
OSAKA_HOST="${OSAKA_HOST:-kapu-iris-osaka-1}"
FILES_FROM="${FILES_FROM:-$REPO_ROOT/scripts/deploy/osaka-active-active-rsync-files.txt}"
EXCLUDES="${EXCLUDES:-$REPO_ROOT/scripts/deploy/osaka-rsync-excludes.txt}"
MODE="${1:---dry-run}"

case "$MODE" in
  --dry-run|--apply) ;;
  *)
    echo "Usage: $0 [--dry-run|--apply]" >&2
    exit 2
    ;;
esac

cd "$REPO_ROOT"

if [[ ! -r "$SSH_KEY" ]]; then
  echo "SSH key not readable: $SSH_KEY" >&2
  exit 1
fi
if [[ ! -r "$FILES_FROM" ]]; then
  echo "files-from list not readable: $FILES_FROM" >&2
  exit 1
fi
if [[ ! -r "$EXCLUDES" ]]; then
  echo "exclude list not readable: $EXCLUDES" >&2
  exit 1
fi

while IFS= read -r path; do
  [[ -n "$path" ]] || continue
  [[ -e "$path" ]] || {
    echo "files-from path does not exist: $path" >&2
    exit 1
  }
  case "$path" in
    hololive/hololive-youtube-producer/go.sum|hololive/hololive-shared/go.sum|shared-go/go.sum|../shared-go/go.sum) ;;
    go.sum|*/go.sum)
      echo "files-from list contains unapproved go.sum path: $path" >&2
      exit 1
      ;;
  esac
  case "$path" in
    hololive/hololive-shared/pkg/domain/internal/model/data/*) ;;
    data|data/*|*/data/*)
      echo "files-from list contains unapproved data path: $path" >&2
      exit 1
      ;;
  esac
done < "$FILES_FROM"

if rg -n '(^|/)(\.env[^/]*|[^/]*\.key|[^/]*\.pem|hololive-alarm-worker|_test\.go|docs|logs|runtime-config|backups|artifacts)(/|$)' "$FILES_FROM"; then
  echo "files-from list contains forbidden deployment scope" >&2
  exit 1
fi

SSH_OSAKA=(ssh -F /dev/null -i "$SSH_KEY" -o IdentitiesOnly=yes -o SetEnv=LC_ALL=C -o SetEnv=LANG=C "ubuntu@$OSAKA_HOST")
RSYNC_RSH="ssh -F /dev/null -i $SSH_KEY -o IdentitiesOnly=yes"

if [[ "$MODE" == "--apply" && "${I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY:-}" != "true" ]]; then
  echo "Refusing apply without I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY=true" >&2
  exit 2
fi

remote() {
  "${SSH_OSAKA[@]}" "$@"
}

rsync_preview() {
  rsync -ani \
    --files-from="$FILES_FROM" \
    --exclude-from="$EXCLUDES" \
    ./ \
    -e "$RSYNC_RSH" \
    "ubuntu@$OSAKA_HOST:~/hololive-bot/"
}

validate_preview() {
  local preview_file="$1"
  if rg -n '(\.env|\.key|\.pem|hololive-alarm-worker|_test\.go|docs/|/logs/|/runtime-config/|/backups/|artifacts/)' "$preview_file"; then
    echo "rsync preview contains forbidden deployment scope" >&2
    exit 1
  fi
  if rg -n '(^|/)data/' "$preview_file" | rg -v 'hololive/hololive-shared/pkg/domain/internal/model/data/'; then
    echo "rsync preview contains unapproved data path" >&2
    exit 1
  fi
}

preview_file="$(mktemp)"
trap 'rm -f "$preview_file"' EXIT

rsync_preview | tee "$preview_file"
validate_preview "$preview_file"

if [[ "$MODE" == "--dry-run" ]]; then
  echo "[DRY-RUN] No remote files or containers changed."
  exit 0
fi

change_id="$(date -u +%Y%m%dT%H%M%SZ)"
backup_dir="backups/osaka-active-active-$change_id"

remote "set -euo pipefail
cd ~/hololive-bot
mkdir -p '$backup_dir'
cp docker-compose.prod.yml '$backup_dir/docker-compose.prod.yml.prechange'
cp docker-compose.osaka.yml '$backup_dir/docker-compose.osaka.yml.prechange'
docker ps -a --filter label=com.docker.compose.project=hololive --format '{{json .}}' > '$backup_dir/prechange-containers.json' 2>/dev/null || true
sudo -n test -r /run/hololive-bot/env
test -w /var/run/docker.sock || groups | grep -qw docker
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml config --quiet
echo backup_dir='$backup_dir'"

rsync -ai \
  --backup \
  --backup-dir="$backup_dir/rsync-overwritten" \
  --files-from="$FILES_FROM" \
  --exclude-from="$EXCLUDES" \
  ./ \
  -e "$RSYNC_RSH" \
  "ubuntu@$OSAKA_HOST:~/hololive-bot/"

change_started_at="$(
  remote 'date -u +%Y-%m-%dT%H:%M:%SZ'
)"

remote "set -euo pipefail
cd ~/hololive-bot
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml config --quiet
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml build youtube-producer-a youtube-producer-b
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml up -d --no-deps --force-recreate --remove-orphans youtube-producer-a youtube-producer-b
echo change_started_at='$change_started_at'"

remote "set -euo pipefail
since='$change_started_at'
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
ready_a=\$(curl -fsS http://127.0.0.1:30005/ready)
ready_b=\$(curl -fsS http://127.0.0.1:30015/ready)
printf '%s' \"\$ready_a\" | grep -q '\"mode\":\"active-active\"'
printf '%s' \"\$ready_a\" | grep -q '\"valkey_available\":true'
printf '%s' \"\$ready_a\" | grep -q '\"scraping_paused\":false'
printf '%s' \"\$ready_b\" | grep -q '\"mode\":\"active-active\"'
printf '%s' \"\$ready_b\" | grep -q '\"valkey_available\":true'
printf '%s' \"\$ready_b\" | grep -q '\"scraping_paused\":false'
for container in hololive-youtube-producer-a hololive-youtube-producer-b; do
  if docker logs --since \"\$since\" \"\$container\" 2>&1 | grep -E 'ERR|panic|permission denied|x509|no such file'; then
    exit 1
  fi
done"

"$REPO_ROOT/scripts/logs/osaka-smoke.sh"
CHANGE_STARTED_AT="$change_started_at" "$REPO_ROOT/scripts/deploy/osaka-active-active-completion-check.sh"
"$REPO_ROOT/scripts/logs/osaka-status.sh"
