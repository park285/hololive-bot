# shellcheck shell=bash
native_release_window=W2

native_payload_digest() (
  set -o pipefail
  LC_ALL=C tar --sort=name --format=gnu --hard-dereference --numeric-owner --owner=0 --group=0 --mtime=@0 \
    -cf - -C "$1" . | sha256sum | cut -d' ' -f1
)

native_window_pins() {
  [[ "$window_name" == "$native_release_window" ]] || return 1
  [[ "$(hostname -s)" == osaka1 && "$service" == youtube-collector-a && "$EXPECTED_REVISION" =~ ^[0-9a-f]{40}$ &&
     "$window_digest" =~ ^[0-9a-f]{64}$ && "$window_old" != "$release_id" ]] || return 1
  native_release_id_validate "$window_old" && native_release_id_validate "$release_id" || return 1
  case "$window_name" in W1) original_port=5433 ;; W2) original_port=6433 ;; *) return 1 ;; esac
  old_target="$releases_root/$window_old"
  window="$old_target/native-window"
  [[ -L "$current_link" && "$(realpath -e "$current_link")" == "${1:-$old_target}" ]] || return 1
  window_units=("$po_socket" "$po_service" "$unit")
}

native_window_previous() {
  if [[ -L "$previous_link" ]]; then
    local previous
    previous="$(realpath -e "$previous_link")" || return 1
    [[ "${previous%/*}" == "$releases_root" && "$previous" != "$old_target" && -d "$previous" ]] || return 1
    printf '%s\n' "$previous"
  else
    [[ ! -e "$previous_link" ]] || return 1
    echo absent
  fi
}

native_snapshot_before_cutover() {
  local contract="$old_target/rollback-contract"
  native_profile_pair_validate "$old_target" "$host_env"
  sudo -n install -d -m 0755 -o root -g root "$contract"
  po_snapshot_previous "$old_target"
  printf '%s\n' "${old_previous_target:-absent}" | sudo -n tee "$contract/previous-before-cutover" >/dev/null
  sudo -n chmod 0644 "$contract/previous-before-cutover"
  sudo -n install -m 0640 -o root -g root "$host_env" "$contract/youtube-collector-host.env"
  sudo -n install -m 0644 -o root -g root "$unit_file" "$contract/hololive-youtube-collector@.service"
  sudo -n sh -ec '
    cd "$1"
    for f in bin/youtube-collector bin/youtube-collector-wrapper bin/healthcheck; do test -x "$f"; done
    test -f youtubejs/src/server.mjs
    sha256sum bin/youtube-collector bin/youtube-collector-wrapper bin/healthcheck worker-profile.json SHA256SUMS \
      rollback-contract/youtube-collector-host.env rollback-contract/hololive-youtube-collector@.service \
      rollback-contract/po-unit-presence rollback-contract/previous-before-cutover > rollback-contract/SHA256SUMS
    LC_ALL=C find youtubejs/src -type f -exec sha256sum {} + >> rollback-contract/SHA256SUMS
    if test -f rollback-contract/hololive-youtube-po.service; then
      sha256sum rollback-contract/hololive-youtube-po.service rollback-contract/hololive-youtube-po.socket >> rollback-contract/SHA256SUMS
    fi
    chmod 0644 rollback-contract/SHA256SUMS
  ' sh "$old_target"
}

native_window_prop() { systemctl show "$1" --property="$2" --value; }

native_window_terminal() {
  local target="$1" job control state
  job="$(native_window_prop "$target" Job)" && control="$(native_window_prop "$target" ControlPID)" || return 1
  state="$(native_window_prop "$target" ActiveState)" || return 1
  [[ -z "$job" && "$control" == 0 &&
     ( ( "$state" == inactive && "$(native_window_prop "$target" Result)" == success ) ||
       ( "${2:-}" == brake && "$state" == failed ) ) ]] || return 1
  [[ "$target" == "$po_socket" || "$(native_window_prop "$target" MainPID)" == 0 ]]
}

