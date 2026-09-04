# shellcheck shell=bash
set -Eeuo pipefail
payload_name="$1"
release_id="$2"
service="$3"
port="$4"
change_started_at="$5"
required_udp_buffer="$6"
swapfile_size_mib="$7"
payload="$HOME/$payload_name"
release_path_lib="$payload/bin/ap-host-native-release-path.sh"
releases_root="/opt/hololive-bot/youtube-collector/releases"
current_link="/opt/hololive-bot/youtube-collector/current"
previous_link="/opt/hololive-bot/youtube-collector/previous"
host_env="/etc/hololive-bot/youtube-collector-host.env"
unit_file="/etc/systemd/system/hololive-youtube-collector@.service"
unit="hololive-youtube-collector@${service}.service"
worker_profile="/etc/stack-secrets/hololive-bot/worker-profiles/${service}.json"
producer_state_file="$releases_root/first-cutover-producer.state"
swapfile="/swapfile"

normalize_runtime_payload_permissions() {
  local root="$1"
  local required

  for required in \
    "$root/internal" \
    "$root/internal/domain" \
    "$root/internal/domain/data" \
    "$root/youtubejs"; do
    sudo -n test -d "$required" || return
    sudo -n test ! -L "$required" || return
  done

  # 서비스 계정은 root 소유 release의 정적 데이터와 helper graph를 읽고 순회할 수 있어야 한다.
  sudo -n chmod a+rx -- "$root/internal" "$root/internal/domain" || return
  sudo -n chmod -R -P a+rX -- "$root/internal/domain/data" "$root/youtubejs"
}

test -r "$release_path_lib"
# 검증한 release payload 내부의 동적 경로만 source합니다.
# shellcheck disable=SC1090
. "$release_path_lib"

if ! getent group opc >/dev/null; then
  sudo -n groupadd --system opc
fi
if ! id hololive >/dev/null 2>&1; then
  sudo -n useradd --system --gid opc --home-dir /nonexistent --shell /usr/sbin/nologin hololive
fi

sudo -n test -r /etc/stack-secrets/hololive-bot/youtube-collector.env
sudo -n test -r "$worker_profile"
if sudo -n grep -Eq '^CACHE_(PASSWORD|HOST|PORT|DB|SOCKET_PATH)=' /etc/stack-secrets/hololive-bot/youtube-collector.env; then
  echo "collector-scoped env must not contain Valkey/cache configuration" >&2
  exit 1
fi
sudo -n test -r /etc/stack-secrets/hololive-bot/certs/postgres-ca.pem
sudo -n test -r /etc/stack-secrets/hololive-bot/certs/hololive-h3.crt
sudo -n test -r /etc/stack-secrets/hololive-bot/certs/hololive-h3.key
require_node_version /usr/bin/node

sudo -n install -d -m 0755 -o root -g root "$releases_root"
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

old_target=""
if [[ -L "$current_link" ]]; then
  old_target="$(readlink -f "$current_link" || true)"
fi
if [[ -z "$old_target" ]]; then
  write_retired_producer_runtime_state "$service" > "$payload/first-cutover-producer.state"
  validate_retired_producer_runtime_state "$payload/first-cutover-producer.state" "$service"
  sudo -n install -m 0644 -o root -g root "$payload/first-cutover-producer.state" "$producer_state_file"
fi
release_dir="$(native_release_dir_resolve "$releases_root" "$release_id" "$current_link")"

sudo -n rm -rf "$release_dir"
sudo -n mkdir -p "$release_dir"
sudo -n rsync -a --delete "$payload/" "$release_dir/"
sudo -n chown -R -P root:root "$release_dir"
normalize_runtime_payload_permissions "$release_dir"
sudo -n chmod 0755 "$release_dir" "$release_dir/bin" "$release_dir/bin/youtube-collector" "$release_dir/bin/healthcheck" "$release_dir/bin/youtube-collector-wrapper"
sudo -n -u hololive env STACK_WORKER_PROFILE_FILE="$worker_profile" \
  "$release_dir/bin/youtube-collector" --check-worker-profile

