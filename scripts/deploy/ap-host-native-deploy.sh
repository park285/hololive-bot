#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
MODE="${2:---dry-run}"
AP_REQUIRED_UDP_BUFFER_BYTES="${AP_REQUIRED_UDP_BUFFER_BYTES:-7500000}"
AP_SWAPFILE_SIZE_MIB="${AP_SWAPFILE_SIZE_MIB:-2048}"
AP_CENTRAL_HOST="${AP_CENTRAL_HOST:-100.100.1.8}"
AP_POSTGRES_HOST="${AP_POSTGRES_HOST:-hololive-postgres.tail742dd8.ts.net}"
AP_POSTGRES_PORT="${AP_POSTGRES_PORT:-5433}"
AP_CLIPROXY_HOST="${AP_CLIPROXY_HOST:-100.100.1.3}"

case "$MODE" in
  --dry-run|--apply) ;;
  *)
    echo "Usage: $0 <ap-host> [--dry-run|--apply]" >&2
    exit 2
    ;;
esac

. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"
. "$REPO_ROOT/scripts/deploy/lib/ap-host-native-release-path.sh"
NODE_VERSION_LIB="$REPO_ROOT/scripts/deploy/lib/youtubejs-node-version.sh"
RETIRED_PRODUCER_LIB="$REPO_ROOT/scripts/deploy/lib/retired-producer-cutover.sh"
REMOTE_APPLY_LIB="$REPO_ROOT/scripts/deploy/lib/ap-host-native-remote-apply.sh"
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

if [[ "$MODE" == "--apply" && "${!AP_APPROVE_DEPLOY_VAR:-}" != "true" ]]; then
  echo "Refusing apply without $AP_APPROVE_DEPLOY_VAR=true" >&2
  exit 2
fi

service="${AP_SERVICES[0]}"
port="${AP_PORTS[0]}"
release_id="${RELEASE_ID:-$(date -u +%Y%m%dT%H%M%SZ)-$(git -C "$REPO_ROOT" rev-parse --short=12 HEAD)-$AP_NAME}"
native_release_id_validate "$release_id"
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
    printf 'YOUTUBE_COLLECTOR_LOG_FILE_NAME=%s.log\n' "$service"
    printf 'YOUTUBE_OUTBOX_DISPATCHER_ENABLED=false\n'
    printf 'YOUTUBE_INGESTION_ENABLED=true\n'
    printf 'SERVER_PORT=%s\n' "$port"
    printf 'HOLOLIVE_HTTP_TRANSPORTS=h3\n'
    printf 'HOLOLIVE_H3_ADDR=127.0.0.1:%s\n' "$port"
    printf 'HOLOLIVE_H3_CERT_FILE=/etc/stack-secrets/hololive-bot/certs/hololive-h3.crt\n'
    printf 'HOLOLIVE_H3_KEY_FILE=/etc/stack-secrets/hololive-bot/certs/hololive-h3.key\n'
    printf 'HOLOLIVE_H3_SERVER_NAME=127.0.0.1\n'
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
    printf 'POSTGRES_POOL_MAX_CONNS=8\n'
    printf 'POSTGRES_SOCKET_PATH=\n'
    printf 'CACHE_HOST=%s\n' "${AP_CACHE_HOST:-$AP_CENTRAL_HOST}"
    printf 'CACHE_PORT=6379\n'
    printf 'CACHE_SOCKET_PATH=\n'
    printf 'SETTINGS_DIR=/var/lib/hololive-bot/youtube-collector/settings\n'
    printf 'GOMEMLIMIT=384MiB\n'
    printf 'GOGC=100\n'
    printf 'GIN_MODE=release\n'
    printf 'LOG_DIR=/var/log/hololive-bot\n'
    printf 'LOG_LEVEL=info\n'
  } > "$dest"
}

write_wrapper() {
  local dest="$1"
  cat > "$dest" <<'EOF'
#!/usr/bin/env sh
set -eu

if [ -z "${POSTGRES_USER:-}" ]; then
  export POSTGRES_USER="${HOLOLIVE_SCRAPER_USER:-hololive_scraper}"
fi
if [ -z "${POSTGRES_DB:-}" ]; then
  export POSTGRES_DB=hololive
fi
if [ -z "${POSTGRES_PASSWORD:-}" ] &&
   [ "$POSTGRES_USER" = "${HOLOLIVE_SCRAPER_USER:-hololive_scraper}" ] &&
   [ -n "${HOLOLIVE_SCRAPER_PASSWORD:-}" ]; then
  export POSTGRES_PASSWORD="$HOLOLIVE_SCRAPER_PASSWORD"
elif [ -z "${POSTGRES_PASSWORD:-}" ] && [ -n "${HOLOLIVE_DB_PASSWORD:-}" ]; then
  export POSTGRES_PASSWORD="$HOLOLIVE_DB_PASSWORD"
elif [ -z "${POSTGRES_PASSWORD:-}" ] && [ -n "${DB_PASSWORD:-}" ]; then
  export POSTGRES_PASSWORD="$DB_PASSWORD"
fi
exec /opt/hololive-bot/youtube-collector/current/bin/youtube-collector
EOF
  chmod 0755 "$dest"
}

