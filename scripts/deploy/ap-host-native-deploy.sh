#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
MODE="${2:---dry-run}"
AP_REQUIRED_UDP_BUFFER_BYTES="${AP_REQUIRED_UDP_BUFFER_BYTES:-7500000}"
AP_SWAPFILE_SIZE_MIB="${AP_SWAPFILE_SIZE_MIB:-2048}"

case "$MODE" in
  --dry-run|--apply) ;;
  *)
    echo "Usage: $0 <ap-host> [--dry-run|--apply]" >&2
    exit 2
    ;;
esac

. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"
ap_host_load "$REPO_ROOT" "${1:-}"

if [[ ! "$AP_REQUIRED_UDP_BUFFER_BYTES" =~ ^[0-9]+$ ]]; then
  echo "AP_REQUIRED_UDP_BUFFER_BYTES must be an integer" >&2
  exit 2
fi
if [[ ! "$AP_SWAPFILE_SIZE_MIB" =~ ^[1-9][0-9]*$ ]]; then
  echo "AP_SWAPFILE_SIZE_MIB must be a positive integer" >&2
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
artifact_dir="${ARTIFACT_DIR:-$REPO_ROOT/artifacts/ap-host-native/$release_id}"
payload_name=".hololive-host-native-${AP_NAME}-${release_id}"
version="${HOLO_BOT_VERSION:-$(git -C "$REPO_ROOT" rev-parse --short=12 HEAD)}"

