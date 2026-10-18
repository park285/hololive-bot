#!/usr/bin/env bash
set -euo pipefail
[[ "$(hostname -s)" == kapu ]] || { echo 'native issuer artifact builds are restricted to kapu' >&2; exit 1; }

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
MODE="${2:---dry-run}"
AP_REQUIRED_UDP_BUFFER_BYTES="${AP_REQUIRED_UDP_BUFFER_BYTES:-7500000}"
AP_SWAPFILE_SIZE_MIB="${AP_SWAPFILE_SIZE_MIB:-2048}"
# Read the non-secret primary owner, never a second address default.
AP_POSTGRES_HOST=""
while IFS='=' read -r host_name host_address; do
  if [[ "$host_name" == central ]]; then AP_POSTGRES_HOST="$host_address"; fi
done < "$REPO_ROOT/deploy/topology/hosts.conf"
[[ -n "$AP_POSTGRES_HOST" ]] || { echo 'topology central address missing' >&2; exit 1; }
AP_POSTGRES_POOL_MAX_CONNS=""
while IFS='|' read -r owner _service _key _source _instances maximum _role; do
  if [[ "$owner" == youtube-collector ]]; then AP_POSTGRES_POOL_MAX_CONNS="$maximum"; fi
done < "$REPO_ROOT/scripts/ci/postgres-capacity-policy.tsv"
[[ "$AP_POSTGRES_POOL_MAX_CONNS" =~ ^[1-9][0-9]*$ ]] || { echo 'collector capacity policy missing' >&2; exit 1; }
AP_POSTGRES_PORT="${AP_POSTGRES_PORT:-5433}"

case "$MODE" in
  --dry-run|--apply|--prepare-profile) ;;
  *)
    echo "Usage: $0 <ap-host> [--dry-run|--apply|--prepare-profile]" >&2
    exit 2
    ;;
esac

. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"
. "$REPO_ROOT/scripts/deploy/lib/ap-host-native-release-path.sh"
. "$REPO_ROOT/scripts/deploy/lib/source-revision.sh"
NODE_VERSION_LIB="$REPO_ROOT/scripts/deploy/lib/youtubejs-node-version.sh"
REMOTE_APPLY_LIB="$REPO_ROOT/scripts/deploy/lib/ap-host-native-remote-apply.sh"
COLLECTOR_WRAPPER_LIB="$REPO_ROOT/scripts/deploy/lib/ap-host-native-collector-wrapper.sh"
READINESS_LIB="$REPO_ROOT/scripts/deploy/lib/ap-collector-readiness.sh"
UNIT_TEMPLATE="$REPO_ROOT/scripts/deploy/lib/hololive-youtube-collector.service"
PO_UNIT_TEMPLATE="$REPO_ROOT/scripts/deploy/lib/hololive-youtube-po.service"
PO_SOCKET_TEMPLATE="$REPO_ROOT/scripts/deploy/lib/hololive-youtube-po.socket"
ap_host_load "$REPO_ROOT" "${1:-}"

if [[ "$AP_RUNTIME_MODE" != "native" ]]; then
  echo "Refusing host-native AP deploy for $AP_NAME (runtime=$AP_RUNTIME_MODE); use ./scripts/deploy/ap-deploy.sh $AP_NAME" >&2
  exit 2
fi

if [[ ! "$AP_REQUIRED_UDP_BUFFER_BYTES" =~ ^[0-9]+$ ]]; then
  echo "AP_REQUIRED_UDP_BUFFER_BYTES must be an integer" >&2
  exit 2
fi
if [[ ! "$AP_SWAPFILE_SIZE_MIB" =~ ^[1-9][0-9]*$ ]]; then
  echo "AP_SWAPFILE_SIZE_MIB must be a positive integer" >&2
  exit 2
