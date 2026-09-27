#!/usr/bin/env bash
set -Eeuo pipefail
[[ "$(hostname -s)" == kapu ]] || { echo 'AP image builds and export are restricted to kapu' >&2; exit 1; }

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
WORKSPACE_ROOT="${WORKSPACE_ROOT:-$(cd "$REPO_ROOT/.." && pwd)}"
REMOTE_REPO_DIR="${REMOTE_REPO_DIR:-hololive-bot}"
FILES_FROM="${FILES_FROM:-$REPO_ROOT/scripts/deploy/ap-rsync-files.txt}"
EXCLUDES="${EXCLUDES:-$REPO_ROOT/scripts/deploy/ap-rsync-excludes.txt}"

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
issuer_build_root=""
collector_check_id=""
trap 'rm -f "$preview_file" "$rsync_files_from"; [[ -z "$image_archive" ]] || rm -f "$image_archive"; [[ -z "$issuer_build_root" ]] || rm -rf "$issuer_build_root"; [[ -z "$collector_check_id" ]] || docker rm "$collector_check_id" >/dev/null' EXIT

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
PO_IMAGE_REF="hololive-youtube-po-sandbox:prod"
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
[[ "$TARGET_PLATFORM" == linux/arm64 ]] || { echo 'Seoul issuer deployment requires arm64' >&2; exit 1; }
issuer_build_root="$(mktemp -d)"
"$REPO_ROOT/scripts/build/build-po-sandbox-artifact.sh" arm64 "$REVISION" "$HOLO_API_VERSION" "$issuer_build_root/issuer"
test -s "$issuer_build_root/issuer/image.tar"
mkdir -p "$issuer_build_root/collector-check"
collector_check_id="$(docker create "$IMAGE_REF")"
docker cp "$collector_check_id:/app/manifest.json" "$issuer_build_root/collector-check/manifest.json"
docker cp "$collector_check_id:/app/bin/youtube-collector" "$issuer_build_root/collector-check/youtube-collector"
docker rm "$collector_check_id" >/dev/null
collector_check_id=""
python3 - "$issuer_build_root/collector-check/manifest.json" "$issuer_build_root/collector-check/youtube-collector" "$REVISION" "$HOLO_API_VERSION" <<'PY'
import hashlib, json, sys
m=json.load(open(sys.argv[1])); assert (m['source_revision'],m['version'],m['go']['goarch']) == (sys.argv[3],sys.argv[4],'arm64')
assert hashlib.sha256(open(sys.argv[2], 'rb').read()).hexdigest() == m['files']['bin/youtube-collector']
PY
image_archive="$(mktemp)"
docker save --output "$image_archive" "$IMAGE_REF"
test -s "$image_archive"
collector_image_id="$(docker image inspect -f '{{.Id}}' "$IMAGE_REF")"
[[ "$collector_image_id" =~ ^sha256:[0-9a-f]{64}$ ]]
collector_archive_sha="$(sha256sum "$image_archive")"
collector_archive_sha="${collector_archive_sha%% *}"
[[ "$collector_archive_sha" =~ ^[0-9a-f]{64}$ ]]

services_list="${AP_SERVICES[*]}"
containers_list="${AP_CONTAINERS[*]}"
ports_list="${AP_PORTS[*]}"
PROD_COMPOSE_FILE="deploy/compose/docker-compose.prod.yml"
PROD_COMPOSE_LEGACY_FILE="docker-compose.prod.yml"
AP_COMPOSE_LEGACY_FILE="$(basename "$AP_COMPOSE_FILE")"

change_id="$(date -u +%Y%m%dT%H%M%SZ)"
backup_dir="backups/$AP_BACKUP_PREFIX-$change_id"
rollback_image_tag="hololive-youtube-collector:rollback-$change_id"
rollback_po_image_tag="hololive-youtube-po-sandbox:rollback-$change_id"
po_manifest_active="backups/po-sandbox-current-b.json"
po_image_id_active="backups/po-sandbox-current-b.image-id"
producer_state_file="$backup_dir/retired-producer-runtime.state"
source_stage="$REMOTE_REPO_DIR/$backup_dir/source-candidate"
source_snapshot="$REPO_ROOT/scripts/deploy/lib/ap-source-snapshot.py"
source_armed=false
cutover_armed=false

