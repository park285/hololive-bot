#!/usr/bin/env bash
set -Eeuo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
WORKSPACE_ROOT="${WORKSPACE_ROOT:-$(cd "$REPO_ROOT/.." && pwd)}"
REMOTE_REPO_DIR="${REMOTE_REPO_DIR:-hololive-bot}"
FILES_FROM="${FILES_FROM:-$REPO_ROOT/scripts/deploy/ap-rsync-files.txt}"
EXCLUDES="${EXCLUDES:-$REPO_ROOT/scripts/deploy/ap-rsync-excludes.txt}"

. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"
. "$REPO_ROOT/scripts/deploy/lib/source-revision.sh"

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

if [[ "$AP_RUNTIME_MODE" != "compose" ]]; then
  echo "Refusing Compose AP deploy for $AP_NAME (runtime=$AP_RUNTIME_MODE); use ./scripts/deploy/ap-host-native-deploy.sh $AP_NAME" >&2
  exit 2
fi

HOLO_API_VERSION=""
. "$REPO_ROOT/scripts/deploy/lib/ap-compose-version.sh"
HOLO_API_VERSION="$(ap_compose_release_version "$REPO_ROOT")"

cd "$REPO_ROOT"

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
    hololive/hololive-youtube-collector/go.sum|hololive/hololive-dbtest/go.sum|hololive/hololive-shared/go.sum|shared-go/go.sum|../shared-go/go.sum) ;;
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

if rg -n '(^|/)(\.env[^/]*|[^/]*\.key|[^/]*\.pem|hololive-alarm-worker|[^/]*_test\.go|docs|logs|runtime-config|backups|artifacts)(/|$)' "$FILES_FROM"; then
  echo "files-from list contains forbidden deployment scope" >&2
  exit 1
fi

RSYNC_RSH="ssh -F /dev/null -i $SSH_KEY -o IdentitiesOnly=yes"

if [[ "$MODE" == "--apply" && "${!AP_APPROVE_DEPLOY_VAR:-}" != "true" ]]; then
  echo "Refusing apply without $AP_APPROVE_DEPLOY_VAR=true" >&2
  exit 2
fi

remote() {
  "${AP_SSH[@]}" "$@"
}

