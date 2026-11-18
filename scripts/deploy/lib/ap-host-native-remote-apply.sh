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
native_recovery_require_clear

normalize_runtime_payload_permissions() {
  local root="$1"
  sudo -n test -d "$root/youtubejs" || return
  sudo -n test ! -L "$root/youtubejs" || return

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
sudo -n install -d -m 0755 -o root -g root /var/lib/hololive-bot
sudo -n install -d -m 0755 -o root -g root /etc/sysctl.d
sudo -n tee /etc/logrotate.d/hololive-bot >/dev/null <<'LOGROTATE'
/var/log/hololive-bot/*.log {
    size 10M
    rotate 14
    missingok
    notifempty
    olddir /var/log/hololive-bot/archive
    compress
    delaycompress
    copytruncate
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

native_cutover_claim "$releases_root/$release_id"
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

sudo -n mkdir "$release_dir"
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
sudo -n install -m 0444 -o root -g root "$worker_profile" "$release_dir/worker-profile.json"
sudo -n systemd-run --quiet --wait --pipe --collect \
  --property=User=hololive --property=Group=opc --property=PrivateNetwork=yes \
  --property=NoNewPrivileges=yes --property=RuntimeMaxSec=30s \
  --property="WorkingDirectory=$release_dir" --property=ProtectHome=yes --property=ProtectSystem=strict \
  --property=PrivateTmp=yes \
  --property=EnvironmentFile=/etc/stack-secrets/hololive-bot/youtube-collector.env \
  --property="EnvironmentFile=$release_dir/youtube-collector-host.env" \
  /usr/bin/env "STACK_WORKER_PROFILE_FILE=$release_dir/worker-profile.json" \
  "$release_dir/bin/youtube-collector-wrapper" "$release_dir/bin/youtube-collector" --check-worker-profile
sudo -n sh -c 'cd "$1" && sha256sum bin/youtube-collector worker-profile.json > SHA256SUMS && chmod 0444 SHA256SUMS' sh "$release_dir"

if [[ -n "$old_target" && -d "$old_target" ]]; then
  native_profile_pair_validate "$old_target" "$host_env"
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
        worker-profile.json \
        SHA256SUMS \
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
fi

collector_prechange_enabled=false
collector_prechange_running=false
collector_prechange_state="$(systemctl show "$unit" -p ActiveState --value 2>/dev/null || true)"
if systemctl is-enabled --quiet "$unit" 2>/dev/null; then
  collector_prechange_enabled=true
fi
case "$collector_prechange_state" in
  active | activating | reloading) collector_prechange_running=true ;;
esac

sudo -n cmp -s "$worker_profile" "$release_dir/worker-profile.json" || {
  echo 'candidate source changed during profile validation; writer unchanged' >&2
  exit 1
}

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

stop_native_units_and_require_inactive() {
  if sudo -n systemctl is-active --quiet "$po_socket"; then
    sudo -n systemctl stop "$po_socket"
  fi
  if sudo -n systemctl is-active --quiet "$po_service"; then
    sudo -n systemctl stop "$po_service"
  fi
  stop_collector_unit_and_require_inactive
}

native_restore_recorded_runtime() {
  stop_native_units_and_require_inactive
  if [[ -n "$old_target" && -d "$old_target" ]]; then
    rollback_contract_dir="$old_target/rollback-contract"
    native_rollback_validate "$old_target"
    sudo -n install -m 0640 -o root -g root "$rollback_contract_dir/youtube-collector-host.env" "$host_env"
    sudo -n install -m 0644 -o root -g root "$rollback_contract_dir/hololive-youtube-collector@.service" "$unit_file"
    sudo -n ln -sfn "$old_target" "$current_link"
    native_previous_link_restore "$releases_root" "$previous_link" "$rollback_contract_dir/previous-before-cutover"
    po_restore_previous "$old_target"
    sudo -n systemctl daemon-reload
    if [[ "${collector_prechange_running:?}" == true ]]; then
      sudo -n systemctl enable --now "$unit"
    else
      if [[ "${collector_prechange_enabled:?}" == true ]]; then
        sudo -n systemctl enable "$unit"
      fi
      echo "collector was not running before the change (ActiveState=${collector_prechange_state:-unknown}); restored $unit without starting it. Start it only after the schema matches the restored release." >&2
    fi
  else
    po_restore_previous ""
    sudo -n rm -f "$current_link" "$host_env" "$unit_file"
    sudo -n systemctl daemon-reload
  fi
}
restore_native_after_failed_cutover() {
  local status="${1:-$?}" restore_status=0
  if [[ "$BASHPID" != "${cutover_restore_owner_pid:?cutover restore owner not armed}" ]]; then
    exit "$status"
  fi
  trap - ERR
  native_restoring=true
  if [[ "${native_phase_active:-false}" == true && "${native_phase_started:-false}" == true ]]; then
    native_cutover_unknown
  fi
  if [[ "${native_outcome_unknown:-false}" == true ]]; then
    exit "$status"
  fi
  set +e
  native_signal_status=0
  native_cutover_run restore native_restore_recorded_runtime
  restore_status="$?"
  set -e
  if (( ${native_signal_status:-0} != 0 )); then
    status="$native_signal_status"
  fi
  if [[ "${native_outcome_unknown:-false}" == true || "${native_worker_status:-$restore_status}" -ne 0 || "${native_completion_verified:-false}" != true ]]; then
    echo "host-native collector cutover failed and the recorded runtime could not be restored" >&2
  else
    native_cutover_release_guard || echo 'recorded runtime restored but native recovery guard could not be released' >&2
  fi
  (( ${native_signal_status:-0} == 0 )) || status="$native_signal_status"
  trap - HUP INT TERM
  exit "$status"
}
native_cutover_failed() {
  local failure_status="$?"
  native_restoring=true
  restore_native_after_failed_cutover "$failure_status"
}
arm_native_cutover_restore() {
  cutover_restore_owner_pid="$BASHPID"
  native_signal_status=0
  native_outcome_unknown=false
  native_phase_active=false
  # shellcheck disable=SC2034 # 연결된 cutover helper의 신호 handler가 재진입을 막습니다.
  native_restoring=false
  trap - EXIT
  trap native_cutover_failed ERR
  trap 'native_cutover_signal 129' HUP
  trap 'native_cutover_signal 130' INT
  trap 'native_cutover_signal 143' TERM
}
arm_native_cutover_restore
if [[ -n "$old_target" && -d "$old_target" ]]; then
  native_cutover_run previous-link sudo -n ln -sfn "$old_target" "$previous_link"
fi
native_cutover_run stop stop_native_units_and_require_inactive

native_cutover_run host-env sudo -n install -m 0640 -o root -g root "$payload/youtube-collector-host.env" "$host_env"
native_cutover_run collector-unit sudo -n install -m 0644 -o root -g root "$payload/hololive-youtube-collector@.service" "$unit_file"
native_cutover_run current-link sudo -n ln -sfn "$release_dir" "$current_link"

native_cutover_run issuer po_install_release "$release_dir" "$EXPECTED_REVISION"
sudo -n systemd-analyze verify "$unit_file"
native_cutover_run daemon-reload sudo -n systemctl daemon-reload
native_cutover_run collector-enable sudo -n systemctl enable "$unit"
native_cutover_run collector-restart sudo -n systemctl restart "$unit"

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
sudo -n python3 "$release_dir/bin/worker-profile-pair.py" verify-native "$service" "$release_dir" "$unit"

native_post_cutover_log_check() {
local journal_since journal_output grep_status=0
journal_since="${change_started_at/T/ }"
journal_since="${journal_since%Z} UTC"
if ! journal_output="$(journalctl -u "$unit" --since "$journal_since" --no-pager)"; then
  echo "failed to read post-cutover logs for $unit" >&2
  false
fi
grep -E 'PostgreSQL|Valkey|active_active|ERR|panic|permission denied|x509|no such file' <<<"$journal_output" || grep_status=$?
(( grep_status <= 1 )) || return "$grep_status"
grep_status=0
if grep -E 'ERR|panic|permission denied|x509|no such file' <<<"$journal_output"; then
  return 1
else
  grep_status=$?
fi
(( grep_status == 1 )) || return "$grep_status"
}
native_post_cutover_log_check
trap - ERR HUP INT TERM
native_cutover_release_guard
