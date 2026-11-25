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
slot_root="${slot_root:?collector slot preamble required}" slot_host_env="${slot_host_env:?collector slot preamble required}" slot_collector_unit="${slot_collector_unit:?collector slot preamble required}" slot_service="${slot_service:?collector slot preamble required}"
payload="$HOME/$payload_name"
release_path_lib="$payload/bin/ap-host-native-release-path.sh"
releases_root="$slot_root/releases"
current_link="$slot_root/current"
previous_link="$slot_root/previous"
host_env="$slot_host_env"
unit_file="/etc/systemd/system/${slot_collector_unit%%@*}@.service"
unit="$slot_collector_unit"
po_apply_lib="$payload/bin/ap-host-native-po.sh"
worker_profile="/etc/stack-secrets/hololive-bot/worker-profiles/${service}.json"
window_action="${9:?native command required}"
window_old="" window_digest="" window_nonce=""
[[ "$(hostname -s)" == osaka1 && "$service" == "$slot_service" ]]
native_release_id_validate "$release_id"
release_dir="$releases_root/$release_id" window="$releases_root/$release_id/native-observation"
window_units=("$po_socket" "$po_service" "$unit")
collector_readiness_fetch() {
  systemctl is-active --quiet "$unit" || return 1
  sudo -n -u hololive env \
    HEALTHCHECK_CA_CERT_FILE=/etc/stack-secrets/hololive-bot/certs/hololive-h3.crt \
    HEALTHCHECK_SERVER_NAME=127.0.0.1 \
    "$current_link/bin/healthcheck" --body "https://127.0.0.1:${port}/ready"
}
native_deployment_inputs() {
  sudo -n test -r /etc/stack-secrets/hololive-bot/collector.env
  sudo -n test -r "$worker_profile"
  sudo -n test -r /etc/stack-secrets/hololive-bot/certs/postgres-ca.pem
  sudo -n test -r /etc/stack-secrets/hololive-bot/certs/hololive-h3.crt
  sudo -n test -r /etc/stack-secrets/hololive-bot/certs/hololive-h3.key
  require_node_version /usr/bin/node
}

