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
window_action="${9:?native command required}"
window_name="${10:-}" window_old="${11:-}" window_digest="${12:-}" window_nonce="${13:-}"
same_window=false
if [[ "$window_action" == same-window || "$window_action" == apply-accepted ]]; then
  [[ "$(hostname -s)" == osaka1 && "$service" == youtube-collector-a ]]
  native_release_id_validate "$release_id"
  release_dir="$releases_root/$release_id" window="$releases_root/$release_id/native-observation"
  window_units=("$po_socket" "$po_service" "$unit")
fi
collector_readiness_fetch() {
  systemctl is-active --quiet "$unit" || return 1
  sudo -n -u hololive env \
    HEALTHCHECK_CA_CERT_FILE=/etc/stack-secrets/hololive-bot/certs/hololive-h3.crt \
    HEALTHCHECK_SERVER_NAME=127.0.0.1 \
    "$current_link/bin/healthcheck" --body "https://127.0.0.1:${port}/ready"
}
case "$window_action" in
  quiesce) [[ $# == 13 && -z "$window_nonce" ]]; native_window_quiesce; exit ;;
  resume) [[ $# == 13 && -n "$window_nonce" ]]; native_window_resume_claim ;;
  accepted|observation-failed) [[ $# == 13 && -n "$window_nonce" ]]; native_window_close_observation; exit ;;
  apply-origin) [[ $# == 9 ]]; native_apply_preflight; exit ;;
  same-window)
    [[ $# == 11 && "${11}" =~ ^[0-9a-f]{64}$ ]]; apply_origin_pin="${10}" window_digest="${11}"
    window_old="${apply_origin_pin%% *}"
    native_release_id_validate "$window_old"
    [[ "$(native_apply_preflight "$window_old")" == "$apply_origin_pin" ]]
    exec 3>&1 >&2
    window_action=deploy same_window=true ;;
  apply-accepted) [[ $# == 10 ]]; window_nonce="${10}"; native_apply_close_observation; exit ;;
  deploy)
    remote_host="$(hostname -s)"
    [[ $# == 9 && -n "$remote_host" && "$remote_host" != osaka1 ]]; native_recovery_require_clear ;;
  *) exit 2 ;;
esac

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

if [[ "$window_action" == deploy ]]; then native_cutover_claim "$releases_root/$release_id"; fi
# 준비 중 실패도 guard와 산출물을 남깁니다. 다음 apply 전에 소유 worker와 runtime을 확인합니다.
old_target=""
if [[ -L "$current_link" ]]; then
  old_target="$(readlink -f "$current_link" || true)"
fi
if [[ "$same_window" == true ]]; then [[ "$(native_window_identity apply "$window_old" '')" == "$apply_origin_pin" ]]; fi
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
if [[ "$window_action" == resume || "$same_window" == true ]]; then
  for reserved in accepted-window native-window native-observation; do
    sudo -n test ! -e "$release_dir/$reserved" && sudo -n test ! -L "$release_dir/$reserved" || exit 1
  done
  copied_digest="$(sudo -n bash -c "$(declare -f native_payload_digest); native_payload_digest \"\$1\"" _ "$release_dir")"
  [[ "$copied_digest" == "$window_digest" ]]
fi
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

if [[ "$window_action" == resume ]]; then
  native_window_pins
  native_window_inputs
else
  if [[ -n "$old_target" && -d "$old_target" ]]; then native_snapshot_before_cutover; fi
  native_capture_collector_state "$unit" "$old_target" || {
    echo 'collector state or persistent enablement is unconfirmed; writer unchanged' >&2
    exit 1
  }
fi

sudo -n cmp -s "$worker_profile" "$release_dir/worker-profile.json" || {
  echo 'candidate source changed during profile validation; writer unchanged' >&2
  exit 1
}
candidate_endpoint="$(native_root_endpoint "$release_dir/youtube-collector-host.env")"
if [[ "$candidate_endpoint" != '100.100.1.8|6433' ]]; then
  echo 'candidate must use the native collector pooler endpoint; writer unchanged' >&2
  exit 1
fi

if [[ "$same_window" == true ]]; then
  native_apply_before_stop
  sudo -n mkdir -m 0700 "$window"
  native_window_boot | sudo -n tee "$window/boot" >/dev/null
  old_issuer="$(native_window_live_generation "$po_service")"
  printf 'InvocationID=%s\n' "$old_issuer" | sudo -n tee "$window/$po_service.before" >/dev/null
fi
# PO issuer를 collector보다 먼저 멈춘다. collector는 종료하면서 소유 generation을 DELETE로 반납하는데, broker가 살아
# 있으면 retire 뒤 exit 0하고 Restart=always(RestartSec=1s)로 다시 떠서 곧바로 이 스크립트에 다시 멈춰진다.
# broker가 먼저 멈추면 그 DELETE는 broker_unavailable로 끝나고 collector는 재전송 없이 무시한다(proof-controller.mjs retireOwned).
# socket을 먼저 멈춰 service가 Requires=로 같은 transaction에서 멈추게 하고, 그 사이 socket activation이 service를 다시 띄우지 못하게 한다.
stop_native_units_and_require_inactive() {
  local target load active persistent status failed=0
  local -a stopped=()
  for target in "$po_socket" "$po_service" "$unit"; do
    status=0
    load="$(systemctl show "$target" -p LoadState --value)" || status=$?
    if [[ "$load" == not-found ]] && (( status == 0 || status == 4 )); then
      continue
    fi
    if (( status != 0 )) || [[ "$load" != loaded && "$load" != masked ]]; then
      failed=1
    fi
    if [[ "$load" == masked ]]; then
      sudo -n systemctl stop "$target" || failed=1
    else
      sudo -n systemctl disable --now "$target" >/dev/null || failed=1
    fi
    stopped+=("$target")
  done
  for target in "${stopped[@]}"; do
    active="$(systemctl show "$target" -p ActiveState --value)" || failed=1
    persistent="$(systemctl show "$target" -p UnitFileState --value)" || failed=1
    if [[ "$active" != inactive && "$active" != failed ]] ||
       [[ "$persistent" != disabled && "$persistent" != masked ]]; then
      failed=1
    fi
  done
  if (( failed != 0 )); then
    echo 'could not confirm native collector and issuer persistently stopped; recovery guard retained' >&2
    return 1
  fi
}

native_restore_recorded_runtime() {
  if [[ "${window_action:-deploy}" == resume ]]; then
    native_window_rehold
    return
  fi
  stop_native_units_and_require_inactive
  if [[ "${native_pool_check_pending:-false}" == true ]]; then
    echo 'pooled database proof incomplete; collector and issuer remain disabled; recovery guard retained' >&2
    return 1
  fi
  if [[ -n "$old_target" && -d "$old_target" ]]; then
    rollback_contract_dir="$old_target/rollback-contract"
    native_rollback_validate "$old_target"
    native_rollback_endpoint_validate "$old_target" "$release_dir/youtube-collector-host.env" || {
      echo "automatic rollback blocked; collector and issuer stopped; retain $old_target and $release_dir; recovery guard retained: $native_recovery_dir" >&2
      return 1
    }
    native_rollback_endpoint_validate "$old_target" "$host_env" || return
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
  if [[ "${same_window:-false}" == true ]] && sudo -n test -d "$window/held"; then
    if [[ "${native_outcome_unknown:-false}" == true ]] || ! native_apply_unarm_issuer; then
      sudo -n touch "$window/blocked"
      native_cutover_unknown
      exit "$status"
    fi
  fi
  if [[ "${native_outcome_unknown:-false}" == true ]]; then
    if [[ "${window_action:-deploy}" == resume ]]; then sudo -n touch "$window/blocked"; fi
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
    if [[ "${window_action:-deploy}" == resume ]]; then
      sudo -n touch "$window/blocked"
      native_cutover_unknown
    elif [[ "${native_outcome_unknown:-false}" != true && "${native_completion_verified:-false}" == true ]]; then
      # 복원이 issuer를 열고 나서 실패했을 수 있다. 미확정 worker와는 경쟁하지 않는다.
      stop_native_units_and_require_inactive || echo 'native failure brake not verified' >&2
    fi
  elif [[ "${window_action:-deploy}" == resume ]]; then
    echo "후보 정지 후에도 hold와 복구 입력을 유지합니다: $window" >&2
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
if [[ "$window_action" == deploy ]]; then native_cutover_run stop stop_native_units_and_require_inactive; fi

native_cutover_run host-env sudo -n install -m 0640 -o root -g root "$release_dir/youtube-collector-host.env" "$host_env"
native_cutover_run collector-unit sudo -n install -m 0644 -o root -g root "$release_dir/hololive-youtube-collector@.service" "$unit_file"
native_cutover_run current-link sudo -n ln -sfn "$release_dir" "$current_link"

if [[ "$same_window" == true ]]; then apply_start_since="@$EPOCHREALTIME"; fi
native_pool_check_pending=true
native_cutover_run pooled-database sudo -n systemd-run --quiet --wait --pipe --collect \
  --property=User=hololive --property=Group=opc --property=NoNewPrivileges=yes --property=RuntimeMaxSec=40s \
  --property="WorkingDirectory=$release_dir" --property=ProtectHome=yes --property=ProtectSystem=strict \
  --property=PrivateTmp=yes --property=RestrictAddressFamilies=AF_INET \
  --property=IPAddressDeny=any --property=IPAddressAllow=100.100.1.8/32 \
  --property=TemporaryFileSystem=/etc/stack-secrets:ro \
  --property=BindReadOnlyPaths=/etc/stack-secrets/hololive-bot/certs/postgres-ca.pem \
  --property=EnvironmentFile=/etc/stack-secrets/hololive-bot/youtube-collector.env \
  --property="EnvironmentFile=$host_env" \
  /usr/bin/env "STACK_WORKER_PROFILE_FILE=$release_dir/worker-profile.json" \
  "$release_dir/bin/youtube-collector-wrapper" "$release_dir/bin/youtube-collector" --check-pooled-database
native_pool_check_pending=false

native_cutover_run issuer po_install_release "$release_dir" "$EXPECTED_REVISION"
sudo -n systemd-analyze verify "$unit_file"
native_cutover_run daemon-reload sudo -n systemctl daemon-reload
native_cutover_run collector-enable sudo -n systemctl enable "$unit"
if [[ "$window_action" == resume ]]; then
  native_window_candidates "$po_socket" "$po_service"
  native_cutover_run collector-start native_window_start "$unit"
  native_window_candidates "${window_units[@]}"
else
  native_cutover_run collector-restart sudo -n systemctl restart "$unit"
fi

since_epoch="$(date -u -d "$change_started_at" +%s)"
for _ in $(seq 1 30); do
  active_state="$(systemctl show "$unit" -p ActiveState --value)"
  [[ "$active_state" == active ]] && break
  sleep 2
done
[[ "$active_state" == active ]]
active_enter="$(systemctl show "$unit" -p ActiveEnterTimestamp --value)"
active_epoch="$(date -u -d "$active_enter" +%s)"
[[ "$active_epoch" -ge "$since_epoch" ]]

systemctl show "$unit" -p ActiveState -p SubState -p ExecMainPID -p MemoryCurrent -p NRestarts -p ActiveEnterTimestamp
printf 'net.core.rmem_max=%s\n' "$(sysctl -n net.core.rmem_max)"
printf 'net.core.wmem_max=%s\n' "$(sysctl -n net.core.wmem_max)"

ready="$(collector_readiness_poll 90 2 collector_readiness_fetch)"
printf '%s\n' "$ready"
collector_readiness_validate "$ready"
if [[ "$window_action" == resume ]]; then native_window_candidates "${window_units[@]}"; fi
sudo -n -u hololive env \
  HEALTHCHECK_CA_CERT_FILE=/etc/stack-secrets/hololive-bot/certs/hololive-h3.crt \
  HEALTHCHECK_SERVER_NAME=127.0.0.1 \
  "$release_dir/bin/healthcheck" "https://127.0.0.1:${port}/health"
[[ "$(readlink -f "$current_link")" == "$release_dir" ]]
sudo -n python3 "$release_dir/bin/worker-profile-pair.py" verify-native "$service" "$release_dir" "$unit"
[[ "$(systemctl show "$unit" -p NRestarts --value)" == 0 ]]
[[ "$(systemctl show "$unit" -p SubState --value)" == running ]]
po_since="${change_started_at/T/ }"
po_require_running_without_failure "${po_since%Z} UTC"

native_post_cutover_log_check() {
local journal_since journal_output grep_status=0
journal_since="${change_started_at/T/ }"
journal_since="${journal_since%Z} UTC"
if ! journal_output="$(journalctl -u "$unit" --since "$journal_since" --no-pager)"; then
  echo "failed to read post-cutover logs for $unit" >&2
  false
fi
grep -E 'PostgreSQL|Valkey|active_active|ERR|panic|permission denied|x509|no such file' <<<"$journal_output" >/dev/null || grep_status=$?
(( grep_status <= 1 )) || return "$grep_status"
grep_status=0
if grep -E 'ERR|panic|permission denied|x509|no such file' <<<"$journal_output" >/dev/null; then
  echo 'collector journal contains a startup error' >&2
  return 1
else
  grep_status=$?
fi
(( grep_status == 1 )) || return "$grep_status"
}
native_post_cutover_log_check
if [[ "${window_action:-deploy}" == resume ]]; then
  native_window_await_observation
elif [[ "${same_window:-false}" == true ]]; then
  trap - ERR HUP INT TERM
  native_apply_await_observation >&3
else
  trap - ERR HUP INT TERM
  native_cutover_release_guard
fi