native_window_permit() { printf '/run/hololive-native-%s-%s\n' "$native_claim_nonce" "$1"; }

native_window_boot() {
  local boot
  IFS= read -r boot < /proc/sys/kernel/random/boot_id || return 1
  [[ "$boot" =~ ^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$ ]] && printf '%s\n' "${boot//-/}"
}

native_window_uncertain() {
  sudo -n touch "$window/blocked"
  native_cutover_unknown
  return 1
}

native_window_original_seal() {
  local n
  local -a files=("$window/pins" "$window/inputs.sha256" "$window/boot")
  for n in "${window_units[@]}"; do files+=("$window/$n.before" "$window/$n.stopped"); done
  sudo -n sha256sum -- "${files[@]}"
}

native_window_verify_quiesced() {
  native_window_record | sudo -n cmp -s - "$window/pins" || return 1
  native_window_original_seal | sudo -n cmp -s - "$window/quiesced" || return 1
  native_window_boot | sudo -n cmp -s - "$window/boot"
}

native_window_barrier_body() {
  printf '[Unit]\nConditionPathExists=!%s/native-window\n' "$current_link"
  printf 'ConditionPathExists=!%s/accepting\n' "$native_recovery_dir"
  if [[ "$2" == held ]]; then
    printf 'ConditionPathExists=!%s/blocked\n' "$window"
    printf 'ConditionPathExists=%s\n' "$(native_window_permit "$1")"
    if [[ "$1" != "$po_socket" ]]; then printf '[Service]\nRestart=no\nRestartForceExitStatus=\n'; fi
  fi
}