write_unit() {
  local dest="$1"
  cat > "$dest" <<'EOF'
[Unit]
Description=Hololive youtube-collector AP (%i)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=hololive
Group=opc
WorkingDirectory=/opt/hololive-bot/youtube-collector/current
EnvironmentFile=/etc/stack-secrets/hololive-bot/ap-compose.env
EnvironmentFile=/etc/stack-secrets/hololive-bot/youtube-collector.env
EnvironmentFile=/etc/hololive-bot/youtube-collector-host.env
ExecStart=/opt/hololive-bot/youtube-collector/current/bin/youtube-collector-wrapper
Restart=always
RestartSec=5s
TimeoutStopSec=30s
MemoryMax=768M
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=/var/log/hololive-bot /tmp
ReadWritePaths=/var/lib/hololive-bot

[Install]
WantedBy=multi-user.target
EOF
}

mkdir -p "$artifact_dir/bin" "$artifact_dir/internal/domain"
cp "$REPO_ROOT/scripts/deploy/lib/ap-host-native-release-path.sh" "$artifact_dir/bin/ap-host-native-release-path.sh"

(
  cd "$REPO_ROOT/hololive/hololive-youtube-collector"
  export GOWORK=off
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64="${GOAMD64:-v1}" \
    go build -tags sonic -trimpath -buildvcs=false \
      -ldflags="-s -w -buildid= -X main.Version=$version" \
      -o "$artifact_dir/bin/youtube-collector" ./cmd/runtime/youtube-collector
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64="${GOAMD64:-v1}" \
    go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" \
      -o "$artifact_dir/bin/healthcheck" ./cmd/runtime/healthcheck
)
write_wrapper "$artifact_dir/bin/youtube-collector-wrapper"
rm -rf "$artifact_dir/internal/domain/data"
cp -R "$REPO_ROOT/hololive/hololive-shared/pkg/domain/internal/model/data" "$artifact_dir/internal/domain/data"
rm -rf "$artifact_dir/youtubejs"
mkdir -p "$artifact_dir/youtubejs"
cp "$REPO_ROOT/hololive/hololive-youtube-collector/youtubejs/package.json" \
  "$REPO_ROOT/hololive/hololive-youtube-collector/youtubejs/package-lock.json" \
  "$artifact_dir/youtubejs/"
cp -R "$REPO_ROOT/hololive/hololive-youtube-collector/youtubejs/src" "$artifact_dir/youtubejs/src"
rm -f "$artifact_dir/youtubejs/src/"*.test.mjs
(
  cd "$artifact_dir/youtubejs"
  npm ci --omit=dev --no-audit --no-fund
)
write_host_env "$artifact_dir/youtube-collector-host.env"
write_unit "$artifact_dir/hololive-youtube-collector@.service"

RSYNC_RSH="ssh -F /dev/null -i $SSH_KEY -o IdentitiesOnly=yes"
if [[ -n "$AP_SSH_HOST_KEY_ALIAS" ]]; then
  RSYNC_RSH+=" -o HostKeyAlias=$AP_SSH_HOST_KEY_ALIAS"
fi

if [[ "$MODE" == "--dry-run" ]]; then
  rsync -ani --delete "$artifact_dir/" -e "$RSYNC_RSH" "ubuntu@$AP_SSH_HOST:~/$payload_name/"
  echo "[DRY-RUN] Built $artifact_dir; no remote files or services changed."
  exit 0
fi

rsync -ai --delete "$artifact_dir/" -e "$RSYNC_RSH" "ubuntu@$AP_SSH_HOST:~/$payload_name/"
change_started_at="$(ap_remote_bash <<'REMOTE'
date -u +%Y-%m-%dT%H:%M:%SZ
REMOTE
)"

{
  cat "$NODE_VERSION_LIB"
  cat "$RETIRED_PRODUCER_LIB"
  cat "$REMOTE_APPLY_LIB"
} | ap_remote_bash "$payload_name" "$release_id" "$service" "$port" "$change_started_at" "$AP_REQUIRED_UDP_BUFFER_BYTES" "$AP_SWAPFILE_SIZE_MIB"

CHANGE_STARTED_AT="$change_started_at" "$REPO_ROOT/scripts/logs/ap-host-native-status.sh" "$AP_NAME"
CHANGE_STARTED_AT="$change_started_at" "$REPO_ROOT/scripts/deploy/ap-completion-check.sh" "$AP_NAME"
