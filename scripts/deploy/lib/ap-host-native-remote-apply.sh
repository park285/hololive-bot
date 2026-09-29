# shellcheck shell=bash
set -Eeuo pipefail
payload_name="$1"
release_id="$2"
service="$3"
port="$4"
change_started_at="$5"
required_udp_buffer="$6"
swapfile_size_mib="$7"
EXPECTED_REVISION="$8"
[[ "$EXPECTED_REVISION" =~ ^[0-9a-f]{40}$ ]] || { echo 'full native release revision required' >&2; exit 1; }
# The preceding ap-host-native-po.sh fragment owns these values.
po_service="${po_service:?PO helper fragment must be loaded}"
po_socket="${po_socket:?PO helper fragment must be loaded}"
po_unit_file="${po_unit_file:?PO helper fragment must be loaded}"
po_socket_file="${po_socket_file:?PO helper fragment must be loaded}"
payload="$HOME/$payload_name"
release_path_lib="$payload/bin/ap-host-native-release-path.sh"
releases_root="/opt/hololive-bot/youtube-collector/releases"
current_link="/opt/hololive-bot/youtube-collector/current"
previous_link="/opt/hololive-bot/youtube-collector/previous"
host_env="/etc/hololive-bot/youtube-collector-host.env"
unit_file="/etc/systemd/system/hololive-youtube-collector@.service"
unit="hololive-youtube-collector@${service}.service"
po_apply_lib="$payload/bin/ap-host-native-po.sh"
worker_profile="/etc/stack-secrets/hololive-bot/worker-profiles/${service}.json"
swapfile="/swapfile"

normalize_runtime_payload_permissions() {
  local root="$1"
  sudo -n test -d "$root/youtubejs" || return
  sudo -n test ! -L "$root/youtubejs" || return

  # 서비스 계정은 root 소유 helper graph를 읽고 순회할 수 있어야 한다.
  # AP 호스트의 chmod는 -P를 지원하지 않으므로 find가 링크를 제외하고 순회합니다.
  sudo -n find -P "$root/youtubejs" \
    \( -type d -o -type f \) -exec chmod a+rX -- {} +
}

test -r "$release_path_lib"
test -r "$po_apply_lib"

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
old_previous_target=""
if [[ -L "$previous_link" ]]; then
  old_previous_target="$(readlink -f "$previous_link" || true)"
elif [[ -e "$previous_link" ]]; then
  echo 'previous release pointer is not a symlink' >&2
  exit 1
fi
if [[ -n "$old_previous_target" && ( "$old_previous_target" != "$releases_root/"* || ! -d "$old_previous_target" ) ]]; then
  echo 'previous release pointer is not an existing immutable release' >&2
  exit 1
fi
if [[ -z "$old_target" && ( -e "$po_unit_file" || -e "$po_socket_file" ) ]]; then
  echo 'unmanaged issuer units exist before initial install' >&2
  exit 1
fi
if [[ -z "$old_target" && -n "$old_previous_target" ]]; then
  echo 'refusing initial native install with stale previous release pointer' >&2
  exit 1
fi
release_dir="$(native_release_dir_resolve "$releases_root" "$release_id" "$current_link")"

sudo -n rm -rf "$release_dir"
sudo -n mkdir -p "$release_dir"
sudo -n rsync -a --delete "$payload/" "$release_dir/"
sudo -n chown -R -P root:root "$release_dir"
normalize_runtime_payload_permissions "$release_dir"
[[ "$(cat "$release_dir/po-sandbox/revision")" == "${EXPECTED_REVISION:?expected native source SHA missing}" ]]
po_install_root="$release_dir/po-sandbox/rootfs"
sudo -n mkdir -p "$po_install_root"
(cd "$release_dir/po-sandbox" && sudo -n sha256sum --check --strict rootfs.tar.sha256)
sudo -n tar -xf "$release_dir/po-sandbox/rootfs.tar" -C "$po_install_root" --no-same-owner --same-permissions
sudo -n chown -R -P root:root "$po_install_root"
po_validate_release "$release_dir"
sudo -n chmod 0755 "$release_dir" "$release_dir/bin" "$release_dir/bin/youtube-collector" "$release_dir/bin/healthcheck" "$release_dir/bin/youtube-collector-wrapper"
sudo -n -u hololive env STACK_WORKER_PROFILE_FILE="$worker_profile" \
  "$release_dir/bin/youtube-collector" --check-worker-profile

if [[ -n "$old_target" && -d "$old_target" ]]; then
  sudo -n test -r "$host_env"
  sudo -n test -r "$unit_file"
  sudo -n test -x "$old_target/bin/youtube-collector"
  sudo -n test -x "$old_target/bin/youtube-collector-wrapper"
  sudo -n test -x "$old_target/bin/healthcheck"
  sudo -n test -f "$old_target/youtubejs/src/server.mjs"
  rollback_contract_dir="$old_target/rollback-contract"
  sudo -n install -d -m 0755 -o root -g root "$rollback_contract_dir"
  po_snapshot_previous "$old_target"
  printf '%s\n' "${old_previous_target:-absent}" | sudo -n tee "$rollback_contract_dir/previous-before-cutover" >/dev/null
  sudo -n chmod 0644 "$rollback_contract_dir/previous-before-cutover"
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
      find youtubejs/src -type f -print0 | LC_ALL=C sort -z | xargs -0 -r sha256sum
      sha256sum rollback-contract/po-unit-presence
      sha256sum rollback-contract/previous-before-cutover
      if test -f rollback-contract/hololive-youtube-po.service; then
        sha256sum rollback-contract/hololive-youtube-po.service rollback-contract/hololive-youtube-po.socket
      fi
    } > rollback-contract/SHA256SUMS
    chmod 0644 rollback-contract/SHA256SUMS
  ' sh "$old_target"
  sudo -n ln -sfn "$old_target" "$previous_link"