native_window_barriers() {
  local action="$1" n file directory link object policy force paths drop
  case "$action" in arm|check|finish|permanent|rearm) ;; *) return 2 ;; esac
  local -a drops
  local -a conditions=("$current_link/native-window" "$native_recovery_dir/accepting")
  if [[ "$action" == arm ]]; then
    sudo -n mkdir -- "$window/held" "$window/permanent"
    sudo -n ln -sT held "$window/policy"
  elif [[ "$action" == finish || "$action" == rearm ]]; then
    local next=permanent
    if [[ "$action" == rearm ]]; then next=held; fi
    sudo -n ln -sT "$next" "$window/policy.$next"
    sudo -n mv -T "$window/policy.$next" "$window/policy"
    sudo -n systemctl daemon-reload
    return
  fi
  if [[ "$action" != permanent ]]; then conditions+=("$window/blocked"); fi
  for n in "${window_units[@]}"; do
    directory="${unit_file%/*}/$n.d"
    file="$window/policy/$n.conf"
    link="$directory/90-native-window-$native_claim_nonce.conf"
    if [[ "$action" == arm ]]; then
      sudo -n install -d -m 0755 -o root -g root "$directory"
      native_window_barrier_body "$n" held | sudo -n tee "$window/held/$n.conf" >/dev/null
      native_window_barrier_body "$n" permanent | sudo -n tee "$window/permanent/$n.conf" >/dev/null
      sudo -n ln -sT "$file" "$link"
    elif [[ "$action" == check || "$action" == permanent ]]; then
      policy=held
      if [[ "$action" == permanent ]]; then policy=permanent; fi
      [[ "$(sudo -n readlink "$window/policy")" == "$policy" ]]
      native_window_barrier_body "$n" "$policy" | sudo -n cmp -s - "$file"
      [[ "$(sudo -n readlink "$link")" == "$file" ]]
      paths="$(native_window_prop "$n" DropInPaths)"
      [[ " $paths " == *" $link "* ]]
      read -r -a drops <<< "$paths"
      for drop in "${drops[@]}"; do
        if [[ "$drop" != "$link" ]]; then
          native_window_barrier_body "$n" permanent | sudo -n cmp -s - "$drop"
        fi
      done
      object="$(busctl --system call org.freedesktop.systemd1 /org/freedesktop/systemd1 org.freedesktop.systemd1.Manager GetUnit s "$n")"
      [[ "$object" =~ ^o\ \"(/org/freedesktop/systemd1/unit/[a-zA-Z0-9_]+)\"$ ]]
      local object_path="${BASH_REMATCH[1]}" permit=''
      if [[ "$action" == check ]]; then permit="$(native_window_permit "$n")"; fi
      busctl --system --json=short get-property org.freedesktop.systemd1 "$object_path" org.freedesktop.systemd1.Unit Conditions |
        python3 -I -c 'import json,sys; a=json.load(sys.stdin); rows=[r[:4] for r in a["data"]]; assert a["type"]=="a(sbbsi)" and all(["ConditionPathExists",False,True,p] in rows for p in sys.argv[2:]) and (not sys.argv[1] or ["ConditionPathExists",False,False,sys.argv[1]] in rows)' "$permit" "${conditions[@]}"
      if [[ "$n" != "$po_socket" ]]; then
        policy="$(native_window_prop "$n" Restart)"; force="$(native_window_prop "$n" RestartForceExitStatus)"
        if [[ "$action" == permanent ]]; then [[ "$policy" == always ]]; else [[ "$policy" == no && -z "$force" ]]; fi
      fi
    fi
  done
  if [[ "$action" == arm ]]; then sudo -n systemctl daemon-reload; fi
}

native_window_inputs() {
  local contract="$old_target/rollback-contract" n
  native_rollback_validate "$old_target"
  [[ "$(native_window_previous)" == "$(sudo -n cat "$contract/previous-before-cutover")" ]]
  sudo -n cmp -s "$host_env" "$contract/youtube-collector-host.env"
  [[ "$(native_root_endpoint "$host_env")" == "100.100.1.8|$original_port" ]]
  for n in "$unit_file" "$po_unit_file" "$po_socket_file"; do sudo -n cmp -s "$n" "$contract/${n##*/}"; done
  sudo -n sha256sum --strict --check --status "$window/inputs.sha256"
}

native_window_process_endpoint() {
  sudo -n python3 -I -B - "$1" "$2" <<'PY'
from pathlib import Path
import sys
entries = (Path('/proc') / sys.argv[1] / 'environ').read_bytes().split(b'\0')
for key, expected in [(b'POSTGRES_HOST', b'100.100.1.8'), (b'POSTGRES_PORT', sys.argv[2].encode()),
                      (b'POSTGRES_USER', b'hololive_collector_a'), (b'POSTGRES_SSLMODE', b'verify-full')]:
    if [v.partition(b'=')[2] for v in entries if v.startswith(key+b'=')] != [expected]:
        sys.exit('collector 프로세스의 DB endpoint 불일치')
if any(v.startswith(b'POSTGRES_SOCKET_PATH=') and v != b'POSTGRES_SOCKET_PATH=' for v in entries):
    sys.exit('collector 프로세스가 TCP endpoint를 사용하지 않음')
PY
}

native_window_verify_original() {
  local n pid root execfile
  pid="$(native_window_prop "$unit" MainPID)"; [[ "$pid" =~ ^[1-9][0-9]*$ ]]
  sudo -n test "/proc/$pid/exe" -ef "$old_target/bin/youtube-collector"
  native_window_process_endpoint "$pid" "$original_port"
  sudo -n python3 -I -B "$old_target/bin/worker-profile-pair.py" verify-native "$service" "$old_target" "$unit"
  pid="$(native_window_prop "$po_service" MainPID)"; [[ "$pid" =~ ^[1-9][0-9]*$ ]]
  root="$old_target/po-sandbox/rootfs"
  sudo -n test "/proc/$pid/root" -ef "$root"
  sudo -n test "/proc/$pid/exe" -ef "$root/app/bin/po-broker"
  for n in "${window_units[@]}"; do
    case "$n" in "$po_service") execfile="$po_unit_file" ;; "$po_socket") execfile="$po_socket_file" ;; *) execfile="$unit_file" ;; esac
    [[ "$(native_window_prop "$n" FragmentPath)" == "$execfile" ]]
    sudo -n cmp -s "$execfile" "$old_target/${execfile##*/}"
  done
}