if [[ -n "$old_target" && -d "$old_target" ]]; then
  sudo -n test -r "$host_env"
  sudo -n test -r "$unit_file"
  sudo -n test -x "$old_target/bin/youtube-collector"
  sudo -n test -x "$old_target/bin/youtube-collector-wrapper"
  sudo -n test -x "$old_target/bin/healthcheck"
  sudo -n test -d "$old_target/internal/domain/data"
  sudo -n test -f "$old_target/youtubejs/src/server.mjs"
  rollback_contract_dir="$old_target/rollback-contract"
  sudo -n install -d -m 0755 -o root -g root "$rollback_contract_dir"
  sudo -n install -m 0640 -o root -g root "$host_env" "$rollback_contract_dir/youtube-collector-host.env"
  sudo -n install -m 0644 -o root -g root "$unit_file" "$rollback_contract_dir/hololive-youtube-collector@.service"
  sudo -n sh -c '
    set -eu
    cd "$1"
    {
      sha256sum \
        bin/youtube-collector \
        bin/youtube-collector-wrapper \
        bin/healthcheck \
        rollback-contract/youtube-collector-host.env \
        rollback-contract/hololive-youtube-collector@.service
      find internal/domain/data youtubejs/src -type f -print0 | LC_ALL=C sort -z | xargs -0 -r sha256sum
    } > rollback-contract/SHA256SUMS
    chmod 0644 rollback-contract/SHA256SUMS
  ' sh "$old_target"
  sudo -n ln -sfn "$old_target" "$previous_link"
fi

restore_native_after_failed_cutover() {
  local status="$?"
  local restore_status=0
  trap - ERR
  if ! (
    set -e
    stop_named_units_and_require_inactive "$unit"
    if [[ -n "$old_target" && -d "$old_target" ]]; then
      rollback_contract_dir="$old_target/rollback-contract"
      sudo -n install -m 0640 -o root -g root "$rollback_contract_dir/youtube-collector-host.env" "$host_env"
      sudo -n install -m 0644 -o root -g root "$rollback_contract_dir/hololive-youtube-collector@.service" "$unit_file"
      sudo -n ln -sfn "$old_target" "$current_link"
      sudo -n systemctl daemon-reload
      sudo -n systemctl enable --now "$unit"
    else
      sudo -n rm -f "$current_link" "$host_env" "$unit_file"
      sudo -n systemctl daemon-reload
      restore_retired_producer_runtime "$producer_state_file" "$service"
    fi
  ); then
    restore_status=1
  fi
  if [[ "$restore_status" -ne 0 ]]; then
    echo "host-native collector cutover failed and the recorded runtime could not be restored" >&2
  fi
  exit "$status"
}
trap restore_native_after_failed_cutover ERR

sudo -n install -m 0640 -o root -g root "$payload/youtube-collector-host.env" "$host_env"
sudo -n install -m 0644 -o root -g root "$payload/hololive-youtube-collector@.service" "$unit_file"
sudo -n ln -sfn "$release_dir" "$current_link"

sudo -n systemd-analyze verify "$unit_file"
sudo -n systemctl daemon-reload
stop_retired_producer_runtime "$service"
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
     HEALTHCHECK_CA_CERT_FILE=/etc/stack-secrets/hololive-bot/certs/hololive-h3.crt \
     HEALTHCHECK_SERVER_NAME=127.0.0.1 \
     "$current_link/bin/healthcheck" "https://127.0.0.1:${port}/health"; then
    break
  fi
  sleep 2
done

collector_readiness_fetch() {
  systemctl is-active --quiet "$unit" || return 1
  sudo -n -u hololive env \
    HEALTHCHECK_CA_CERT_FILE=/etc/stack-secrets/hololive-bot/certs/hololive-h3.crt \
    HEALTHCHECK_SERVER_NAME=127.0.0.1 \
    "$current_link/bin/healthcheck" --body "https://127.0.0.1:${port}/ready"
}
ready="$(collector_readiness_poll 90 2 collector_readiness_fetch)"
printf '%s\n' "$ready"
collector_readiness_validate "$ready"

journal_since="${change_started_at/T/ }"
journal_since="${journal_since%Z} UTC"
if ! journal_output="$(journalctl -u "$unit" --since "$journal_since" --no-pager)"; then
  echo "failed to read post-cutover logs for $unit" >&2
  exit 1
fi
printf '%s\n' "$journal_output" |
  grep -E 'PostgreSQL|Valkey|active_active|ERR|panic|permission denied|x509|no such file' || true
if grep -E 'ERR|panic|permission denied|x509|no such file' <<<"$journal_output"; then
  exit 1
fi
trap - ERR