fi
if [[ ! "$AP_POSTGRES_PORT" =~ ^[0-9]{1,5}$ ]] || (( 10#${AP_POSTGRES_PORT} < 1 || 10#${AP_POSTGRES_PORT} > 65535 )); then
  echo "AP_POSTGRES_PORT must be a valid TCP port" >&2
  exit 2
fi

if [[ ${#AP_SERVICES[@]} -ne 1 || ${#AP_PORTS[@]} -ne 1 ]]; then
  echo "host-native deploy currently supports exactly one AP service per host" >&2
  exit 2
fi

if [[ "$MODE" != "--dry-run" && "${!AP_APPROVE_DEPLOY_VAR:-}" != "true" ]]; then
  echo "Refusing apply without $AP_APPROVE_DEPLOY_VAR=true" >&2
  exit 2
fi
if [[ "$MODE" == --prepare-profile ]]; then
  service="${AP_SERVICES[0]}"
  "${AP_SSH[@]}" "sudo -n python3 - prepare-native '$service' /opt/hololive-bot/youtube-collector/current /etc/hololive-bot/youtube-collector-host.env 'hololive-youtube-collector@$service.service'" \
    < "$REPO_ROOT/scripts/deploy/lib/worker-profile-pair.py"
  exit 0
fi

release_id="${RELEASE_ID:-$(date -u +%Y%m%dT%H%M%SZ)-$(git -C "$REPO_ROOT" rev-parse --short=12 HEAD)-$AP_NAME}"
native_release_id_validate "$release_id"

if [[ "$AP_GOAMD64" != v1 ]]; then
  # 지원하지 않는 CPU에서는 새 바이너리가 기동하지 못하므로 빌드·전송 전에 거절합니다.
  if ! ap_remote_bash "$AP_GOAMD64" <<'REMOTE'
/lib64/ld-linux-x86-64.so.2 --help | grep -Fq "x86-64-$1 (supported"
REMOTE
  then
    echo "Refusing native AP deploy for $AP_NAME: host CPU does not report x86-64-$AP_GOAMD64 support" >&2
    exit 2
  fi
fi

service="${AP_SERVICES[0]}"
port="${AP_PORTS[0]}"
artifact_dir="${ARTIFACT_DIR:-$REPO_ROOT/artifacts/ap-host-native/$release_id}"
payload_name=".hololive-host-native-${AP_NAME}-${release_id}"
version="${HOLO_BOT_VERSION:-$(git -C "$REPO_ROOT" rev-parse --short=12 HEAD)}"

write_host_env() {
  local dest="$1"
  {
    printf 'APP_ENV=production\n'
    printf 'NOTIFICATION_EGRESS_ROLE=off\n'
    printf 'YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true\n'
    printf 'YOUTUBE_COLLECTOR_INSTANCE_ID=%s\n' "$service"
    printf 'PGAPPNAME=%s\n' "$service"
    printf 'YOUTUBE_COLLECTOR_LOG_FILE_NAME=%s.log\n' "$service"
    printf 'STACK_WORKER_PROFILE_FILE=/opt/hololive-bot/youtube-collector/releases/%s/worker-profile.json\n' "$release_id"
    printf 'STACK_WORKER_PROFILE_SOURCE_FILE=/etc/stack-secrets/hololive-bot/worker-profiles/%s.json\n' "$service"
    printf 'SERVER_PORT=%s\n' "$port"
    printf 'HOLOLIVE_HTTP_TRANSPORTS=h3\n'
    printf 'HOLOLIVE_H3_ADDR=127.0.0.1:%s\n' "$port"
    printf 'HOLOLIVE_H3_CERT_FILE=/etc/stack-secrets/hololive-bot/certs/hololive-h3.crt\n'
    printf 'HOLOLIVE_H3_KEY_FILE=/etc/stack-secrets/hololive-bot/certs/hololive-h3.key\n'
    printf 'HOLOLIVE_INTERNAL_H3_CA_CERT_FILE=/etc/stack-secrets/hololive-bot/certs/hololive-h3.crt\n'
    printf 'HOLOLIVE_INTERNAL_H3_SERVER_NAME=127.0.0.1\n'
    printf 'HOLOLIVE_METRICS_ADDR=%s:30096\n' "$AP_SSH_HOST"
    printf 'HEALTHCHECK_CA_CERT_FILE=/etc/stack-secrets/hololive-bot/certs/hololive-h3.crt\n'
    printf 'HEALTHCHECK_SERVER_NAME=127.0.0.1\n'
    printf 'PHOTO_SYNC_ENABLED=false\n'
    printf 'YOUTUBEJS_NODE=/usr/bin/node\n'
    printf 'YOUTUBEJS_SCRIPT=/opt/hololive-bot/youtube-collector/current/youtubejs/src/server.mjs\n'
    printf 'POSTGRES_USER=hololive_scraper\n'
    printf 'POSTGRES_HOST=%s\n' "$AP_POSTGRES_HOST"
    printf 'POSTGRES_PORT=%s\n' "$AP_POSTGRES_PORT"
    printf 'POSTGRES_DB=hololive\n'
    printf 'POSTGRES_SSLMODE=verify-full\n'
    printf 'POSTGRES_SSLROOTCERT=/etc/stack-secrets/hololive-bot/certs/postgres-ca.pem\n'
    printf 'POSTGRES_QUERY_EXEC_MODE=cache_statement\n'
    printf 'POSTGRES_POOL_MIN_CONNS=2\n'
    printf 'POSTGRES_POOL_MAX_CONNS=%s\n' "$AP_POSTGRES_POOL_MAX_CONNS"
    printf 'POSTGRES_SOCKET_PATH=\n'
    # 물리 메모리가 956MiB인 호스트라 예산식(768M−Node 192MiB−여유 32MiB)보다 낮게 둔다.
    printf 'GOMEMLIMIT=384MiB\n'
    printf 'GOGC=100\n'
    printf 'GIN_MODE=release\n'
    printf 'LOG_DIR=/var/log/hololive-bot\n'
    printf 'LOG_LEVEL=info\n'
  } > "$dest"
}

write_wrapper() {
  local dest="$1"
  cp "$COLLECTOR_WRAPPER_LIB" "$dest"
  chmod 0755 "$dest"
}

mkdir -p "$artifact_dir/bin"
artifact_dir="$(cd "$artifact_dir" && pwd)"
cp "$REPO_ROOT/scripts/deploy/lib/ap-host-native-release-path.sh" "$artifact_dir/bin/ap-host-native-release-path.sh"
cp "$REPO_ROOT/scripts/deploy/lib/ap-host-native-po.sh" "$artifact_dir/bin/ap-host-native-po.sh"
cp "$REPO_ROOT/scripts/deploy/lib/ap-host-native-cutover.sh" "$artifact_dir/bin/ap-host-native-cutover.sh"
cp "$REPO_ROOT/scripts/deploy/lib/ap-host-native-rollback-check.sh" "$artifact_dir/bin/ap-host-native-rollback-check.sh"
cp "$REPO_ROOT/scripts/deploy/lib/worker-profile-pair.py" "$artifact_dir/bin/worker-profile-pair.py"

native_revision="$(deploy_source_revision "$REPO_ROOT")"
sh "$REPO_ROOT/scripts/build/build-youtube-collector-go.sh" \
  --output-dir "$artifact_dir" \
  --version "$version" \
  --revision "$native_revision" \
  --goos linux \
  --goarch amd64 \
  --goamd64 "$AP_GOAMD64"
sh "$REPO_ROOT/scripts/build/check-youtube-collector-go-artifact.sh" "$artifact_dir" \
  --version "$version" \
  --revision "$native_revision" \
  --goos linux \
  --goarch amd64 \
  --goamd64 "$AP_GOAMD64"
issuer_artifact="$artifact_dir/po-sandbox-build"
"$REPO_ROOT/scripts/build/build-po-sandbox-artifact.sh" amd64 "$native_revision" "$version" "$issuer_artifact"
mkdir -p "$artifact_dir/po-sandbox"
cp "$issuer_artifact/rootfs.tar" "$issuer_artifact/rootfs.tar.sha256" \
  "$issuer_artifact/rootfs-manifest.json" "$issuer_artifact/revision" \
  "$issuer_artifact/architecture" "$issuer_artifact/version" "$artifact_dir/po-sandbox/"
cp "$REPO_ROOT/scripts/build/po-sandbox-manifest.py" "$artifact_dir/bin/po-sandbox-manifest.py"
rm -rf "$issuer_artifact"
write_wrapper "$artifact_dir/bin/youtube-collector-wrapper"
rm -rf "$artifact_dir/youtubejs"
mkdir -p "$artifact_dir/youtubejs"
cp "$REPO_ROOT/hololive/hololive-youtube-collector/youtubejs/package.json" \
  "$REPO_ROOT/hololive/hololive-youtube-collector/youtubejs/package-lock.json" \
  "$artifact_dir/youtubejs/"
cp -R "$REPO_ROOT/hololive/hololive-youtube-collector/youtubejs/src" "$artifact_dir/youtubejs/src"
rm -f "$artifact_dir/youtubejs/src/"*.test.mjs
(
  cd "$artifact_dir/youtubejs"
  npm_config_engine_strict=true npm exec --yes --package=corepack@0.36.0 -- corepack npm ci --omit=dev --no-audit --no-fund --ignore-scripts
)
write_host_env "$artifact_dir/youtube-collector-host.env"
cp "$UNIT_TEMPLATE" "$artifact_dir/hololive-youtube-collector@.service"
sed "s/@RELEASE_ID@/$release_id/g" "$PO_UNIT_TEMPLATE" > "$artifact_dir/hololive-youtube-po.service"
cp "$PO_SOCKET_TEMPLATE" "$artifact_dir/hololive-youtube-po.socket"

RSYNC_RSH="$(ap_rsync_rsh)"

if [[ "$MODE" == "--dry-run" ]]; then
  rsync -ani --delete "$artifact_dir/" -e "$RSYNC_RSH" "$(ap_rsync_target "./$payload_name/")"
  echo "[DRY-RUN] Built $artifact_dir; no remote files or services changed."
  exit 0
fi

rsync -ai --delete "$artifact_dir/" -e "$RSYNC_RSH" "$(ap_rsync_target "./$payload_name/")"
change_started_at="$(ap_remote_bash <<'REMOTE'
date -u +%Y-%m-%dT%H:%M:%SZ
REMOTE
)"

{
  cat "$REPO_ROOT/scripts/deploy/lib/ap-host-native-release-path.sh"
  cat "$REPO_ROOT/scripts/deploy/lib/ap-host-native-po.sh"
  cat "$REPO_ROOT/scripts/deploy/lib/ap-host-native-cutover.sh"
  cat "$REPO_ROOT/scripts/deploy/lib/ap-host-native-rollback-check.sh"
  cat "$NODE_VERSION_LIB"
  cat "$READINESS_LIB"
  cat "$REMOTE_APPLY_LIB"
} | ap_remote_bash "$payload_name" "$release_id" "$service" "$port" "$change_started_at" "$AP_REQUIRED_UDP_BUFFER_BYTES" "$AP_SWAPFILE_SIZE_MIB" "$native_revision"

CHANGE_STARTED_AT="$change_started_at" "$REPO_ROOT/scripts/logs/ap-host-native-status.sh" "$AP_NAME"
CHANGE_STARTED_AT="$change_started_at" "$REPO_ROOT/scripts/deploy/ap-completion-check.sh" "$AP_NAME"