native_window_stop_original() {
  local LC_ALL=C n since id persistent state job before
  local -A generation=()
  for n in "${window_units[@]}"; do
    id="$(native_window_prop "$n" InvocationID)"; job="$(native_window_prop "$n" Job)"
    persistent="$(native_window_prop "$n" UnitFileState)"; state="$(native_window_prop "$n" ActiveState)"
    [[ "$id" =~ ^[0-9a-f]{32}$ && "$id" != 00000000000000000000000000000000 && -z "$job" && "$state" == active &&
       ( "$persistent" == enabled || "$persistent" == disabled ) ]]
    generation[$n]="$id"
    systemctl show "$n" -p Id -p InvocationID -p MainPID -p ControlPID -p UnitFileState -p ActiveState |
      sudo -n tee "$window/$n.before" >/dev/null
    sudo -n grep -Fxq "InvocationID=$id" "$window/$n.before"
  done
  native_window_verify_original
  native_window_barriers arm
  native_window_barriers check
  for n in "${window_units[@]}"; do [[ "$(native_window_prop "$n" InvocationID)" == "${generation[$n]}" ]]; done
  # socket의 Requires 관계가 issuer를 함께 정지시킨다. 둘의 원 invocation을 같은 job 구간에서 증명한다.
  since="@$EPOCHREALTIME"
  sudo -n systemctl stop "$po_socket" "$po_service"
  for n in "${window_units[@]}"; do
    if [[ "$n" == "$unit" ]]; then
      [[ "$(native_window_prop "$n" InvocationID)" == "${generation[$n]}" ]]
      since="@$EPOCHREALTIME"
      sudo -n systemctl stop "$n"
    fi
    # socket은 시작 메시지 없이 바로 완료할 수 있다. 원 invocation의 단일 완료 job만 인정한다.
    before="$(sudo -n journalctl --boot --quiet --no-pager -o json --output-fields=INVOCATION_ID,JOB_ID,JOB_RESULT --since="$since" \
      _PID=1 "UNIT=$n" "INVOCATION_ID=${generation[$n]}" JOB_TYPE=stop JOB_RESULT=done MESSAGE_ID=9d1aaa27d60140bd96365438aad20286)"
    python3 -I -B -c 'import json,sys; r=json.load(sys.stdin); j=r["JOB_ID"]; assert r["INVOCATION_ID"]==sys.argv[1] and r["JOB_RESULT"]=="done" and j.isdecimal() and int(j)>0' "${generation[$n]}" <<< "$before"
    if [[ "$n" == "$unit" ]]; then
      sudo -n journalctl --boot --quiet --no-pager --grep='\byoutube_collector_stopped\b' -o json \
        --output-fields=_SYSTEMD_INVOCATION_ID --since="$since" "_SYSTEMD_UNIT=$n" "_SYSTEMD_INVOCATION_ID=${generation[$n]}" |
        python3 -I -B -c 'import json,sys; assert json.load(sys.stdin)["_SYSTEMD_INVOCATION_ID"] == sys.argv[1]' "${generation[$n]}"
    fi
    native_window_terminal "$n"
    printf '%s\n' "$before" | sudo -n tee "$window/$n.stopped" >/dev/null
  done
}

native_window_record() { printf '%s\n' "$window_name" "$old_target" "$original_port" "$release_id" "$EXPECTED_REVISION" "$window_digest" "$native_claim_nonce"; }