remote "set -euo pipefail
cd ~/hololive-bot
mkdir -p '$backup_dir'
if sudo -n docker image inspect '$IMAGE_REF' >/dev/null 2>&1; then
  [[ \$(docker inspect -f '{{.State.Status}}' hololive-youtube-collector-b) == running ]]
  [[ \$(docker inspect -f '{{.Image}}' hololive-youtube-collector-b) == \$(sudo -n docker image inspect -f '{{.Id}}' '$IMAGE_REF') ]]
  sudo -n docker tag '$IMAGE_REF' '$rollback_image_tag'
  printf '%s\n' '$rollback_image_tag' > '$backup_dir/rollback-image-tag'
  sudo -n docker image inspect '$rollback_image_tag' >/dev/null
  previous_collector_revision=\$(sudo -n docker image inspect -f '{{index .Config.Labels \"org.opencontainers.image.revision\"}}' '$rollback_image_tag')
  [[ \"\$previous_collector_revision\" =~ ^[0-9a-f]{40}\$ ]]
  previous_collector_image_id=\$(sudo -n docker image inspect -f '{{.Id}}' '$rollback_image_tag')
  [[ \"\$previous_collector_image_id\" =~ ^sha256:[0-9a-f]{64}\$ ]]
  [[ \$(sudo -n docker image inspect -f '{{.Os}}/{{.Architecture}}' '$rollback_image_tag') == linux/arm64 ]]
  printf '%s\n' \"\$previous_collector_image_id\" > '$backup_dir/collector-prechange.image-id'
  python3 -c 'import json,sys; json.dump({\"schema_version\":1,\"source_revision\":sys.argv[1],\"go\":{\"goarch\":\"arm64\"},\"image_id\":sys.argv[2],\"version\":sys.argv[3]},open(sys.argv[4],\"w\"))' \"\$previous_collector_revision\" \"\$previous_collector_image_id\" \"\$(sudo -n docker image inspect -f '{{index .Config.Labels \"org.opencontainers.image.version\"}}' '$rollback_image_tag')\" '$backup_dir/collector-prechange-manifest.json'
else
  [[ -z \$(docker ps -q --filter 'name=^hololive-youtube-collector-b$') ]]
fi
if sudo -n docker image inspect '$PO_IMAGE_REF' >/dev/null 2>&1; then
  test -r '$po_manifest_active'
  test -r '$po_image_id_active'
  [[ \$(docker inspect -f '{{.State.Status}}' hololive-youtube-po-b) == running ]]
  [[ \$(docker inspect -f '{{.Image}}' hololive-youtube-po-b) == \$(sudo -n docker image inspect -f '{{.Id}}' '$PO_IMAGE_REF') ]]
  sudo -n docker tag '$PO_IMAGE_REF' '$rollback_po_image_tag'
  printf '%s\n' '$rollback_po_image_tag' > '$backup_dir/rollback-po-image-tag'
  cp '$po_manifest_active' '$backup_dir/po-sandbox-prechange-manifest.json'
  cp '$po_image_id_active' '$backup_dir/po-sandbox-prechange.image-id'
  printf 'present\n' > '$backup_dir/po-sandbox-prechange.state'
else
  test ! -e '$po_manifest_active'
  test ! -e '$po_image_id_active'
  [[ -z \$(docker ps -q --filter 'name=^hololive-youtube-po-b$') ]]
  printf 'absent\n' > '$backup_dir/po-sandbox-prechange.state'
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
printf '%s\n' \"\$prod_prechange_file\" > '$backup_dir/prod-compose-source-path'
printf '%s\n' \"\$ap_prechange_file\" > '$backup_dir/ap-compose-source-path'
docker ps -a --filter label=com.docker.compose.project=hololive --format '{{json .}}' > '$backup_dir/prechange-containers.json' 2>/dev/null || true
sudo -n test -r /etc/stack-secrets/hololive-bot/ap-compose.env
sudo -n test -r /etc/stack-secrets/hololive-bot/youtube-collector.env
test -w /var/run/docker.sock || groups | grep -qw docker
$(declare -f ap_prechange_config)
ap_prechange_config sudo -n env COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f \"\$prod_prechange_file\" -f \"\$ap_prechange_file\" config --quiet
echo backup_dir='$backup_dir'"

# 네트워크 전송은 live checkout을 덮지 않습니다. 완전한 후보를 먼저 준비합니다.
remote "mkdir -p '$source_stage'"
rsync -ai \
  --files-from="$rsync_files_from" \
  --exclude-from="$EXCLUDES" \
  "$WORKSPACE_ROOT"/ \
  -e "$RSYNC_RSH" \
  "$(ap_rsync_target "./$source_stage/")"
rsync -ai "$rsync_files_from" -e "$RSYNC_RSH" "$(ap_rsync_target "./$REMOTE_REPO_DIR/$backup_dir/source-files.manifest")"
rsync -anic --files-from="$rsync_files_from" --exclude-from="$EXCLUDES" \
  "$WORKSPACE_ROOT"/ -e "$RSYNC_RSH" "$(ap_rsync_target "./$source_stage/")" > "$preview_file"
[[ ! -s "$preview_file" ]] || { echo 'AP staged source differs from reviewed input' >&2; exit 1; }
"${AP_SSH[@]}" "python3 - capture \"\$HOME\" \"\$HOME/$REMOTE_REPO_DIR/$backup_dir\" \"\$HOME/$REMOTE_REPO_DIR/$backup_dir/source-files.manifest\"" < "$source_snapshot"

restore_after_failed_deploy() {
  local status="${1:-1}" restore_status=0
  trap - ERR INT TERM HUP
  set +e
  if [[ "$cutover_armed" == true ]]; then
    env "$AP_APPROVE_ROLLBACK_VAR=true" BACKUP_DIR="$backup_dir" \
      "$REPO_ROOT/scripts/deploy/ap-rollback.sh" "$AP_NAME" --apply
    restore_status="$?"
  elif [[ "$source_armed" == true ]]; then
    "${AP_SSH[@]}" "python3 - restore \"\$HOME\" \"\$HOME/$REMOTE_REPO_DIR/$backup_dir\"" < "$source_snapshot"
    restore_status="$?"
  fi
  if [[ "$restore_status" -ne 0 ]]; then
    echo "AP deployment rollback failed; preserve and recover from $backup_dir" >&2
  fi
  exit "$status"
}
trap 'restore_after_failed_deploy $?' ERR
trap 'restore_after_failed_deploy 130' INT
trap 'restore_after_failed_deploy 143' TERM
trap 'restore_after_failed_deploy 129' HUP
source_armed=true
remote "set -euo pipefail
rsync -a --no-implied-dirs --files-from='$REMOTE_REPO_DIR/$backup_dir/source-files.manifest' '$source_stage/' ./"

image_remote_path="$REMOTE_REPO_DIR/$backup_dir/hololive-youtube-collector-prod.tar"
rsync -ai \
  "$image_archive" \
  -e "$RSYNC_RSH" \
  "$(ap_rsync_target "./$image_remote_path")"
po_remote_dir="$REMOTE_REPO_DIR/$backup_dir"
rsync -ai "$issuer_build_root/issuer/image.tar" "$issuer_build_root/issuer/image.tar.sha256" \
  -e "$RSYNC_RSH" "$(ap_rsync_target "./$po_remote_dir/")"
rsync -ai "$issuer_build_root/issuer/rootfs-manifest.json" \
  -e "$RSYNC_RSH" "$(ap_rsync_target "./$po_remote_dir/po-sandbox-candidate-manifest.json")"
rsync -ai "$issuer_build_root/issuer/image-id" \
  -e "$RSYNC_RSH" "$(ap_rsync_target "./$po_remote_dir/po-sandbox-candidate.image-id")"

remote "set -euo pipefail
cd ~/hololive-bot
. scripts/deploy/lib/retired-producer-cutover.sh
write_retired_producer_runtime_state > '$producer_state_file'
validate_retired_producer_runtime_state '$producer_state_file'"

cutover_armed=true
remote "set -euo pipefail
cd ~/hololive-bot
. scripts/deploy/lib/po-sandbox-image.sh
if [[ \$(cat '$backup_dir/po-sandbox-prechange.state') == present ]]; then
  previous_po_revision=\$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))[\"source_revision\"])' '$backup_dir/po-sandbox-prechange-manifest.json')
  po_verify_image '$rollback_po_image_tag' '$backup_dir/po-sandbox-prechange-manifest.json' \"\$previous_po_revision\" arm64 '$backup_dir/po-sandbox-prechange.image-id'
  old_collector_revision=\$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))[\"source_revision\"])' '$backup_dir/collector-prechange-manifest.json')
  [[ \"\$previous_po_revision\" == \"\$old_collector_revision\" ]]
  [[ \$(sudo -n docker image inspect -f '{{index .Config.Labels \"org.opencontainers.image.version\"}}' '$rollback_po_image_tag') == \$(sudo -n docker image inspect -f '{{index .Config.Labels \"org.opencontainers.image.version\"}}' '$rollback_image_tag') ]]
fi
(cd '$backup_dir' && sha256sum --check --strict image.tar.sha256)
sudo -n docker load --input '$backup_dir/image.tar'
po_verify_image '$PO_IMAGE_REF' '$backup_dir/po-sandbox-candidate-manifest.json' '$REVISION' arm64 '$backup_dir/po-sandbox-candidate.image-id'
[[ \$(sudo -n docker image inspect -f '{{index .Config.Labels \"org.opencontainers.image.version\"}}' '$PO_IMAGE_REF') == '$HOLO_API_VERSION' ]]
image_archive='$backup_dir/hololive-youtube-collector-prod.tar'
trap 'rm -f \"\$image_archive\"' EXIT
[[ \$(sha256sum \"\$image_archive\" | cut -d' ' -f1) == '$collector_archive_sha' ]]
sudo -n docker load --input \"\$image_archive\"
loaded_revision=\$(sudo -n docker image inspect -f '{{index .Config.Labels \"org.opencontainers.image.revision\"}}' '$IMAGE_REF')
[[ \"\$loaded_revision\" == '$REVISION' ]]
loaded_platform=\$(sudo -n docker image inspect -f '{{.Os}}/{{.Architecture}}{{if .Variant}}/{{.Variant}}{{end}}' '$IMAGE_REF')
[[ \"\$loaded_platform\" == '$TARGET_PLATFORM' ]]
[[ \$(sudo -n docker image inspect -f '{{.Id}}' '$IMAGE_REF') == '$collector_image_id' ]]
[[ \$(sudo -n docker image inspect -f '{{index .Config.Labels \"org.opencontainers.image.version\"}}' '$IMAGE_REF') == '$HOLO_API_VERSION' ]]"

change_started_at="$(
  remote 'date -u +%Y-%m-%dT%H:%M:%SZ'
)"


remote "set -euo pipefail
cd ~/hololive-bot
. scripts/deploy/lib/retired-producer-cutover.sh
stop_retired_producer_runtime
sudo -n env HOLO_API_VERSION='$HOLO_API_VERSION' REVISION='$REVISION' COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f '$PROD_COMPOSE_FILE' -f '$AP_COMPOSE_FILE' config --quiet
sudo -n env HOLO_API_VERSION='$HOLO_API_VERSION' REVISION='$REVISION' COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f '$PROD_COMPOSE_FILE' -f '$AP_COMPOSE_FILE' up -d --no-build --no-deps --force-recreate youtube-po-b
for _ in \$(seq 1 30); do
  [[ \$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' hololive-youtube-po-b) == healthy ]] && break
  sleep 2
done
[[ \$(docker inspect -f '{{.State.Health.Status}}' hololive-youtube-po-b) == healthy ]]
docker exec hololive-youtube-po-b /app/bin/po-broker --healthcheck --socket /run/hololive-youtube-po/worker.sock
socket_mount=\$(docker inspect -f '{{range .Mounts}}{{if eq .Destination \"/run/hololive-youtube-po\"}}{{.Source}}{{end}}{{end}}' hololive-youtube-po-b)
[[ \$(sudo -n stat -c '%u:%g %a' \"\$socket_mount\") == '65532:1000 770' ]]
cp '$backup_dir/po-sandbox-candidate-manifest.json' '$po_manifest_active'
cp '$backup_dir/po-sandbox-candidate.image-id' '$po_image_id_active'
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
source_armed=false
trap - ERR INT TERM HUP