native_same_window_baseline() {
  local old_target="$releases_root/$window_old" n file paths drop directory persistent
  local -a drops
  [[ "$#" == 4 ]]
  for n in "$@"; do [[ "$n" =~ ^[0-9a-f]{64}$ ]]; done
  native_release_dir_resolve "$releases_root" "$release_id" "$current_link" >/dev/null
  native_window_previous >/dev/null
  native_existing_host_check "$required_udp_buffer" "$swapfile_size_mib"
  native_deployment_inputs
  sudo -n cmp -s "$old_target/worker-profile.json" "$worker_profile"
  native_release_pin check profile "$host_env" "$old_target" "$release_dir" "$1"
  native_release_pin check issuer "$po_unit_file" "$old_target" "$release_dir" "$3"
  [[ "$(native_file_digest "$unit_file")" == "$2" && "$(native_file_digest "$po_socket_file")" == "$4" ]]
  original_port=5433 native_window_verify_original
  [[ "$(realpath -e "${unit_file%/*}")" == "${unit_file%/*}" ]]
  for n in "${window_units[@]}"; do
    native_window_live_generation "$n" >/dev/null
    [[ "$(native_window_prop "$n" ControlPID)" == 0 && "$(native_window_prop "$n" NeedDaemonReload)" == no ]]
    persistent="$(native_window_prop "$n" UnitFileState)"
    [[ "$persistent" == enabled || ( "$n" == "$po_service" && "$persistent" == disabled ) ]]
    directory="${unit_file%/*}/$n.d"
    sudo -n test ! -L "$directory"
    if sudo -n test -e "$directory"; then
      sudo -n test -d "$directory"
      [[ "$(sudo -n stat -c '%u:%g:%a' "$directory")" == 0:0:755 ]]
    fi
    paths="$(native_window_prop "$n" DropInPaths)"
    read -r -a drops <<< "$paths"
    for drop in "${drops[@]}"; do
      native_window_barrier_body "$n" permanent | sudo -n cmp -s - "$drop"
    done
  done
  sudo -n test ! -L "$old_target/rollback-contract"
  if sudo -n test -e "$old_target/rollback-contract"; then
    sudo -n test -d "$old_target/rollback-contract"
    for file in "$host_env" "$unit_file" "$po_unit_file" "$po_socket_file"; do
      sudo -n cmp -s "$file" "$old_target/rollback-contract/${file##*/}"
    done
    [[ "$(native_window_previous)" == "$(sudo -n cat "$old_target/rollback-contract/previous-before-cutover")" ]]
    sudo -n sh -ec 'cd "$1"; sha256sum --check --strict --status rollback-contract/SHA256SUMS' sh "$old_target"
  fi
}

case "$window_action" in
  apply-origin)
    [[ $# == 13 && ! -e "$payload" && ! -L "$payload" ]]
    apply_origin_pin="$(native_apply_preflight)"
    window_old="${apply_origin_pin%% *}"
    native_same_window_baseline "${@:10}" >&2
    printf '%s\n' "$apply_origin_pin"
    exit ;;
  same-window)
    [[ $# == 11 && "${11}" =~ ^[0-9a-f]{64}$ ]]; apply_origin_pin="${10}" window_digest="${11}"
    window_old="${apply_origin_pin%% *}"
    native_release_id_validate "$window_old"
    [[ "$(native_apply_preflight "$window_old")" == "$apply_origin_pin" ]]
    exec 3>&1 >&2
    [[ "$(native_payload_digest "$payload")" == "$window_digest" ]]
    profile_digest="$(native_file_digest "$payload/${slot_host_env##*/}")"
    issuer_digest="$(native_file_digest "$payload/$po_service")"
    native_same_window_baseline "$profile_digest" "$(native_file_digest "$payload/${unit_file##*/}")" "$issuer_digest" "$(native_file_digest "$payload/$po_socket")" >&2
    ;;
  apply-accepted)
    [[ $# == 10 ]]
    # shellcheck disable=SC2034
    window_nonce="${10}"
    native_apply_close_observation
    exit
    ;;
  *) exit 2 ;;
esac

normalize_runtime_payload_permissions() {
  local root="$1"
  sudo -n test -d "$root/youtubejs" || return "$?"
  sudo -n test ! -L "$root/youtubejs" || return "$?"

  sudo -n find -P "$root/youtubejs" \
    \( -type d ! -perm -0555 -o -type f \( ! -perm -0444 -o -perm /0111 ! -perm -0111 \) \) -exec chmod a+rX -- {} +
}

test -r "$release_path_lib"
test -r "$po_apply_lib"
native_cutover_claim "$releases_root/$release_id"
old_target=""
if [[ -L "$current_link" ]]; then
  old_target="$(readlink -f "$current_link" || true)"
fi
[[ "$old_target" == "$releases_root/$window_old" && -d "$old_target" &&
   "$(native_window_identity apply "$window_old" '')" == "$apply_origin_pin" ]]
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
release_dir="$(native_release_dir_resolve "$releases_root" "$release_id" "$current_link")"

sudo -n mkdir "$release_dir"
sudo -n rsync -a --delete "$payload/" "$release_dir/"
sudo -n chown -R -P root:root "$release_dir"
for reserved in accepted-window native-window native-observation; do
  sudo -n test ! -e "$release_dir/$reserved" && sudo -n test ! -L "$release_dir/$reserved" || exit 1
done
copied_digest="$(sudo -n bash -c "$(declare -f native_payload_digest); native_payload_digest \"\$1\"" _ "$release_dir")"
[[ "$copied_digest" == "$window_digest" ]]
normalize_runtime_payload_permissions "$release_dir"
[[ "$(cat "$release_dir/po-sandbox/revision")" == "${EXPECTED_REVISION:?expected native source SHA missing}" ]]
po_install_root="$release_dir/po-sandbox/rootfs"
sudo -n mkdir -p "$po_install_root"
(cd "$release_dir/po-sandbox" && sudo -n sha256sum --check --strict rootfs.tar.sha256)
sudo -n tar -xf "$release_dir/po-sandbox/rootfs.tar" -C "$po_install_root" --no-same-owner --same-permissions
sudo -n chown -R -P root:root "$po_install_root"
po_validate_release "$release_dir"
sudo -n find -P "$release_dir" "$release_dir/bin" "$release_dir/bin/hololive-collector" "$release_dir/bin/healthcheck" \
  "$release_dir/bin/hololive-collector-wrapper" -maxdepth 0 ! -perm 0755 -exec chmod 0755 -- {} +
sudo -n install -m 0444 -o root -g root "$worker_profile" "$release_dir/worker-profile.json"
sudo -n systemd-run --quiet --wait --pipe --collect \
  --property=User=hololive --property=Group=opc --property=PrivateNetwork=yes \
  --property=NoNewPrivileges=yes --property=RuntimeMaxSec=30s \
  --property="WorkingDirectory=$release_dir" --property=ProtectHome=yes --property=ProtectSystem=strict \
  --property=PrivateTmp=yes \
  --property=EnvironmentFile=/etc/stack-secrets/hololive-bot/collector.env \
  --property="EnvironmentFile=$release_dir/${slot_host_env##*/}" \
  /usr/bin/env "STACK_WORKER_PROFILE_FILE=$release_dir/worker-profile.json" \
  "$release_dir/bin/hololive-collector-wrapper" "$release_dir/bin/hololive-collector" --check-worker-profile
sudo -n sh -c 'cd "$1" && sha256sum bin/hololive-collector worker-profile.json > SHA256SUMS && chmod 0444 SHA256SUMS' sh "$release_dir"

if sudo -n test -d "$old_target/rollback-contract"; then
  native_rollback_validate "$old_target"
else
  native_snapshot_before_cutover
fi

sudo -n cmp -s "$worker_profile" "$release_dir/worker-profile.json" || {
  echo 'candidate source changed during profile validation; writer unchanged' >&2
  exit 1
}
candidate_endpoint="$(native_root_endpoint "$release_dir/${slot_host_env##*/}")"
if [[ "$candidate_endpoint" != '100.100.1.8|5433' ]]; then
  echo 'candidate must use the central PostgreSQL direct TLS endpoint; writer unchanged' >&2
  exit 1
fi

native_apply_before_stop
native_existing_host_check "$required_udp_buffer" "$swapfile_size_mib"
sudo -n cmp -s "$old_target/worker-profile.json" "$release_dir/worker-profile.json"
native_release_pin check profile "$host_env" "$old_target" "$release_dir" "$profile_digest"
native_release_pin check issuer "$po_unit_file" "$old_target" "$release_dir" "$issuer_digest"
sudo -n mkdir -m 0700 "$window"
native_window_boot | sudo -n tee "$window/boot" >/dev/null
printf '%s\n' "$apply_origin_pin" "$release_dir" "$window_digest" "${native_claim_nonce:?}" | sudo -n tee "$window/pins" >/dev/null
sudo -n sha256sum "$old_target/rollback-contract/SHA256SUMS" | sudo -n tee "$window/inputs.sha256" >/dev/null

native_same_window_origin() {
  local link="$current_link" current prior
  [[ "$old_target" == "$releases_root/$window_old" && -d "$old_target" ]] || return 1
  current="$(realpath -e "$current_link")" || return 1
  prior=absent
  if [[ -L "$previous_link" ]]; then
    prior="$(realpath -e "$previous_link")" || return 1
  else
    [[ ! -e "$previous_link" ]] || return 1
  fi
  if [[ "$current" == "$release_dir" && "$prior" == "$old_target" ]]; then
    link="$previous_link"
  else
    [[ "$current" == "$old_target" && ( "$prior" == "$old_target" || "$prior" == "${old_previous_target:-absent}" ) ]] || return 1
  fi
  local current_link="$link"
  [[ "$(native_window_identity apply "$window_old" '')" == "$apply_origin_pin" &&
     "$(sudo -n cat "${native_recovery_dir:?}/claim")" == "$native_claim_nonce" &&
     "$(sudo -n cat "$native_recovery_dir/owner-pid")" == "$cutover_restore_owner_pid" &&
     "$(sudo -n cat "$native_recovery_dir/release")" == "$release_dir" ]] || return 1
  sudo -n test ! -e "$native_recovery_dir/outcome" || return 1
  native_window_boot | sudo -n cmp -s - "$window/boot" || return 1
  printf '%s\n' "$apply_origin_pin" "$release_dir" "$window_digest" "$native_claim_nonce" | sudo -n cmp -s - "$window/pins"
}

native_same_window_terminal() {
  local n
  for n in "${window_units[@]}"; do
    native_window_terminal "$n" || return 1
    [[ "$(native_window_prop "$n" LoadState)" == loaded && "$(native_window_prop "$n" UnitFileState)" == disabled ]] || return 1
  done
}

native_same_window_stop() {
  local n id persistent since
  local -A original=()
  native_same_window_origin
  for n in "${window_units[@]}"; do
    original[$n]="$(native_window_live_generation "$n")"
    persistent="$(native_window_prop "$n" UnitFileState)"
    [[ "$persistent" == enabled || ( "$n" == "$po_service" && "$persistent" == disabled ) ]]
    [[ "$(native_window_prop "$n" ControlPID)" == 0 ]]
    printf 'InvocationID=%s\nUnitFileState=%s\n' "${original[$n]}" "$persistent" | sudo -n tee "$window/$n.before" >/dev/null
  done
  original_port=5433 native_window_verify_original
  for n in "${window_units[@]}"; do [[ "$(native_window_live_generation "$n")" == "${original[$n]}" ]]; done
  since="@$EPOCHREALTIME"
  sudo -n systemctl --job-mode=fail stop "$po_socket" "$po_service"
  for n in "${window_units[@]}"; do
    id="${original[$n]}"
    if [[ "$n" == "$unit" ]]; then
      [[ "$(native_window_live_generation "$n")" == "$id" ]]
      since="@$EPOCHREALTIME"
      sudo -n systemctl --job-mode=fail stop "$n"
      sudo -n journalctl --boot --quiet --no-pager --grep='\bcollector_stopped\b' -o json \
        --output-fields=_SYSTEMD_INVOCATION_ID --since="$since" "_SYSTEMD_UNIT=$n" "_SYSTEMD_INVOCATION_ID=$id" |
        python3 -I -B -c 'import json,sys; assert json.load(sys.stdin)["_SYSTEMD_INVOCATION_ID"] == sys.argv[1]' "$id"
    fi
    native_window_stop_receipt "$n" "$id" "$since" | sudo -n tee "$window/$n.stopped" >/dev/null
    native_window_terminal "$n"
  done
  for n in "${window_units[@]}"; do
    if sudo -n grep -Fxq UnitFileState=enabled "$window/$n.before"; then sudo -n systemctl disable "$n"; fi
  done
  native_same_window_terminal
  native_window_original_seal | sudo -n tee "$window/original-stops" >/dev/null
  sudo -n sync -f "$window"
}

native_same_window_stop_phase() {
  native_cutover_run stop native_same_window_stop
  [[ "${native_completion_verified:?}" == true && "${native_worker_status?}" == 0 &&
     "$(sudo -n cat "$native_recovery_dir/step")" == stop && "$(sudo -n cat "$native_recovery_dir/terminal")" == 0 ]]
  sudo -n ln -T -- "$window/original-stops" "$window/confirmed-stops"
}

native_same_window_recoverable() {
  local n
  [[ "${native_pool_check_pending:-false}" == false ]] || return 1
  native_same_window_origin || return 1
  sudo -n test ! -e "$window/blocked" || return 1
  for n in "${window_units[@]}"; do sudo -n test ! -e "$window/$n.requesting" || return 1; done
  native_window_original_seal | sudo -n cmp -s - "$window/confirmed-stops" || return 1
  sudo -n sha256sum --strict --check --status "$window/inputs.sha256" || return 1
  native_same_window_terminal || return 1
  for n in "$unit_file" "$po_socket_file"; do sudo -n cmp -s "$n" "$old_target/${n##*/}" || return 1; done
  native_release_pin recoverable profile "$host_env" "$old_target" "$release_dir" "$profile_digest" || return 1
  native_release_pin recoverable issuer "$po_unit_file" "$old_target" "$release_dir" "$issuer_digest"
}

native_same_window_restore_state() {
  local target record live boot
  boot="$(native_window_boot)" || return 1
  printf '%s\n' "$boot" | sudo -n cmp -s - "$window/boot" || return 1
  for target in "${window_units[@]}"; do
    if sudo -n test -f "$window/$target.restored"; then
      record="$(sudo -n cat "$window/$target.restored")" || return 1
      [[ "$record" =~ ^[0-9a-f]{32}\ [1-9][0-9]*\ [0-9a-f]{32}$ && "${record%% *}" == "$boot" ]] || return 1
      live="$(native_window_live_generation "$target")" || return 1
      [[ "$live" == "${record##* }" && "$(native_window_prop "$target" ControlPID)" == 0 ]] || return 1
    else
      native_window_terminal "$target" || return 1
    fi
  done
}

native_same_window_restore() {
  local n since receipt
  native_same_window_recoverable
  native_rollback_validate "$old_target"
  native_rollback_endpoint_validate "$old_target" "$host_env"
  native_rollback_endpoint_validate "$old_target" "$release_dir/${slot_host_env##*/}"
  native_same_window_recoverable
  if sudo -n test -d "$window/held"; then native_apply_unarm_issuer; fi
  native_release_pin restore profile "$host_env" "$old_target" "$release_dir" "$profile_digest"
  native_release_pin restore issuer "$po_unit_file" "$old_target" "$release_dir" "$issuer_digest"
  if [[ "$(realpath -e "$current_link")" != "$old_target" ]]; then sudo -n ln -sfn "$old_target" "$current_link"; fi
  if [[ -L "$previous_link" && "$(realpath -e "$previous_link")" == "$old_target" ]]; then
    native_previous_link_restore "$releases_root" "$previous_link" "$old_target/rollback-contract/previous-before-cutover"
  fi
  native_same_window_origin
  for n in "$host_env" "$unit_file" "$po_socket_file" "$po_unit_file"; do sudo -n cmp -s "$n" "$old_target/${n##*/}"; done
  sudo -n systemctl daemon-reload
  native_same_window_terminal
  for n in "${window_units[@]}"; do
    native_same_window_restore_state
    if sudo -n grep -Fxq UnitFileState=enabled "$window/$n.before"; then sudo -n systemctl enable "$n"; fi
    native_window_terminal "$n"
    [[ "$(native_window_prop "$n" NeedDaemonReload)" == no ]]
    [[ "$(native_window_prop "$n" UnitFileState)" == "$(sudo -n sed -n 's/^UnitFileState=//p' "$window/$n.before")" ]]
    since="@$EPOCHREALTIME"
    sudo -n systemctl --job-mode=fail start "$n"
    receipt="$(native_window_start_receipt "$n" "$(native_window_boot)" "$since")"
    [[ "$(native_window_live_generation "$n")" == "${receipt##* }" ]]
    printf '%s\n' "$receipt" | sudo -n tee "$window/$n.restored" >/dev/null
    if [[ "$n" == "$po_service" ]]; then po_wait_ready "$old_target"; fi
  done
  native_same_window_restore_state
  collector_readiness_poll 90 2 collector_readiness_fetch >/dev/null
  original_port=5433 native_window_verify_original
  native_same_window_restore_state
}

native_same_window_issuer() {
  native_same_window_origin
  native_same_window_terminal
  native_release_pin apply issuer "$po_unit_file" "$old_target" "$release_dir" "$issuer_digest"
  sudo -n cmp -s "$po_socket_file" "$release_dir/$po_socket"
  po_verify_units "$release_dir" "$po_unit_file" "$po_socket_file"
  sudo -n systemctl daemon-reload
  native_same_window_terminal
  native_apply_start_issuer
  po_wait_ready "$release_dir"
  native_window_candidates "$po_socket" "$po_service"
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
  if [[ "${native_outcome_unknown:-false}" == true ]] || ! native_same_window_recoverable; then
    sudo -n touch "$window/blocked"
    native_cutover_unknown
    exit "$status"
  fi
  set +e
  native_signal_status=0
  native_cutover_run restore native_same_window_restore
  restore_status="$?"
  set -e
  if (( ${native_signal_status:-0} != 0 )); then
    status="$native_signal_status"
  fi
  if [[ "${native_outcome_unknown:-false}" == true || "${native_worker_status:-$restore_status}" -ne 0 || "${native_completion_verified:-false}" != true ]]; then
    echo "host-native collector cutover failed and the recorded runtime could not be restored" >&2
    sudo -n touch "$window/blocked"
    native_cutover_unknown
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
native_same_window_stop_phase
native_cutover_run previous-link sudo -n ln -sfn "$old_target" "$previous_link"
native_cutover_run host-env native_release_pin apply profile "$host_env" "$old_target" "$release_dir" "$profile_digest"
native_cutover_run current-link sudo -n ln -sfn "$release_dir" "$current_link"

# shellcheck disable=SC2034
apply_start_since="@$EPOCHREALTIME"
native_pool_check_pending=true
native_cutover_run database-endpoint sudo -n systemd-run --quiet --wait --pipe --collect \
  --property=User=hololive --property=Group=opc --property=NoNewPrivileges=yes --property=RuntimeMaxSec=40s \
  --property="WorkingDirectory=$release_dir" --property=ProtectHome=yes --property=ProtectSystem=strict \
  --property=PrivateTmp=yes --property=RestrictAddressFamilies=AF_INET \
  --property=IPAddressDeny=any --property=IPAddressAllow=100.100.1.8/32 \
  --property=TemporaryFileSystem=/etc/stack-secrets:ro \
  --property=BindReadOnlyPaths=/etc/stack-secrets/hololive-bot/certs/postgres-ca.pem \
  --property=EnvironmentFile=/etc/stack-secrets/hololive-bot/collector.env \
  --property="EnvironmentFile=$host_env" \
  /usr/bin/env "STACK_WORKER_PROFILE_FILE=$release_dir/worker-profile.json" \
  "$release_dir/bin/hololive-collector-wrapper" "$release_dir/bin/hololive-collector" --check-database-endpoint
native_pool_check_pending=false

native_cutover_run issuer native_same_window_issuer
sudo -n systemd-analyze verify "$unit_file"
native_cutover_run daemon-reload sudo -n systemctl daemon-reload
native_cutover_run collector-enable sudo -n systemctl enable "$unit"
native_window_terminal "$unit"
native_cutover_run collector-restart sudo -n systemctl --job-mode=fail start "$unit"

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
native_apply_await_observation >&3
trap - ERR HUP INT TERM