native_window_quiesce() {
  local old_previous_target old_revision
  native_window_pins
  native_recovery_require_clear
  native_release_dir_resolve "$releases_root" "$release_id" "$current_link" >/dev/null
  old_previous_target="$(native_window_previous)"
  [[ "$(native_root_endpoint "$host_env")" == "100.100.1.8|$original_port" &&
     "$(native_payload_digest "$payload")" == "$window_digest" ]]
  old_revision="$(cat "$old_target/po-sandbox/revision")"
  [[ "$old_revision" =~ ^[0-9a-f]{40}$ && "$old_revision" != "$EXPECTED_REVISION" ]]
  sudo -n test ! -e "$old_target/rollback-contract"
  sudo -n test ! -L "$old_target/rollback-contract"
  native_cutover_claim "$releases_root/$release_id"
  sudo -n mkdir -m 0700 "$window"
  native_window_boot | sudo -n tee "$window/boot" >/dev/null
  native_window_record | sudo -n tee "$window/pins" >/dev/null
  sudo -n touch "$window/blocked"
  native_snapshot_before_cutover
  sudo -n sha256sum "$old_target/rollback-contract/SHA256SUMS" | sudo -n tee "$window/inputs.sha256" >/dev/null
  native_window_inputs
  native_cutover_run window-stop native_window_stop_original
  native_window_inputs
  sudo -n sync -f "${unit_file%/*}"
  sudo -n sync -f "$window"
  native_window_boot | sudo -n cmp -s - "$window/boot"
  native_window_original_seal | sudo -n tee "$window/stopped.tmp" >/dev/null
  sudo -n ln -T "$window/stopped.tmp" "$window/quiesced"
  sudo -n sync -f "$window"
  printf 'native a %s quiesced: nonce=%s hold=%s\n' "$window_name" "$native_claim_nonce" "$window"
}

native_window_resume_claim() {
  local n owner="$BASHPID"
  native_window_pins
  [[ "$window_nonce" =~ ^[0-9a-f-]{36}$ && "$(sudo -n cat "$native_recovery_dir/claim")" == "$window_nonce" &&
     "$(sudo -n cat "$native_recovery_dir/release")" == "$releases_root/$release_id" &&
     "$(sudo -n cat "$native_recovery_dir/step")" == window-stop && "$(sudo -n cat "$native_recovery_dir/terminal")" == 0 ]]
  sudo -n test ! -e "$native_recovery_dir/outcome"
  native_claim_nonce="$window_nonce"
  native_window_verify_quiesced
  [[ "$(native_payload_digest "$payload")" == "$window_digest" ]]
  sudo -n test -f "$window/blocked"
  native_window_inputs
  native_window_barriers check
  for n in "${window_units[@]}"; do native_window_terminal "$n"; done
  sudo -n mkdir -m 0700 "$window/resuming"
  printf '%s\n' "$owner" | sudo -n tee "$native_recovery_dir/owner-pid" >/dev/null
}

native_window_admit_candidate() {
  local n
  [[ "$(realpath -e "$current_link")" == "$release_dir" ]]
  for n in "$unit_file" "$po_unit_file" "$po_socket_file"; do sudo -n cmp -s "$n" "$release_dir/${n##*/}"; done
  sudo -n cmp -s "$host_env" "$release_dir/youtube-collector-host.env"
  native_window_barriers check
  [[ "$(native_window_prop "$po_service" RootDirectory)" == "$release_dir/po-sandbox/rootfs" &&
     "$(native_window_prop "$po_service" FragmentPath)" == "$po_unit_file" &&
     "$(native_window_prop "$po_socket" FragmentPath)" == "$po_socket_file" &&
     "$(native_window_prop "$unit" FragmentPath)" == "$unit_file" ]]
  for n in "${window_units[@]}"; do native_window_terminal "$n"; done
  sudo -n rm -- "$window/blocked"
}

native_window_live_generation() {
  local n="$1" job state id
  job="$(native_window_prop "$n" Job)" && state="$(native_window_prop "$n" ActiveState)" &&
    id="$(native_window_prop "$n" InvocationID)" && [[ -z "$job" && "$state" == active &&
    "$id" =~ ^[0-9a-f]{32}$ && "$id" != 00000000000000000000000000000000 ]] && printf '%s\n' "$id"
}