build_rsync_files_from() {
  while IFS= read -r path; do
    [[ -n "$path" ]] || continue
    case "$path" in
      ../shared-go/*)
        printf 'shared-go/%s\n' "${path#../shared-go/}"
        ;;
      ../*)
        echo "files-from list contains unsupported parent path: $path" >&2
        exit 1
        ;;
      *)
        printf '%s/%s\n' "$REMOTE_REPO_DIR" "$path"
        ;;
    esac
  done < "$FILES_FROM" > "$rsync_files_from"
}

rsync_preview() {
  rsync -ani \
    --files-from="$rsync_files_from" \
    --exclude-from="$EXCLUDES" \
    "$WORKSPACE_ROOT"/ \
    -e "$RSYNC_RSH" \
    "ubuntu@$AP_SSH_HOST:~/"
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

rsync_files_from="$(mktemp)"
preview_file="$(mktemp)"
image_archive=""
trap 'rm -f "$preview_file" "$rsync_files_from"; [[ -z "$image_archive" ]] || rm -f "$image_archive"' EXIT

build_rsync_files_from
rsync_preview | tee "$preview_file"
validate_preview "$preview_file"

"$REPO_ROOT/scripts/deploy/check-ap-rsync-manifest.sh" "$FILES_FROM"

"$REPO_ROOT/scripts/deploy/ap-iris-h3-trust-preflight.sh" "$AP_NAME"

if [[ "$MODE" == "--dry-run" ]]; then
  echo "[DRY-RUN] No remote files or containers changed."
  exit 0
fi

REVISION="$(deploy_source_revision "$REPO_ROOT")"
export REVISION

IMAGE_REF="hololive-youtube-collector:prod"
TARGET_PLATFORM="$(
  remote "set -euo pipefail
runtime_arch=\$(sudo -n docker info --format '{{.Architecture}}')
case \"\$runtime_arch\" in
  aarch64|arm64) printf '%s\\n' linux/arm64 ;;
  x86_64|amd64) printf '%s\\n' linux/amd64 ;;
  armv7|armv7l) printf '%s\\n' linux/arm/v7 ;;
  *)
    echo \"Unsupported AP Docker architecture: \$runtime_arch\" >&2
    exit 1
    ;;
esac"
)"

echo "[BUILD] Building $IMAGE_REF for $TARGET_PLATFORM"
docker buildx build \
  --platform "$TARGET_PLATFORM" \
  --provenance=false \
  --sbom=false \
  --load \
  --tag "$IMAGE_REF" \
  --file "$REPO_ROOT/hololive/hololive-youtube-collector/Dockerfile" \
  --build-arg "VERSION=$HOLO_API_VERSION" \
  --build-arg "REVISION=$REVISION" \
  "$REPO_ROOT"
built_revision="$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$IMAGE_REF")"
[[ "$built_revision" == "$REVISION" ]]
built_platform="$(docker image inspect -f '{{.Os}}/{{.Architecture}}{{if .Variant}}/{{.Variant}}{{end}}' "$IMAGE_REF")"
[[ "$built_platform" == "$TARGET_PLATFORM" ]]
image_archive="$(mktemp)"
docker save --output "$image_archive" "$IMAGE_REF"
test -s "$image_archive"

services_list="${AP_SERVICES[*]}"
containers_list="${AP_CONTAINERS[*]}"
ports_list="${AP_PORTS[*]}"
PROD_COMPOSE_FILE="deploy/compose/docker-compose.prod.yml"
PROD_COMPOSE_LEGACY_FILE="docker-compose.prod.yml"
AP_COMPOSE_LEGACY_FILE="$(basename "$AP_COMPOSE_FILE")"

change_id="$(date -u +%Y%m%dT%H%M%SZ)"
backup_dir="backups/$AP_BACKUP_PREFIX-$change_id"
rollback_image_tag="hololive-youtube-collector:rollback-$change_id"
producer_state_file="$backup_dir/retired-producer-runtime.state"

remote "set -euo pipefail
cd ~/hololive-bot
mkdir -p '$backup_dir'
if sudo -n docker image inspect '$IMAGE_REF' >/dev/null 2>&1; then
  sudo -n docker tag '$IMAGE_REF' '$rollback_image_tag'
  printf '%s\n' '$rollback_image_tag' > '$backup_dir/rollback-image-tag'
  sudo -n docker image inspect '$rollback_image_tag' >/dev/null
fi
prod_prechange_file='$PROD_COMPOSE_FILE'
if [[ ! -r \"\$prod_prechange_file\" && -r '$PROD_COMPOSE_LEGACY_FILE' ]]; then
  prod_prechange_file='$PROD_COMPOSE_LEGACY_FILE'
fi
ap_prechange_file='$AP_COMPOSE_FILE'
if [[ ! -r \"\$ap_prechange_file\" && -r '$AP_COMPOSE_LEGACY_FILE' ]]; then
  ap_prechange_file='$AP_COMPOSE_LEGACY_FILE'
fi
test -r \"\$prod_prechange_file\"
test -r \"\$ap_prechange_file\"
mkdir -p \"\$(dirname '$backup_dir/$PROD_COMPOSE_FILE.prechange')\" \"\$(dirname '$backup_dir/$AP_COMPOSE_FILE.prechange')\"
cp \"\$prod_prechange_file\" '$backup_dir/$PROD_COMPOSE_FILE.prechange'
cp \"\$ap_prechange_file\" '$backup_dir/$AP_COMPOSE_FILE.prechange'
docker ps -a --filter label=com.docker.compose.project=hololive --format '{{json .}}' > '$backup_dir/prechange-containers.json' 2>/dev/null || true
sudo -n test -r /etc/stack-secrets/hololive-bot/ap-compose.env
sudo -n test -r /etc/stack-secrets/hololive-bot/youtube-collector.env
test -w /var/run/docker.sock || groups | grep -qw docker
prechange_config_err=\$(mktemp)
if ! sudo -n env HOLO_API_VERSION='$HOLO_API_VERSION' COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f \"\$prod_prechange_file\" -f \"\$ap_prechange_file\" config --quiet 2>\"\$prechange_config_err\"; then
  if grep -Eq 'IRIS_(WEBHOOK|BOT)_TOKEN|SESSION_SECRET|ADMIN_PASS_BCRYPT|HOLO_BOT_API_KEY|/etc/stack-secrets/hololive-bot/(bot|alarm-worker)\.env' \"\$prechange_config_err\"; then
    echo 'AP prechange compose config skipped: token-free ap-compose.env is incompatible with pre-rsync compose; post-rsync config remains required' >&2
  else
    cat \"\$prechange_config_err\" >&2
    rm -f \"\$prechange_config_err\"
    exit 1
  fi
fi
rm -f \"\$prechange_config_err\"
echo backup_dir='$backup_dir'"

rsync -ai \
  --backup \
  --backup-dir="$REMOTE_REPO_DIR/$backup_dir/rsync-overwritten" \
  --files-from="$rsync_files_from" \
  --exclude-from="$EXCLUDES" \
  "$WORKSPACE_ROOT"/ \
  -e "$RSYNC_RSH" \
  "ubuntu@$AP_SSH_HOST:~/"

image_remote_path="$REMOTE_REPO_DIR/$backup_dir/hololive-youtube-collector-prod.tar"
rsync -ai \
  "$image_archive" \
  -e "$RSYNC_RSH" \
  "ubuntu@$AP_SSH_HOST:~/$image_remote_path"

remote "set -euo pipefail
cd ~/hololive-bot
image_archive='$backup_dir/hololive-youtube-collector-prod.tar'
trap 'rm -f \"\$image_archive\"' EXIT
sudo -n docker load --input \"\$image_archive\"
loaded_revision=\$(sudo -n docker image inspect -f '{{index .Config.Labels \"org.opencontainers.image.revision\"}}' '$IMAGE_REF')
[[ \"\$loaded_revision\" == '$REVISION' ]]
loaded_platform=\$(sudo -n docker image inspect -f '{{.Os}}/{{.Architecture}}{{if .Variant}}/{{.Variant}}{{end}}' '$IMAGE_REF')
[[ \"\$loaded_platform\" == '$TARGET_PLATFORM' ]]"

change_started_at="$(
  remote 'date -u +%Y-%m-%dT%H:%M:%SZ'
)"

remote "set -euo pipefail
cd ~/hololive-bot
. scripts/deploy/lib/retired-producer-cutover.sh
write_retired_producer_runtime_state > '$producer_state_file'
validate_retired_producer_runtime_state '$producer_state_file'"

cutover_armed=true
restore_retired_producer_after_failed_cutover() {
  local status="$?"
  local restore_status=0
  trap - ERR
  if [[ "${cutover_armed:-false}" == "true" ]]; then
    set +e
    remote "set -euo pipefail
cd ~/hololive-bot
. scripts/deploy/lib/retired-producer-cutover.sh
stop_named_containers_and_require_inactive $containers_list
restore_retired_producer_runtime '$producer_state_file'"
    restore_status="$?"
    set -e
    if [[ "$restore_status" -ne 0 ]]; then
      echo "AP collector cutover failed and the recorded producer runtime could not be restored" >&2
    fi
  fi
  exit "$status"
}
trap restore_retired_producer_after_failed_cutover ERR

remote "set -euo pipefail
cd ~/hololive-bot
. scripts/deploy/lib/retired-producer-cutover.sh
. scripts/deploy/lib/ap-collector-readiness.sh
stop_retired_producer_runtime
sudo -n env HOLO_API_VERSION='$HOLO_API_VERSION' REVISION='$REVISION' COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f '$PROD_COMPOSE_FILE' -f '$AP_COMPOSE_FILE' config --quiet
sudo -n env HOLO_API_VERSION='$HOLO_API_VERSION' REVISION='$REVISION' COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f '$PROD_COMPOSE_FILE' -f '$AP_COMPOSE_FILE' up -d --no-build --no-deps --force-recreate $services_list
echo change_started_at='$change_started_at'"

remote "set -euo pipefail
since='$change_started_at'
expected_revision='$REVISION'
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
  actual_revision=\$(docker inspect -f '{{index .Config.Labels \"org.opencontainers.image.revision\"}}' \"\$container\")
  [[ \"\$actual_revision\" == \"\$expected_revision\" ]]
done
ports=($ports_list)
idx=0
for container in $containers_list; do
  ready=\$(docker exec \"\$container\" ./bin/healthcheck --body \"https://127.0.0.1:\${ports[\$idx]}/ready\")
  collector_readiness_validate \"\$ready\"
  docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' \"\$container\" | grep -qx 'YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true'
  docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' \"\$container\" | grep -qx 'POSTGRES_USER=hololive_scraper'
  idx=\$((idx + 1))
done
for container in $containers_list; do
  if docker logs --since \"\$since\" \"\$container\" 2>&1 | grep -E 'ERR|panic|permission denied|x509|no such file'; then
    exit 1
  fi
done"

"$REPO_ROOT/scripts/logs/ap-smoke.sh" "$AP_NAME"
CHANGE_STARTED_AT="$change_started_at" "$REPO_ROOT/scripts/deploy/ap-completion-check.sh" "$AP_NAME"
"$REPO_ROOT/scripts/logs/ap-status.sh" "$AP_NAME"
cutover_armed=false
trap - ERR