write_host_env() {
  local dest="$1"
  {
    printf 'APP_ENV=production\n'
    printf 'NOTIFICATION_EGRESS_ROLE=producer\n'
    printf 'YOUTUBE_PRODUCER_RUNTIME_ALLOWED=true\n'
    printf 'YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED=true\n'
    printf 'YOUTUBE_PRODUCER_LEASE_NAMESPACE=production\n'
    printf 'YOUTUBE_PRODUCER_INSTANCE_ID=%s\n' "$service"
    printf 'YOUTUBE_PRODUCER_LOG_FILE_NAME=%s.log\n' "$service"
    printf 'YOUTUBE_OUTBOX_DISPATCHER_ENABLED=false\n'
    printf 'YOUTUBE_INGESTION_ENABLED=true\n'
    printf 'YOUTUBE_COMMUNITY_SHORTS_BIGBANG_ENABLED=true\n'
    printf 'SERVER_PORT=%s\n' "$port"
    printf 'HOLOLIVE_HTTP_TRANSPORTS=h3\n'
    printf 'HOLOLIVE_H3_ADDR=:%s\n' "$port"
    printf 'HOLOLIVE_H3_CERT_FILE=/run/hololive-bot/certs/hololive-h3.crt\n'
    printf 'HOLOLIVE_H3_KEY_FILE=/run/hololive-bot/certs/hololive-h3.key\n'
    printf 'HOLOLIVE_H3_SERVER_NAME=127.0.0.1\n'
    printf 'HOLOLIVE_INTERNAL_H3_CA_CERT_FILE=/run/hololive-bot/certs/hololive-h3.crt\n'
    printf 'HOLOLIVE_INTERNAL_H3_SERVER_NAME=127.0.0.1\n'
    printf 'HOLOLIVE_METRICS_ADDR=%s:30095\n' "$AP_SSH_HOST"
    printf 'HEALTHCHECK_CA_CERT_FILE=/run/hololive-bot/certs/hololive-h3.crt\n'
    printf 'HEALTHCHECK_SERVER_NAME=127.0.0.1\n'
    printf 'PHOTO_SYNC_ENABLED=false\n'
    printf 'SCRAPER_FETCHER_ENGINE=nethttp\n'
    printf 'SCRAPER_SCHEDULER_WORKER_COUNT=1\n'
    printf 'SCRAPER_BACKFILL_ENABLED=false\n'
    printf 'YOUTUBE_PRODUCER_REQUEST_INTERVAL_SECONDS=2\n'
    printf 'POSTGRES_HOST=100.100.1.3\n'
    printf 'POSTGRES_PORT=5433\n'
    printf 'POSTGRES_DB=hololive\n'
    printf 'POSTGRES_SSLMODE=verify-full\n'
    printf 'POSTGRES_SSLROOTCERT=/run/hololive-bot/certs/postgres-ca.pem\n'
    printf 'POSTGRES_QUERY_EXEC_MODE=exec\n'
    printf 'POSTGRES_AUTO_PREPARE_SCHEMA=false\n'
    printf 'POSTGRES_POOL_MIN_CONNS=2\n'
    printf 'POSTGRES_POOL_MAX_CONNS=8\n'
    printf 'POSTGRES_SOCKET_PATH=\n'
    printf 'CACHE_HOST=100.100.1.3\n'
    printf 'CACHE_PORT=6379\n'
    printf 'CACHE_SOCKET_PATH=\n'
    printf 'CLIPROXY_BASE_URL=http://100.100.1.3:8787/v1\n'
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

if [ -z "${POSTGRES_PASSWORD:-}" ] && [ -n "${DB_PASSWORD:-}" ]; then
  export POSTGRES_PASSWORD="$DB_PASSWORD"
fi
if [ -z "${POSTGRES_USER:-}" ]; then
  export POSTGRES_USER="${HOLOLIVE_DB_USER:-hololive_runtime}"
fi
if [ -z "${POSTGRES_DB:-}" ]; then
  export POSTGRES_DB=hololive
fi
exec /opt/hololive-bot/youtube-producer/current/bin/youtube-producer
EOF
  chmod 0755 "$dest"
}

write_unit() {
  local dest="$1"
  cat > "$dest" <<'EOF'
[Unit]
Description=Hololive youtube-producer AP (%i)
After=network-online.target openbao-agent-hololive-bot.service
Wants=network-online.target
Requires=openbao-agent-hololive-bot.service

[Service]
Type=simple
User=hololive
Group=opc
WorkingDirectory=/opt/hololive-bot/youtube-producer/current
EnvironmentFile=/run/hololive-bot/ap-compose.env
EnvironmentFile=/run/hololive-bot/youtube-producer.env
EnvironmentFile=/etc/hololive-bot/youtube-producer-host.env
ExecStart=/opt/hololive-bot/youtube-producer/current/bin/youtube-producer-wrapper
Restart=always
RestartSec=5s
TimeoutStopSec=30s
MemoryMax=768M
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=/run/hololive-bot /var/log/hololive-bot /tmp

[Install]
WantedBy=multi-user.target
EOF
}

mkdir -p "$artifact_dir/bin" "$artifact_dir/internal/domain"

(
  cd "$REPO_ROOT/hololive/hololive-youtube-producer"
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64="${GOAMD64:-v1}" \
    go build -tags sonic -trimpath -buildvcs=false \
      -ldflags="-s -w -buildid= -X main.Version=$version" \
      -o "$artifact_dir/bin/youtube-producer" ./cmd/runtime/youtube-producer
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64="${GOAMD64:-v1}" \
    go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" \
      -o "$artifact_dir/bin/healthcheck" ./cmd/runtime/healthcheck
)
write_wrapper "$artifact_dir/bin/youtube-producer-wrapper"
rm -rf "$artifact_dir/internal/domain/data"
cp -R "$REPO_ROOT/hololive/hololive-shared/pkg/domain/internal/model/data" "$artifact_dir/internal/domain/data"
write_host_env "$artifact_dir/youtube-producer-host.env"
write_unit "$artifact_dir/hololive-youtube-producer@.service"

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

ap_remote_bash "$payload_name" "$release_id" "$service" "$port" "$change_started_at" "$AP_REQUIRED_UDP_BUFFER_BYTES" "$AP_SWAPFILE_SIZE_MIB" <<'REMOTE'
set -euo pipefail
payload_name="$1"
release_id="$2"
service="$3"
port="$4"
change_started_at="$5"
required_udp_buffer="$6"
swapfile_size_mib="$7"
payload="$HOME/$payload_name"
release_dir="/opt/hololive-bot/youtube-producer/releases/$release_id"
current_link="/opt/hololive-bot/youtube-producer/current"
previous_link="/opt/hololive-bot/youtube-producer/previous"
unit="hololive-youtube-producer@${service}.service"
swapfile="/swapfile"

if ! getent group opc >/dev/null; then
  sudo -n groupadd --system opc
fi
if ! id hololive >/dev/null 2>&1; then
  sudo -n useradd --system --gid opc --home-dir /nonexistent --shell /usr/sbin/nologin hololive
fi

sudo -n test -r /run/hololive-bot/ap-compose.env
sudo -n test -r /run/hololive-bot/youtube-producer.env
sudo -n test -r /run/hololive-bot/certs/postgres-ca.pem
sudo -n test -r /run/hololive-bot/certs/hololive-h3.crt
sudo -n test -r /run/hololive-bot/certs/hololive-h3.key

sudo -n install -d -m 0755 -o root -g root /opt/hololive-bot/youtube-producer/releases
sudo -n install -d -m 0750 -o hololive -g opc /var/log/hololive-bot /var/log/hololive-bot/archive
sudo -n install -d -m 0750 -o root -g root /etc/hololive-bot
sudo -n install -d -m 0755 -o root -g root /etc/sysctl.d
sudo -n tee /etc/logrotate.d/hololive-bot >/dev/null <<'LOGROTATE'
/var/log/hololive-bot/*.log {
    daily
    rotate 14
    size 10M
    missingok
    notifempty
    olddir /var/log/hololive-bot/archive
    compress
    delaycompress
    copytruncate
    create 0640 hololive opc
}
LOGROTATE
if command -v logrotate >/dev/null 2>&1; then
  sudo -n logrotate -d /etc/logrotate.d/hololive-bot >/dev/null
fi
sudo -n tee /etc/sysctl.d/99-hololive-quic-udp-buffer.conf >/dev/null <<SYSCTL
net.core.rmem_max = ${required_udp_buffer}
net.core.wmem_max = ${required_udp_buffer}
SYSCTL
sudo -n sysctl -w "net.core.rmem_max=${required_udp_buffer}" "net.core.wmem_max=${required_udp_buffer}" >/dev/null
if ! sudo -n test -f "$swapfile"; then
  if command -v fallocate >/dev/null 2>&1; then
    sudo -n fallocate -l "${swapfile_size_mib}M" "$swapfile" || sudo -n dd if=/dev/zero of="$swapfile" bs=1M count="$swapfile_size_mib" status=none
  else
    sudo -n dd if=/dev/zero of="$swapfile" bs=1M count="$swapfile_size_mib" status=none
  fi
fi
sudo -n chown root:root "$swapfile"
sudo -n chmod 600 "$swapfile"
if ! sudo -n file "$swapfile" | grep -q 'swap file'; then
  sudo -n mkswap "$swapfile" >/dev/null
fi
if ! swapon --noheadings --show=NAME | grep -Fxq "$swapfile"; then
  sudo -n swapon "$swapfile"
fi
if ! sudo -n grep -Eq '^/swapfile[[:space:]]+none[[:space:]]+swap[[:space:]]+' /etc/fstab; then
  printf '/swapfile none swap sw 0 0\n' | sudo -n tee -a /etc/fstab >/dev/null
fi
sudo -n tee /etc/sysctl.d/99-hololive-swap.conf >/dev/null <<'SYSCTL'
vm.swappiness = 10
SYSCTL
sudo -n sysctl -w vm.swappiness=10 >/dev/null
sudo -n rm -rf "$release_dir"
sudo -n mkdir -p "$release_dir"
sudo -n rsync -a --delete "$payload/" "$release_dir/"
sudo -n chown -R root:root "$release_dir"
sudo -n chmod 0755 "$release_dir" "$release_dir/bin" "$release_dir/bin/youtube-producer" "$release_dir/bin/healthcheck" "$release_dir/bin/youtube-producer-wrapper"
sudo -n install -m 0640 -o root -g root "$payload/youtube-producer-host.env" /etc/hololive-bot/youtube-producer-host.env
sudo -n install -m 0644 -o root -g root "$payload/hololive-youtube-producer@.service" /etc/systemd/system/hololive-youtube-producer@.service

if [[ -L "$current_link" ]]; then
  old_target="$(readlink -f "$current_link" || true)"
  if [[ -n "$old_target" && -d "$old_target" ]]; then
    sudo -n ln -sfn "$old_target" "$previous_link"
  fi
fi
sudo -n ln -sfn "$release_dir" "$current_link"

sudo -n systemd-analyze verify /etc/systemd/system/hololive-youtube-producer@.service
sudo -n systemctl daemon-reload
sudo -n systemctl enable --now "$unit"
sudo -n systemctl restart "$unit"

since_epoch="$(date -u -d "$change_started_at" +%s)"
for _ in $(seq 1 30); do
  active_state="$(systemctl show "$unit" -p ActiveState --value)"
  [[ "$active_state" == active ]] && break
  sleep 2
done
active_enter="$(systemctl show "$unit" -p ActiveEnterTimestamp --value)"
active_epoch="$(date -u -d "$active_enter" +%s)"
[[ "$active_epoch" -ge "$since_epoch" ]]

systemctl show "$unit" -p ActiveState -p SubState -p ExecMainPID -p MemoryCurrent -p NRestarts -p ActiveEnterTimestamp
printf 'net.core.rmem_max=%s\n' "$(sysctl -n net.core.rmem_max)"
printf 'net.core.wmem_max=%s\n' "$(sysctl -n net.core.wmem_max)"

for _ in $(seq 1 30); do
  if sudo -n -u hololive env \
     HEALTHCHECK_CA_CERT_FILE=/run/hololive-bot/certs/hololive-h3.crt \
     HEALTHCHECK_SERVER_NAME=127.0.0.1 \
     "$current_link/bin/healthcheck" "https://127.0.0.1:${port}/health"; then
    break
  fi
  sleep 2
done

ready="$(
  sudo -n -u hololive env \
  HEALTHCHECK_CA_CERT_FILE=/run/hololive-bot/certs/hololive-h3.crt \
  HEALTHCHECK_SERVER_NAME=127.0.0.1 \
  "$current_link/bin/healthcheck" --body "https://127.0.0.1:${port}/ready"
)"
printf '%s\n' "$ready"
printf '%s' "$ready" | grep -q '"mode":"active-active"'
printf '%s' "$ready" | grep -q '"valkey_available":true'
printf '%s' "$ready" | grep -q '"scraping_paused":false'

journal_since="${change_started_at/T/ }"
journal_since="${journal_since%Z} UTC"
journalctl -u "$unit" --since "$journal_since" --no-pager |
  grep -E 'PostgreSQL|Valkey|active_active|ERR|panic|permission denied|x509|no such file' || true
if journalctl -u "$unit" --since "$journal_since" --no-pager |
   grep -E 'ERR|panic|permission denied|x509|no such file'; then
  exit 1
fi
REMOTE

CHANGE_STARTED_AT="$change_started_at" "$REPO_ROOT/scripts/logs/ap-host-native-status.sh" "$AP_NAME"