native_window_owned() {
  local n="$1" boot job id receipt
  boot="$(native_window_boot)" && receipt="$(sudo -n cat "$window/$n.started")" || return 1
  [[ "$receipt" =~ ^([0-9a-f]{32})[[:blank:]]([1-9][0-9]*)[[:blank:]]([0-9a-f]{32})$ ]] || return 1
  job="${BASH_REMATCH[2]}" id="${BASH_REMATCH[3]}"
  [[ "${BASH_REMATCH[1]}" == "$boot" && "$(native_window_live_generation "$n")" == "$id" ]] || return 1
  printf '%s\n' "$job" | sudo -n cmp -s - "$window/$n.job"
}

native_window_candidates() {
  local n
  for n in "$@"; do native_window_owned "$n" || { native_window_uncertain; return 1; }; done
}

native_window_start() {
  local LC_ALL=C n="$1" boot since permit transaction line job='' record id client=0 client_unit
  boot="$(native_window_boot)"
  printf '%s\n' "$boot" | sudo -n cmp -s - "$window/boot"
  native_window_terminal "$n"
  permit="$(native_window_permit "$n")"
  sudo -n test ! -e "$permit"
  sudo -n test ! -L "$permit"
  sudo -n mkdir -m 0700 -- "$window/$n.requesting"
  since="@$EPOCHREALTIME"
  # 이 unit만 통과시키는 /run 디렉터리는 manager가 시작 client의 수명 동안만 유지한다. 부팅으로 승계되지 않는다.
  client_unit="hololive-native-start-${native_claim_nonce}-$n.service"
  sudo -n systemd-run --quiet --wait --collect --unit="$client_unit" --property=RuntimeMaxSec=120s --property=KillMode=control-group \
    --property=StandardOutput=journal --property=StandardError=journal \
    --property=RuntimeDirectoryPreserve=no --property=RuntimeDirectoryMode=0700 \
    --property="RuntimeDirectory=${permit##*/}" --setenv=LC_ALL=C --setenv=SYSTEMD_LOG_TARGET=console \
    --setenv=SYSTEMD_LOG_COLOR=0 --setenv=SYSTEMD_LOG_LEVEL=info \
    systemctl --job-mode=fail --show-transaction start "$n" || client=$?
  if (( client != 0 )); then
    printf '후보 기동 결과 미확정: %s (client=%d)\n' "$n" "$client" >&2
    return 1
  fi
  sudo -n test ! -e "$permit"
  transaction="$(sudo -n journalctl --boot="$boot" --quiet --no-pager --since="$since" -o cat "_SYSTEMD_UNIT=$client_unit")"
  while IFS= read -r line; do
    [[ "$line" == 'Enqueued anchor job '* ]] || continue
    [[ -z "$job" && "$line" =~ ^Enqueued\ anchor\ job\ ([1-9][0-9]*)\ (.*)/start\.$ && "${BASH_REMATCH[2]}" == "$n" ]] || return 1
    job="${BASH_REMATCH[1]}"
  done <<< "$transaction"
  [[ -n "$job" ]]
  printf '%s\n' "$job" | sudo -n tee "$window/$n.job" >/dev/null
  record="$(sudo -n journalctl --boot="$boot" --quiet --no-pager -o json --output-fields=_BOOT_ID,INVOCATION_ID,JOB_ID \
    --since="$since" _PID=1 "UNIT=$n" "JOB_ID=$job" JOB_TYPE=start JOB_RESULT=done MESSAGE_ID=39f53479d3a045ac8e11786248231fbf)"
  id="$(python3 -I -B -c 'import json,re,sys; r=json.load(sys.stdin); i=r["INVOCATION_ID"]; assert r["_BOOT_ID"]==sys.argv[1] and r["JOB_ID"]==sys.argv[2] and re.fullmatch("[0-9a-f]{32}",i) and int(i,16); print(i)' "$boot" "$job" <<< "$record")"
  [[ "$(native_window_live_generation "$n")" == "$id" ]]
  sudo -n awk -F= -v id="$id" '$1=="InvocationID" {count++; old=$2} END {exit !(count==1 && ("id:" old)!=("id:" id))}' "$window/$n.before"
  printf '%s %s %s\n' "$boot" "$job" "$id" | sudo -n tee "$window/$n.started" >/dev/null
  native_window_candidates "$n"
}