fi

stop_collector_unit_and_require_inactive() {
  command -v systemctl >/dev/null 2>&1 || return 0
  systemctl cat "$unit" >/dev/null 2>&1 || return 0
  echo "[CUTOVER] Stopping collector unit: ${unit}"
  sudo -n systemctl disable --now "$unit" >/dev/null
  if systemctl is-active --quiet "$unit" 2>/dev/null; then
    echo "collector unit still active: $unit" >&2
    return 1
  fi
}

# PO issuer를 collector보다 먼저 멈춘다. collector는 종료하면서 소유 generation을 DELETE로 반납하는데, broker가 살아
# 있으면 retire 뒤 exit 0하고 Restart=always(RestartSec=1s)로 다시 떠서 곧바로 이 스크립트에 다시 멈춰진다.
# broker가 먼저 멈추면 그 DELETE는 broker_unavailable로 끝나고 collector는 재전송 없이 무시한다(proof-controller.mjs retireOwned).
# socket을 먼저 멈춰 service가 Requires=로 같은 transaction에서 멈추게 하고, 그 사이 socket activation이 service를 다시 띄우지 못하게 한다.
stop_native_units_and_require_inactive() {
  if sudo -n systemctl is-active --quiet "$po_socket"; then
    sudo -n systemctl stop "$po_socket"
  fi
  if sudo -n systemctl is-active --quiet "$po_service"; then
    sudo -n systemctl stop "$po_service"
  fi
  stop_collector_unit_and_require_inactive
}

# 실패하면 이전 collector release로 되돌린다. 이전 release가 없는 첫 설치는 반쯤 구성된 unit을 지우고 실패로 끝낸다.
# 퇴역 producer의 첫 cutover 상태를 기록·복원하던 경로는 T18(2026-09-26)에서 모든 host-native AP의 current·previous가
# collector release이고 producer unit이 0개임을 확인해 지웠다(stack-audit T11 holo-collector-retired-producer-cutover-tooling).
# set -E라 ERR trap은 명령 치환·subshell에도 상속된다. 그 안의 실패는 subshell에서 한 번, 치환이 실패로 끝난 부모에서
# 또 한 번 trap을 부르므로 trap을 건 shell에서만 복원하고 subshell은 원래 상태로 끝나 부모에 실패를 넘긴다.
restore_native_after_failed_cutover() {
  local status="$?"
  local restore_status
  if [[ "$BASHPID" != "${cutover_restore_owner_pid:?cutover restore owner not armed}" ]]; then
    exit "$status"
  fi
  trap - ERR
  # if/!/&&/|| 조건 안의 subshell은 set -e를 무시해 실패한 복원 단계를 지나친다. 조건 밖에서 실행해 첫 실패에서 멈추고 상태를 받는다.
  set +e
  (
    set -e
    stop_native_units_and_require_inactive
    if [[ -n "$old_target" && -d "$old_target" ]]; then
      rollback_contract_dir="$old_target/rollback-contract"
      sudo -n install -m 0640 -o root -g root "$rollback_contract_dir/youtube-collector-host.env" "$host_env"
      sudo -n install -m 0644 -o root -g root "$rollback_contract_dir/hololive-youtube-collector@.service" "$unit_file"
      sudo -n ln -sfn "$old_target" "$current_link"
      native_previous_link_restore "$releases_root" "$previous_link" "$rollback_contract_dir/previous-before-cutover"
      po_restore_previous "$old_target"
      sudo -n systemctl daemon-reload
      sudo -n systemctl enable --now "$unit"
    else
      po_restore_previous ""
      sudo -n rm -f "$current_link" "$host_env" "$unit_file"
      sudo -n systemctl daemon-reload
    fi
  )
  restore_status="$?"
  set -e
  if [[ "$restore_status" -ne 0 ]]; then
    echo "host-native collector cutover failed and the recorded runtime could not be restored" >&2
  fi
  exit "$status"
}
arm_native_cutover_restore() {
  cutover_restore_owner_pid="$BASHPID"
  trap restore_native_after_failed_cutover ERR
}
arm_native_cutover_restore
stop_native_units_and_require_inactive

sudo -n install -m 0640 -o root -g root "$payload/youtube-collector-host.env" "$host_env"
sudo -n install -m 0644 -o root -g root "$payload/hololive-youtube-collector@.service" "$unit_file"
sudo -n ln -sfn "$release_dir" "$current_link"

po_install_release "$release_dir" "$EXPECTED_REVISION"
sudo -n systemd-analyze verify "$unit_file"
sudo -n systemctl daemon-reload
sudo -n systemctl enable "$unit"
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
