#!/usr/bin/env bash
set -Eeuo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
WORKSPACE_ROOT="${WORKSPACE_ROOT:-$(cd "$REPO_ROOT/.." && pwd)}"
REMOTE_REPO_DIR="${REMOTE_REPO_DIR:-hololive-bot}"
FILES_FROM="${FILES_FROM:-$REPO_ROOT/scripts/deploy/ap-rsync-files.txt}"
EXCLUDES="${EXCLUDES:-$REPO_ROOT/scripts/deploy/ap-rsync-excludes.txt}"
AP_ROLLBACK_TAG_KEEP="${AP_ROLLBACK_TAG_KEEP:-5}"

. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"
. "$REPO_ROOT/scripts/deploy/lib/ap-prechange-config.sh"
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
    data|data/*|*/data/*)
      echo "files-from list contains unapproved data path: $path" >&2
      exit 1
      ;;
  esac
done < "$FILES_FROM"

if rg -n '(^|/)(\.env[^/]*|[^/]*\.key|[^/]*\.pem|hololive-alarm-worker|[^/]*_test\.go|docs|logs|runtime-config|backups|artifacts)(/|$)' "$FILES_FROM" \
  | rg -v 'hololive/hololive-alarm-worker/VERSION$'; then
  echo "files-from list contains forbidden deployment scope" >&2
  exit 1
fi

RSYNC_RSH="$(ap_rsync_rsh)"

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
    "$(ap_rsync_target './')"
}

validate_preview() {
  local preview_file="$1"
  "$REPO_ROOT/scripts/deploy/check-ap-rsync-preview.sh" "$preview_file" "$REMOTE_REPO_DIR"
  if rg -n '(^|/)data/' "$preview_file"; then
    echo "rsync preview contains unapproved data path" >&2
    exit 1
  fi
}

rsync_files_from="$(mktemp)"
preview_file="$(mktemp)"
image_archive=""
trap 'rm -f "$preview_file" "$rsync_files_from"; [[ -z "$image_archive" ]] || rm -f "$image_archive"' EXIT

build_rsync_files_from
"$REPO_ROOT/scripts/deploy/check-ap-rsync-manifest.sh" "$FILES_FROM"
rsync_preview | tee "$preview_file"
validate_preview "$preview_file"

"$REPO_ROOT/scripts/deploy/ap-collector-preflight.sh" "$AP_NAME"

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

change_id="$(date -u +%Y%m%dT%H%M%SZ)"
backup_dir="backups/$AP_BACKUP_PREFIX-$change_id"
rollback_image_tag="hololive-youtube-collector:rollback-$change_id"
rollback_tag_prune_offset="$((AP_ROLLBACK_TAG_KEEP + 1))"

remote "set -euo pipefail
cd ~/hololive-bot
mkdir -p '$backup_dir'
if sudo -n docker image inspect '$IMAGE_REF' >/dev/null 2>&1; then
  sudo -n docker tag '$IMAGE_REF' '$rollback_image_tag'
  printf '%s\n' '$rollback_image_tag' > '$backup_dir/rollback-image-tag'
  sudo -n docker image inspect '$rollback_image_tag' >/dev/null
  stale_rollback_tags=\$(sudo -n docker images 'hololive-youtube-collector' --format '{{.Tag}}' | grep -E '^rollback-[0-9]{8}T[0-9]{6}Z\$' | sort -r | tail -n +'$rollback_tag_prune_offset' || true)
  for stale_rollback_tag in \$stale_rollback_tags; do
    sudo -n docker rmi \"hololive-youtube-collector:\$stale_rollback_tag\" >/dev/null 2>&1 || true
  done
fi
# compose 파일은 deploy/compose 아래 한 경로만 읽는다. repo 루트 사본으로 내려가던 폴백은 T18(2026-09-26)에서
# compose AP 루트에 docker-compose*.yml이 없음을 확인해 지웠다(stack-audit T11 holo-ap-legacy-compose-path-fallback).
prod_prechange_file='$PROD_COMPOSE_FILE'
ap_prechange_file='$AP_COMPOSE_FILE'
test -r \"\$prod_prechange_file\"
test -r \"\$ap_prechange_file\"
mkdir -p \"\$(dirname '$backup_dir/$PROD_COMPOSE_FILE.prechange')\" \"\$(dirname '$backup_dir/$AP_COMPOSE_FILE.prechange')\"
cp \"\$prod_prechange_file\" '$backup_dir/$PROD_COMPOSE_FILE.prechange'
cp \"\$ap_prechange_file\" '$backup_dir/$AP_COMPOSE_FILE.prechange'
docker ps -a --filter label=com.docker.compose.project=hololive --format '{{json .}}' > '$backup_dir/prechange-containers.json' 2>/dev/null || true
sudo -n test -r /etc/stack-secrets/hololive-bot/ap-compose.env
sudo -n test -r /etc/stack-secrets/hololive-bot/youtube-collector.env
test -w /var/run/docker.sock || groups | grep -qw docker
$(declare -f ap_prechange_config)
ap_prechange_config sudo -n env COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f \"\$prod_prechange_file\" -f \"\$ap_prechange_file\" config --quiet
echo backup_dir='$backup_dir'"

rsync -ai \
  --backup \
  --backup-dir="$REMOTE_REPO_DIR/$backup_dir/rsync-overwritten" \
  --files-from="$rsync_files_from" \
  --exclude-from="$EXCLUDES" \
  "$WORKSPACE_ROOT"/ \
  -e "$RSYNC_RSH" \
  "$(ap_rsync_target './')"

image_remote_path="$REMOTE_REPO_DIR/$backup_dir/hololive-youtube-collector-prod.tar"
rsync -ai \
  "$image_archive" \
  -e "$RSYNC_RSH" \
  "$(ap_rsync_target "./$image_remote_path")"

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

# rollback 기준점은 ap-rollback.sh와 같은 파일(rollback-image-tag)로 판정하고, trap을 걸기 전에 값을 확정한다.
rollback_available="$(
  remote "cd ~/hololive-bot && if [[ -r '$backup_dir/rollback-image-tag' ]]; then printf '%s\n' true; else printf '%s\n' false; fi"
)"
case "$rollback_available" in
  true|false) ;;
  *)
    echo "unexpected rollback-image-tag probe result for $AP_NAME: $rollback_available" >&2
    exit 1
    ;;
esac

# 기준점이 있는 재배포가 실패하면 collector를 배포된 상태로 두고 ap-rollback.sh로 되돌리게 한다. 기준점 없이 멈추면
# AP에 collector가 하나도 남지 않기 때문이다(stack-audit T05). 기준점이 없는 첫 배포가 실패하면 되돌릴 이전 collector가
# 없으므로 검증에 실패한 새 collector를 멈추고 비활성을 확인한 뒤 fix-forward한다. 이 정지는 퇴역 producer 복원과 무관하게
# 유지한다. 퇴역 producer의 첫 cutover 상태를 기록·복원하던 경로는 T18(2026-09-26)에서 모든 AP의 current·previous가
# collector release이고 producer unit·컨테이너가 0개임을 확인해 지웠다(stack-audit T11 holo-collector-retired-producer-cutover-tooling).
cutover_armed=true
handle_failed_collector_deploy() {
  local status="$?"
  local stop_status=0
  trap - ERR
  if [[ "${cutover_armed:-false}" != "true" ]]; then
    exit "$status"
  fi
  if [[ "$rollback_available" == "true" ]]; then
    echo "AP collector deploy failed after cutover; collector left as deployed. Roll back with BACKUP_DIR='$backup_dir' ./scripts/deploy/ap-rollback.sh $AP_NAME --apply" >&2
    exit "$status"
  fi
  set +e
  remote "set -euo pipefail
cd ~/hololive-bot
for container in $containers_list; do
  active=\$(docker ps -q --filter \"name=^\${container}\$\")
  if [[ -n \"\$active\" ]]; then
    echo \"[CUTOVER] Stopping failed first-deploy collector container: \${container}\"
    docker stop \"\$container\" >/dev/null
  fi
  active=\$(docker ps -q --filter \"name=^\${container}\$\")
  if [[ -n \"\$active\" ]]; then
    echo \"collector container still active: \${container}\" >&2
    exit 1
  fi
done"
  stop_status="$?"
  set -e
  if [[ "$stop_status" -ne 0 ]]; then
    echo "AP collector first deploy failed and the new collector could not be confirmed stopped on $AP_NAME ($containers_list); stop it before fixing forward" >&2
  else
    echo "AP collector first deploy failed; new collector stopped. $AP_NAME has no recorded rollback-image-tag, so fix forward" >&2
  fi
  exit "$status"
}
trap handle_failed_collector_deploy ERR

remote "set -euo pipefail
cd ~/hololive-bot
sudo -n env HOLO_API_VERSION='$HOLO_API_VERSION' REVISION='$REVISION' COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f '$PROD_COMPOSE_FILE' -f '$AP_COMPOSE_FILE' config --quiet
sudo -n env HOLO_API_VERSION='$HOLO_API_VERSION' REVISION='$REVISION' COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f '$PROD_COMPOSE_FILE' -f '$AP_COMPOSE_FILE' up -d --no-build --no-deps --force-recreate $services_list
echo change_started_at='$change_started_at'"

remote "set -euo pipefail
cd ~/hololive-bot
since='$change_started_at'
expected_revision='$REVISION'
. scripts/deploy/lib/ap-collector-readiness.sh
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