native_window_candidate_seal() {
  local n
  local -a files=("$window/quiesced")
  for n in "${window_units[@]}"; do files+=("$window/$n.job" "$window/$n.started"); done
  sudo -n sha256sum -- "${files[@]}"
}

native_window_await_observation() {
  native_window_candidates "${window_units[@]}"
  native_window_candidate_seal | sudo -n tee "$window/observed.tmp" >/dev/null
  sudo -n sync -f "$window"
  # 별도 SSH가 영수증을 인수한 뒤 원 SSH의 ERR 복원이 경쟁해서는 안 된다.
  trap - ERR HUP INT TERM
  sudo -n ln -T "$window/observed.tmp" "$window/await-observation"
  sudo -n sync -f "$window"
}

native_window_candidate_runtime() {
  local n="$1" pid exe
  if [[ "$n" == "$po_socket" ]]; then
    sudo -n cmp -s "$po_socket_file" "$release_dir/$po_socket"
    return
  fi
  pid="$(native_window_prop "$n" MainPID)" && [[ "$pid" =~ ^[1-9][0-9]*$ ]] || return 1
  if [[ "$n" == "$po_service" ]]; then
    sudo -n test "/proc/$pid/root" -ef "$release_dir/po-sandbox/rootfs" || return 1
    exe="$release_dir/po-sandbox/rootfs/app/bin/po-broker"
  else
    exe="$release_dir/bin/youtube-collector"
  fi
  sudo -n test "/proc/$pid/exe" -ef "$exe"
}

native_window_rehold() {
  local n state policy collector_open=false
  local -a opened=()
  sudo -n touch "$window/blocked"
  policy="$(sudo -n readlink "$window/policy")"
  if [[ "$policy" == permanent ]]; then native_window_barriers rearm; fi
  native_window_barriers check
  native_window_verify_quiesced || { native_window_uncertain; return 1; }
  sudo -n test ! -e "$native_recovery_dir/outcome"
  for n in "${window_units[@]}"; do
    sudo -n test ! -e "$(native_window_permit "$n")" || { native_window_uncertain; return 1; }
    state="$(native_window_prop "$n" ActiveState)" || { native_window_uncertain; return 1; }
    if [[ "$state" == active ]]; then
      [[ "$(realpath -e "$current_link")" == "$release_dir" ]] || { native_window_uncertain; return 1; }
      native_window_candidates "$n"
      native_window_candidate_runtime "$n" || { native_window_uncertain; return 1; }
      opened+=("$n")
    else
      # 실행 요청이 있었으나 그 세대가 사라졌다면 이미 정지된 것으로 단정하지 않는다.
      sudo -n test ! -e "$window/$n.requesting" && native_window_terminal "$n" || { native_window_uncertain; return 1; }
    fi
  done
  if (( ${#opened[@]} )); then
    native_window_candidates "${opened[@]}"
    if [[ "${opened[-1]}" == "$unit" ]]; then collector_open=true; unset 'opened[-1]'; fi
    if (( ${#opened[@]} )); then sudo -n systemctl --job-mode=fail stop "${opened[@]}" || { native_window_uncertain; return 1; }; fi
    if [[ "$collector_open" == true ]]; then
      native_window_candidates "$unit"
      sudo -n systemctl --job-mode=fail stop "$unit" || { native_window_uncertain; return 1; }
    fi
  fi
  for n in "${window_units[@]}"; do native_window_terminal "$n" brake || { native_window_uncertain; return 1; }; done
  echo "후보 정지 확인, 구 publisher 복원 금지: $window" >&2
}

native_window_commit_policy() {
  local n pid
  native_window_candidates "${window_units[@]}"
  pid="$(native_window_prop "$unit" MainPID)"; [[ "$pid" =~ ^[1-9][0-9]*$ ]]
  for n in "${window_units[@]}"; do native_window_candidate_runtime "$n" || { native_window_uncertain; return 1; }; done
  native_window_process_endpoint "$pid" 6433
  native_window_barriers check
  native_window_barriers finish
  native_window_barriers permanent
  native_window_candidates "${window_units[@]}"
}

native_window_close_observation() {
  local owner="$BASHPID"
  case "$window_action" in accepted|observation-failed) ;; *) return 2 ;; esac
  release_dir="$releases_root/$release_id"
  native_window_pins "$release_dir"
  [[ "$window_nonce" =~ ^[0-9a-f-]{36}$ && "$(sudo -n cat "$native_recovery_dir/claim")" == "$window_nonce" &&
     "$(sudo -n cat "$native_recovery_dir/release")" == "$release_dir" ]]
  sudo -n test ! -e "$native_recovery_dir/outcome"
  native_claim_nonce="$window_nonce"
  native_window_record | sudo -n cmp -s - "$window/pins"
  [[ "$(sudo -n cat "$native_recovery_dir/step")" == collector-start && "$(sudo -n cat "$native_recovery_dir/terminal")" == 0 ]]
  sudo -n test -f "$window/await-observation"
  sudo -n mkdir -m 0700 "$window/closing"
  printf '%s\n' "$owner" | sudo -n tee "$native_recovery_dir/owner-pid" >/dev/null
  native_window_verify_quiesced || { native_window_uncertain; return 1; }
  native_window_candidate_seal | sudo -n cmp -s - "$window/await-observation" || { native_window_uncertain; return 1; }
  native_window_candidates "${window_units[@]}"
  local status
  if [[ "$window_action" == observation-failed ]]; then
    set +e
    native_cutover_run observation-brake native_window_rehold
    status=$?
    set -e
    if (( status != 0 )); then native_window_uncertain; fi
  else
    # permanent 정책을 reload해도 guard의 accepting이 남은 동안에는 부팅·재기동할 수 없다.
    sudo -n mkdir -m 0700 "$native_recovery_dir/accepting"
    sudo -n sync -f "$native_recovery_dir"
    # 작업 함수 안의 errexit을 보존한 채 감독자가 종료 영수증을 받는다.
    set +e
    native_cutover_run accept native_window_commit_policy
    status=$?
    set -e
    if (( status != 0 )); then
      if [[ "${native_completion_verified:-false}" == true ]] && sudo -n test ! -e "$native_recovery_dir/outcome"; then
        native_cutover_run acceptance-brake native_window_rehold
      fi
      return "$status"
    fi
    native_window_candidates "${window_units[@]}"
    # 원자적 rename 한 번이 시작 권한과 guard를 함께 넘긴다. 중단된 수용은 accepting을 남긴다.
    sudo -n python3 -I -B - "$native_recovery_dir" "$window/accepted-guard" "$window_nonce" "$owner" "$release_dir" <<'PY'
from pathlib import Path
import os, sys
g, dest = map(Path, sys.argv[1:3])
assert not dest.exists() and not dest.is_symlink() and not g.is_symlink()
assert {p.name for p in g.iterdir()} == {'accepting','owner-pid','worker-pid','claim','release','step','terminal'}
assert (g/'accepting').is_dir() and all(not p.is_symlink() for p in g.iterdir())
for name, value in zip(('claim','owner-pid','release','step','terminal'), (*sys.argv[3:], 'accept', '0')):
    assert (g/name).read_text() == value+'\n'
os.rename(g,dest)
for path in (dest.parent, g.parent):
    fd=os.open(path,os.O_RDONLY|os.O_DIRECTORY)
    try: os.fsync(fd)
    finally: os.close(fd)
PY
  fi
}
