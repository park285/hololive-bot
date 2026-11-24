# shellcheck shell=bash
native_release_window=W3

native_payload_digest() (
  set -o pipefail
  LC_ALL=C tar --sort=name --format=gnu --hard-dereference --numeric-owner --owner=0 --group=0 --mtime=@0 \
    -cf - -C "$1" . | sha256sum | cut -d' ' -f1
)

native_window_identity() {
  sudo -n python3 -I -B - "${current_link?}" "${releases_root?}" "${native_recovery_dir?}" "$native_release_window" "$@" <<'PY'
from pathlib import Path
import os, re, stat, sys

current, root, guard = map(Path, sys.argv[1:4])
family, action, expected_id, expected_rev = sys.argv[4:]

def owned(path, kind):
    s = path.lstat()
    writable = s.st_mode & stat.S_IWOTH or (s.st_mode & stat.S_IWGRP and s.st_gid != os.getegid())
    if not kind(s.st_mode) or s.st_uid != os.geteuid() or (not stat.S_ISLNK(s.st_mode) and writable):
        sys.exit(f'수용 경로의 소유권·형식 불일치: {path}')
    return s

owned(root.parent, stat.S_ISDIR)
owned(root, stat.S_ISDIR)
owned(current, stat.S_ISLNK)
release = current.resolve(strict=True)
assert release.parent == root and re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]{0,127}', release.name)
assert Path(os.path.abspath(current.parent / os.readlink(current))) == release
s = owned(release, stat.S_ISDIR)
assert not expected_id or release.name == expected_id
assert not os.path.lexists(release/'native-window')
owned(release/'po-sandbox', stat.S_ISDIR)
rev = release/'po-sandbox/revision'
owned(rev, stat.S_ISREG)
revision = rev.read_bytes().removesuffix(b'\n').decode('ascii')
assert re.fullmatch('[0-9a-f]{40}', revision) and (not expected_rev or expected_rev == revision)
marker = release/'accepted-window'

def identity(w, record):
    return f'{w}\n{release.name}\n{revision}\n{s.st_dev}:{s.st_ino}:{record.st_ino}\n'.encode('ascii')

if action == 'stage':
    assert family in ('W1','W2','W3') and expected_id and expected_rev and not os.path.lexists(marker)
    owned(guard, stat.S_ISDIR)
    with os.fdopen(os.open(guard/'accepted-window', os.O_WRONLY|os.O_CREAT|os.O_EXCL, 0o600), 'wb') as f:
        f.write(identity(family, os.fstat(f.fileno())))
        f.flush()
        os.fsync(f.fileno())
elif action == 'quiesce' and family == 'W1':
    assert not os.path.lexists(marker) and not os.path.lexists(release/'native-observation')
else:
    assert action in ('apply','quiesce') and family in ('W1','W2','W3')
    record = owned(marker, stat.S_ISREG)
    assert stat.S_IMODE(record.st_mode) == 0o600
    origins = {('quiesce','W2'): ('W1','W2'), ('quiesce','W3'): ('W2','W3'), ('apply','W3'): ('W2','W3')}
    families = origins.get((action, family), (family,))
    data = marker.read_bytes()
    assert data in [identity(w, record) for w in families], '현재 릴리스는 요청한 창에서 수용되지 않았습니다'
    print(' '.join(data.decode('ascii').splitlines()[1:]))
PY
}

native_apply_preflight() {
  [[ "$(hostname -s)" == osaka1 && "${service?}" == "${slot_service?}" ]] || return 1
  native_recovery_require_clear && sudo -n test ! -L "$native_recovery_dir" || return 1
  native_window_identity apply "${1:-}" ''
}

native_window_pins() {
  [[ "${window_name?}" == "$native_release_window" ]] || return 1
  [[ "$(hostname -s)" == osaka1 && "$service" == "$slot_service" && "$EXPECTED_REVISION" =~ ^[0-9a-f]{40}$ &&
     "${window_digest?}" =~ ^[0-9a-f]{64}$ && "${window_old?}" != "${release_id?}" ]] || return 1
  native_release_id_validate "$window_old" && native_release_id_validate "$release_id" || return 1
  original_port=5433
  old_target="$releases_root/$window_old"
  window="$old_target/native-window"
  [[ -L "$current_link" && "$(realpath -e "$current_link")" == "${1:-$old_target}" ]] || return 1
  window_units=("${po_socket?}" "${po_service?}" "${unit?}")
}

native_window_previous() {
  if [[ -L "${previous_link?}" ]]; then
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
  : "${slot_host_env?}" "${slot_collector_unit?}"
  local contract="$old_target/rollback-contract"
  native_profile_pair_validate "$old_target" "${host_env?}"
  sudo -n install -d -m 0755 -o root -g root "$contract"
  po_snapshot_previous "$old_target"
  printf '%s\n' "${old_previous_target:-absent}" | sudo -n tee "$contract/previous-before-cutover" >/dev/null
  sudo -n chmod 0644 "$contract/previous-before-cutover"
  sudo -n install -m 0640 -o root -g root "$host_env" "$contract/${slot_host_env##*/}"
  sudo -n install -m 0644 -o root -g root "${unit_file?}" "$contract/${slot_collector_unit%%@*}@.service"
  sudo -n sh -ec '
    cd "$1"
    for f in bin/hololive-collector bin/hololive-collector-wrapper bin/healthcheck; do test -x "$f"; done
    test -f youtubejs/src/server.mjs
    sha256sum bin/hololive-collector bin/hololive-collector-wrapper bin/healthcheck worker-profile.json SHA256SUMS \
      "rollback-contract/$2" "rollback-contract/$3" \
      rollback-contract/po-unit-presence rollback-contract/previous-before-cutover > rollback-contract/SHA256SUMS
    LC_ALL=C find youtubejs/src -type f -exec sha256sum {} + >> rollback-contract/SHA256SUMS
    if test -f "rollback-contract/$4"; then
      sha256sum "rollback-contract/$4" "rollback-contract/$5" >> rollback-contract/SHA256SUMS
    fi
    chmod 0644 rollback-contract/SHA256SUMS
  ' sh "$old_target" "${slot_host_env##*/}" "${slot_collector_unit%%@*}@.service" "$po_service" "$po_socket"
}

native_window_prop() { systemctl show "$1" --property="$2" --value; }

native_window_terminal() {
  local target="$1" job control state
  job="$(native_window_prop "$target" Job)" && control="$(native_window_prop "$target" ControlPID)" || return 1
  state="$(native_window_prop "$target" ActiveState)" || return 1
  [[ -z "$job" && "$control" == 0 && "$state" == inactive && "$(native_window_prop "$target" Result)" == success ]] || return 1
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

native_window_rotation_hook() {
  cat <<'HOOK'
set -eu
umask 077
action=$1 lease=$2 unit=$3 blocked=$4
job_id() { /usr/bin/timeout -k 1 3 /usr/bin/systemctl --system show "$unit" --property=Job --value; }
case "$action" in
  grant)
    [[ "${SERVICE_RESULT:-}:${EXIT_CODE:-}:${EXIT_STATUS:-}" == success:exited:0 && -d "$lease" && ! -e "$blocked" ]] || exit 0
    existing=$(job_id)
    [[ -z "$existing" ]] || exit 0
    # stop-post가 끝나기 전에는 이 job의 start-pre를 실행할 수 없다.
    path=$(/usr/bin/timeout -k 1 3 /usr/bin/busctl --system call org.freedesktop.systemd1 \
      /org/freedesktop/systemd1 org.freedesktop.systemd1.Manager StartUnit ss "$unit" fail)
    [[ "$path" =~ ^o\ \"/org/freedesktop/systemd1/job/([1-9][0-9]*)\"$ ]]
    job=${BASH_REMATCH[1]}
    [[ "$INVOCATION_ID" =~ ^[0-9a-f]{32}$ ]]
    set -C
    printf '%s %s\n' "$job" "$INVOCATION_ID" >"$lease/next"
    ;;
  consume)
    [[ ! -e "$blocked" && "$INVOCATION_ID" =~ ^[0-9a-f]{32}$ ]]
    read -r want previous <"$lease/next"
    [[ "$want" =~ ^[1-9][0-9]*$ && "$previous" =~ ^[0-9a-f]{32}$ && "$previous" != "$INVOCATION_ID" && "$want" == "$(job_id)" ]]
    /usr/bin/ln -T -- "$lease/next" "$lease/$INVOCATION_ID"
    /usr/bin/rm -- "$lease/next"
    ;;
  *) exit 2 ;;
esac
HOOK
}

native_window_barrier_body() {
  printf '[Unit]\nConditionPathExists=!%s/native-window\n' "$current_link"
  printf 'ConditionPathExists=!%s/accepting\n' "$native_recovery_dir"
  if [[ "$2" != permanent ]]; then
    local permit
    permit="$(native_window_permit "$1")"
    if [[ "$1" == "$po_service" && "$2" == observing ]]; then permit="$(native_window_permit rotation)/next"; fi
    printf 'ConditionPathExists=!%s/blocked\nConditionPathExists=%s\n' "$window" "$permit"
    if [[ "$1" == "$po_service" && "$2" == observing ]]; then
      printf '[Service]\nRestart=no\nRestartForceExitStatus=\nSuccessExitStatus=\nRootDirectoryStartOnly=yes\n'
      printf 'ExecStartPre=+/usr/bin/bash %s/issuer-rotation.sh consume %s %s %s/blocked\n' "$window" "${permit%/next}" "$1" "$window"
      printf 'ExecStopPost=+/usr/bin/bash %s/issuer-rotation.sh grant %s %s %s/blocked\n' "$window" "${permit%/next}" "$1" "$window"
    elif [[ "$1" != "$po_socket" ]]; then
      printf '[Service]\nRestart=no\nRestartForceExitStatus=\n'
    fi
  elif [[ "$1" != "$po_socket" ]]; then
    printf '[Service]\n'
    if [[ "$1" == "$po_service" ]]; then printf 'RootDirectoryStartOnly=yes\n'; fi
    printf 'ExecCondition=+/usr/bin/test ! -e %s/native-window\n' "$current_link"
    printf 'ExecCondition=+/usr/bin/test ! -e %s/accepting\n' "$native_recovery_dir"
  fi
}

native_window_barriers() {
  local action="$1" n file directory link object policy force paths drop
  case "$action" in arm|check|rotate|observing|finish|permanent|rearm) ;; *) return 2 ;; esac
  shift
  if (( $# == 0 )); then set -- "${window_units[@]}"; fi
  local -a drops
  local -a conditions=("$current_link/native-window" "$native_recovery_dir/accepting")
  if [[ "$action" == arm ]]; then
    sudo -n mkdir -- "$window/held" "$window/observing" "$window/permanent"
    sudo -n ln -sT held "$window/policy"
    native_window_rotation_hook | sudo -n sh -ec 'umask 077; set -C; cat >"$1"' sh "$window/issuer-rotation.sh"
  elif [[ "$action" == finish || "$action" == rearm || "$action" == rotate ]]; then
    local next=permanent
    case "$action" in rearm) next=held ;; rotate) next=observing ;; esac
    sudo -n ln -sT "$next" "$window/policy.$next"
    sudo -n mv -T "$window/policy.$next" "$window/policy"
    sudo -n systemctl daemon-reload
    return
  fi
  if [[ "$action" != permanent ]]; then conditions+=("$window/blocked"); fi
  for n in "$@"; do
    directory="${unit_file%/*}/$n.d"
    file="$window/policy/$n.conf"
    link="$directory/90-native-window-$native_claim_nonce.conf"
    if [[ "$action" == arm ]]; then
      sudo -n sh -ec '
        test ! -L "$1"
        test "$(readlink -e -- "${1%/*}")" = "${1%/*}"
        if test -e "$1"; then
          test -d "$1" && test "$(stat -c "%u:%g:%a" "$1")" = "$(id -u):$(id -g):755"
        else
          mkdir -m 0755 -- "$1"
        fi
      ' sh "$directory"
      native_window_barrier_body "$n" held | sudo -n tee "$window/held/$n.conf" >/dev/null
      native_window_barrier_body "$n" observing | sudo -n tee "$window/observing/$n.conf" >/dev/null
      native_window_barrier_body "$n" permanent | sudo -n tee "$window/permanent/$n.conf" >/dev/null
      sudo -n ln -sT "$file" "$link"
    elif [[ "$action" == check || "$action" == observing || "$action" == permanent ]]; then
      policy="$action"
      if [[ "$action" == check ]]; then policy=held; fi
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
      object="$(busctl --system call org.freedesktop.systemd1 /org/freedesktop/systemd1 org.freedesktop.systemd1.Manager LoadUnit s "$n")"
      [[ "$object" =~ ^o\ \"(/org/freedesktop/systemd1/unit/[a-zA-Z0-9_]+)\"$ ]]
      local object_path="${BASH_REMATCH[1]}" permit=''
      if [[ "$action" != permanent ]]; then
        if [[ "$action" == observing && "$n" == "$po_service" ]]; then permit="$(native_window_permit rotation)/next"
        else permit="$(native_window_permit "$n")"; fi
      fi
      busctl --system --json=short get-property org.freedesktop.systemd1 "$object_path" org.freedesktop.systemd1.Unit Conditions |
        python3 -I -c 'import json,sys; a=json.load(sys.stdin); rows=[r[:4] for r in a["data"]]; assert a["type"]=="a(sbbsi)" and all(["ConditionPathExists",False,True,p] in rows for p in sys.argv[2:]) and (not sys.argv[1] or ["ConditionPathExists",False,False,sys.argv[1]] in rows)' "$permit" "${conditions[@]}"
      if [[ "$n" != "$po_socket" ]]; then
        policy="$(native_window_prop "$n" Restart)"; force="$(native_window_prop "$n" RestartForceExitStatus)"
        if [[ "$action" == permanent ]]; then
          [[ "$policy" == always && -z "$force" ]]
          if [[ "$n" == "$po_service" ]]; then [[ "$(native_window_prop "$n" RootDirectoryStartOnly)" == yes ]]; fi
          busctl --system --json=short get-property org.freedesktop.systemd1 "$object_path" org.freedesktop.systemd1.Service ExecConditionEx |
            python3 -I -c 'import json,sys; row=json.load(sys.stdin); a,b,n=sys.argv[1:]; expected=[["/usr/bin/test",["/usr/bin/test","!","-e",p],["privileged"]] for p in (a,b)]; assert row["type"]=="a(sasasttttuii)" and [r[:3] for r in row["data"]]==expected*int(n)' "$current_link/native-window" "$native_recovery_dir/accepting" "${#drops[@]}"
        elif [[ "$action" == observing && "$n" == "$po_service" ]]; then
          [[ "$policy" == no && -z "$force" && -z "$(native_window_prop "$n" SuccessExitStatus)" &&
             "$(native_window_prop "$n" RootDirectoryStartOnly)" == yes ]]
          sudo -n sh -ec 'test -f "$1"; test ! -L "$1"; test "$(stat -c "%u:%g:%a" "$1")" = "$(id -u):$(id -g):600"' sh "$window/issuer-rotation.sh"
          native_window_rotation_hook | sudo -n cmp -s - "$window/issuer-rotation.sh"
          busctl --system --json=short get-property org.freedesktop.systemd1 "$object_path" org.freedesktop.systemd1.Service ExecStartPreEx ExecStopPostEx |
            python3 -I -c 'import json,sys; w,p,n=sys.argv[1:]; rows=[json.loads(s) for s in sys.stdin]; assert len(rows)==2
for row,action in zip(rows,("consume","grant")):
    argv=["/usr/bin/bash",w+"/issuer-rotation.sh",action,p,n,w+"/blocked"]
    assert row["type"]=="a(sasasttttuii)" and len(row["data"])==1 and row["data"][0][:3]==[argv[0],argv,["privileged"]]' "$window" "${permit%/next}" "$n"
        else
          [[ "$policy" == no && -z "$force" ]]
        fi
      fi
    fi
  done
  if [[ "$action" == arm ]]; then sudo -n systemctl daemon-reload; fi
}

native_window_inputs() {
  local contract="$old_target/rollback-contract" n
  native_rollback_validate "$old_target"
  [[ "$(native_window_previous)" == "$(sudo -n cat "$contract/previous-before-cutover")" ]]
  sudo -n cmp -s "$host_env" "$contract/${slot_host_env##*/}"
  [[ "$(native_root_endpoint "$host_env")" == "100.100.1.8|$original_port" ]]
  for n in "$unit_file" "${po_unit_file?}" "${po_socket_file?}"; do sudo -n cmp -s "$n" "$contract/${n##*/}"; done
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
  sudo -n test "/proc/$pid/exe" -ef "$old_target/bin/hololive-collector"
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

native_window_stop_receipt() {
  local n="$1" id="$2" since="$3" record
  record="$(sudo -n journalctl --boot --quiet --no-pager -o json --output-fields=INVOCATION_ID,JOB_ID,JOB_RESULT --since="$since" \
    _PID=1 "UNIT=$n" "INVOCATION_ID=$id" JOB_TYPE=stop JOB_RESULT=done MESSAGE_ID=9d1aaa27d60140bd96365438aad20286)" || return 1
  python3 -I -B -c 'import json,sys; r=json.load(sys.stdin); j=r["JOB_ID"]; assert r["INVOCATION_ID"]==sys.argv[1] and r["JOB_RESULT"]=="done" and j.isdecimal() and int(j)>0' "$id" <<< "$record" &&
    printf '%s\n' "$record"
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
  since="@$EPOCHREALTIME"
  sudo -n systemctl stop "$po_socket" "$po_service"
  for n in "${window_units[@]}"; do
    if [[ "$n" == "$unit" ]]; then
      [[ "$(native_window_prop "$n" InvocationID)" == "${generation[$n]}" ]]
      since="@$EPOCHREALTIME"
      sudo -n systemctl stop "$n"
    fi
    before="$(native_window_stop_receipt "$n" "${generation[$n]}" "$since")"
    if [[ "$n" == "$unit" ]]; then
      sudo -n journalctl --boot --quiet --no-pager --grep='\bcollector_stopped\b' -o json \
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
  native_window_identity quiesce "$window_old" '' >/dev/null
  native_release_dir_resolve "$releases_root" "$release_id" "$current_link" >/dev/null
  old_previous_target="$(native_window_previous)"
  [[ "$(native_root_endpoint "$host_env")" == "100.100.1.8|$original_port" &&
     "$(native_payload_digest "${payload?}")" == "$window_digest" ]]
  old_revision="$(cat "$old_target/po-sandbox/revision")"
  [[ "$old_revision" =~ ^[0-9a-f]{40}$ && "$old_revision" != "$EXPECTED_REVISION" ]]
  sudo -n test ! -e "$old_target/rollback-contract"
  sudo -n test ! -L "$old_target/rollback-contract"
  native_cutover_claim "$releases_root/$release_id"
  native_window_identity quiesce "$window_old" '' >/dev/null
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
  [[ "${window_nonce?}" =~ ^[0-9a-f-]{36}$ && "$(sudo -n cat "$native_recovery_dir/claim")" == "$window_nonce" &&
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
  sudo -n cmp -s "$host_env" "$release_dir/${slot_host_env##*/}"
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

native_window_issuer_chain() (
  set -o pipefail
  local boot="$1" job="$2" first="$3" since="$4" current="$5" restarts="$6" mode="$7" lease
  [[ "$since" =~ ^@[0-9]+([.][0-9]+)?$ && "$restarts" == 0 ]] || return 1
  lease="$(native_window_permit rotation)"
  sudo -n journalctl --boot="$boot" --since="$since" --lines=4096 --quiet --no-pager -o json \
    --output-fields=_BOOT_ID,_PID,UNIT,INVOCATION_ID,MESSAGE_ID,JOB_ID,JOB_TYPE,JOB_RESULT,EXIT_CODE,EXIT_STATUS \
    _PID=1 "UNIT=$po_service" | sudo -n python3 -I -B -c '
from pathlib import Path
import json, os, re, stat, sys
boot, unit, first_job, first, current, mode, lease = sys.argv[1:]
start, clean = "39f53479d3a045ac8e11786248231fbf", "7ad2d189f7e94e70a38c781354912448"
jobs, ids = set(), set()
state, last = "initial", None
for line in sys.stdin:
    r = json.loads(line)
    assert r["_BOOT_ID"] == boot and r["_PID"] == "1" and r["UNIT"] == unit
    mid, inv, job = r.get("MESSAGE_ID"), r.get("INVOCATION_ID"), r.get("JOB_ID")
    if mid == start:
        assert r["JOB_TYPE"] == "start" and r["JOB_RESULT"] == "done"
        if mode == "retired" and state == "clean" and inv == last:
            continue
        assert state in ("initial","clean") and re.fullmatch("[1-9][0-9]*", job) and job not in jobs
        assert re.fullmatch("[0-9a-f]{32}", inv) and int(inv,16) and inv not in ids
        if state == "initial":
            assert (job,inv) == (first_job,first)
        else:
            receipt = Path(lease) / inv
            s = receipt.lstat()
            assert (s.st_uid,s.st_gid) == (os.geteuid(),os.getegid()) and stat.S_ISREG(s.st_mode) and stat.S_IMODE(s.st_mode) == 0o600
            assert receipt.read_text() == f"{job} {last}\n"
        jobs.add(job); ids.add(inv)
        state, last = "live", inv
    elif mid == clean:
        assert state == "live" and inv == last
        state = "clean"
    elif mid == "98e322203f7a4ed290d09fe03c09fe15":
        assert r["EXIT_CODE"] == "exited" and r["EXIT_STATUS"] == "0"
    else:
        assert mid not in ("5eb03494b6584870a536b337290809b3","be02cf6855d2428ba40df7e9d022f03d","d9b373ed55a64feb8242e02dbe79a49c","de5b426a63be47a7b6ac3eaac82e2f6f") and not r.get("JOB_RESULT")
assert last == current
assert (mode == "live" and state == "live") or (mode == "retired" and state == "clean")
' "$boot" "$po_service" "$job" "$first" "$current" "$mode" "$lease"
)

native_window_lease_deadline() {
  local permit lease started
  permit="$(native_window_permit rotation)" && lease="${permit##*/}.service"
  sudo -n test -d "$permit" && native_window_live_generation "$lease" | sudo -n cmp -s - "$window/issuer-lease" || return 1
  started="$(native_window_prop "$lease" ActiveEnterTimestampMonotonic)" || return 1
  [[ "$started" =~ ^[1-9][0-9]*$ ]] || return 1
  python3 -I -B -c 'import sys,time; expires=int(sys.argv[1])+900_000_000; assert time.monotonic_ns()//1000 < expires; print(expires)' "$started"
}

native_window_lease_live() { native_window_lease_deadline >/dev/null; }

native_window_open_rotation() {
  local permit lease
  native_window_candidates "$po_socket" "$po_service"
  native_window_candidate_runtime "$po_service"
  permit="$(native_window_permit rotation)" && lease="${permit##*/}.service"
  sudo -n test ! -e "$permit"
  sudo -n test ! -L "$permit"
  sudo -n systemd-run --quiet --collect --unit="$lease" --property=Type=exec --property=RuntimeMaxSec=900s \
    --property=RuntimeDirectoryPreserve=no --property=RuntimeDirectoryMode=0700 --property="RuntimeDirectory=${permit##*/}" /usr/bin/sleep infinity
  native_window_live_generation "$lease" | sudo -n tee "$window/issuer-lease" >/dev/null
  native_window_lease_live
  native_window_barriers rotate "$@"
  native_window_barriers observing "$@"
  native_window_candidates "$po_socket" "$po_service"
}

native_window_owned() {
  local n="$1" mode="${2:-live}" boot job id receipt current since count
  boot="$(native_window_boot)" && receipt="$(sudo -n cat "$window/$n.started")" || return 1
  [[ "$receipt" =~ ^([0-9a-f]{32})[[:blank:]]([1-9][0-9]*)[[:blank:]]([0-9a-f]{32})$ ]] || return 1
  job="${BASH_REMATCH[2]}" id="${BASH_REMATCH[3]}"
  [[ "${BASH_REMATCH[1]}" == "$boot" ]] && printf '%s\n' "$job" | sudo -n cmp -s - "$window/$n.job" || return 1
  if [[ "$mode" == retired ]]; then
    [[ "$n" == "$po_service" ]] && native_window_terminal "$n" &&
      [[ "$(native_window_prop "$n" ExecMainCode)" == 1 && "$(native_window_prop "$n" ExecMainStatus)" == 0 ]] || return 1
    current="$(native_window_prop "$n" InvocationID)" || return 1
  else
    current="$(native_window_live_generation "$n")" || return 1
  fi
  if [[ "$n" != "$po_service" ]]; then [[ "$current" == "$id" ]]; return; fi
  if [[ "$current" != "$id" ]]; then sudo -n test -f "$window/issuer-lease" || return 1; fi
  count="$(native_window_prop "$n" NRestarts)" || return 1
  since="$(sudo -n cat "$window/$n.since")" || return 1
  native_window_issuer_chain "$boot" "$job" "$id" "$since" "$current" "$count" "$mode" || return 1
  if [[ "$mode" == retired ]]; then
    native_window_terminal "$n" && [[ "$(native_window_prop "$n" InvocationID)" == "$current" ]]
    return
  fi
  native_window_candidate_runtime "$n" && [[ "$(native_window_live_generation "$n")" == "$current" ]]
}

native_window_probe_units() {
  local n
  for n in "$@"; do
    if [[ "$n" == "$po_service" ]]; then
      native_window_issuer_state live >/dev/null || return 1
    else
      native_window_owned "$n" || return 1
    fi
  done
}

native_window_candidates() {
  native_window_probe_units "$@" || { native_window_uncertain; return 1; }
}

native_window_start_receipt() {
  local n="$1" boot="$2" since="$3" job="${4:-}" record
  local -a filter=()
  if [[ -n "$job" ]]; then filter=("JOB_ID=$job"); fi
  record="$(sudo -n journalctl --boot="$boot" --quiet --no-pager -o json --output-fields=_BOOT_ID,INVOCATION_ID,JOB_ID \
    --since="$since" _PID=1 "UNIT=$n" "${filter[@]}" JOB_TYPE=start JOB_RESULT=done MESSAGE_ID=39f53479d3a045ac8e11786248231fbf)" || return 1
  python3 -I -B -c 'import json,re,sys; r=json.load(sys.stdin); i=r["INVOCATION_ID"]; j=r["JOB_ID"]; assert r["_BOOT_ID"]==sys.argv[1] and (not sys.argv[2] or j==sys.argv[2]) and re.fullmatch("[0-9a-f]{32}",i) and int(i,16) and re.fullmatch("[1-9][0-9]*",j); print(sys.argv[1],j,i)' "$boot" "$job" <<< "$record"
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
  if [[ "$n" == "$po_service" ]]; then printf '%s\n' "$since" | sudo -n tee "$window/$n.since" >/dev/null; fi
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
  record="$(native_window_start_receipt "$n" "$boot" "$since" "$job")"
  id="${record##* }"
  [[ "$(native_window_live_generation "$n")" == "$id" ]]
  sudo -n awk -F= -v id="$id" '$1=="InvocationID" {count++; old=$2} END {exit !(count==1 && ("id:" old)!=("id:" id))}' "$window/$n.before"
  printf '%s %s %s\n' "$boot" "$job" "$id" | sudo -n tee "$window/$n.started" >/dev/null
  native_window_candidates "$n"
}

native_window_candidate_seal() {
  local n
  local -a files=("$window/quiesced" "$window/issuer-lease" "$window/$po_service.since")
  for n in "${window_units[@]}"; do files+=("$window/$n.job" "$window/$n.started"); done
  sudo -n sha256sum -- "${files[@]}"
}

native_window_await_observation() {
  native_window_lease_live || { native_window_uncertain; return 1; }
  native_window_candidates "${window_units[@]}"
  native_window_candidate_seal | sudo -n tee "$window/observed.tmp" >/dev/null
  sudo -n sync -f "$window"
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
    [[ "$(native_window_prop "$n" RootDirectory)" == "$release_dir/po-sandbox/rootfs" &&
       "$(native_window_prop "$n" SubState)" == running && "$(native_window_prop "$n" ControlPID)" == 0 ]] || return 1
    sudo -n test "/proc/$pid/root" -ef "$release_dir/po-sandbox/rootfs" || return 1
    exe="$release_dir/po-sandbox/rootfs/app/bin/po-broker"
  else
    exe="$release_dir/bin/hololive-collector"
  fi
  sudo -n test "/proc/$pid/exe" -ef "$exe"
}

native_window_issuer_ready() {
  local current after ready start state deadline=$((SECONDS+90))
  while (( SECONDS < deadline )); do
    native_window_lease_live && native_window_probe_units "${window_units[@]}" || return 1
    current="$(native_window_live_generation "$po_service")" || continue
    state=cold
    if po_health_ready "$release_dir"; then
      state=not_ready
      if ready="$(collector_readiness_fetch)" && collector_readiness_validate "$ready" &&
          collector_readiness_json_field "$ready" instance_id "\"$service\""; then state=ready; fi
    fi
    native_window_probe_units "${window_units[@]}" || return 1
    after="$(native_window_live_generation "$po_service")" || continue
    if [[ "$after" != "$current" ]]; then continue; fi
    case "$state" in
      ready) return 0 ;;
      not_ready) return 1 ;;
    esac
    start="$(native_window_prop "$po_service" ExecMainStartTimestampMonotonic)" || return 1
    [[ "$start" =~ ^[1-9][0-9]*$ ]] || return 1
    python3 -I -B -c 'import sys,time; assert 0 <= time.monotonic_ns()//1000-int(sys.argv[1]) < 90_000_000' "$start" || return 1
    sleep 2
  done
  return 1
}

native_window_issuer_state() {
  local mode="$1" state attempt id pid
  for attempt in {1..6}; do
    state="$(native_window_prop "$po_service" ActiveState)" || return 1
    case "$state" in
      active)
        id="$(native_window_prop "$po_service" InvocationID)" || return 1
        if native_window_owned "$po_service"; then echo live; return; fi
        if [[ "$(native_window_prop "$po_service" ActiveState)" == active && "$(native_window_prop "$po_service" InvocationID)" == "$id" ]]; then
          pid="$(native_window_prop "$po_service" MainPID)" || return 1
          [[ "$pid" =~ ^[0-9]+$ ]] || return 1
          if [[ "$pid" != 0 ]] && sudo -n test -e "/proc/$pid/exe"; then return 1; fi
        fi
        ;;
      inactive)
        if [[ "$mode" == brake && -z "$(native_window_prop "$po_service" Job)" ]]; then
          native_window_owned "$po_service" retired && echo retired
          return
        fi
        ;;
      activating|deactivating)
        case "$(native_window_prop "$po_service" SubState)" in
          condition|start-pre|start|stop-post) ;;
          *) [[ "$(native_window_prop "$po_service" ActiveState)" != "$state" ]] || return 1 ;;
        esac
        ;;
      *) return 1 ;;
    esac
    (( attempt == 6 )) || sleep 1
  done
  return 1
}

native_window_rehold() {
  local n state policy issuer_requested=false collector_open=false issuer_state id since
  local -a opened=()
  sudo -n touch "$window/blocked"
  if sudo -n test -f "$window/issuer-lease"; then native_window_lease_live || { native_window_uncertain; return 1; }; fi
  policy="$(sudo -n readlink "$window/policy")"
  case "$policy" in
    held) native_window_barriers check ;;
    observing) native_window_barriers observing ;;
    permanent) sudo -n test -d "$native_recovery_dir/accepting"; native_window_barriers permanent ;;
    *) native_window_uncertain; return 1 ;;
  esac
  native_window_verify_quiesced || { native_window_uncertain; return 1; }
  sudo -n test ! -e "$native_recovery_dir/outcome"
  for n in "${window_units[@]}"; do
    sudo -n test ! -e "$(native_window_permit "$n")" || { native_window_uncertain; return 1; }
    if [[ "$n" == "$po_service" ]] && sudo -n test -d "$window/$n.requesting"; then
      [[ "$(realpath -e "$current_link")" == "$release_dir" ]] || { native_window_uncertain; return 1; }
      issuer_requested=true
      continue
    fi
    state="$(native_window_prop "$n" ActiveState)" || { native_window_uncertain; return 1; }
    if [[ "$state" == active ]]; then
      [[ "$(realpath -e "$current_link")" == "$release_dir" ]] || { native_window_uncertain; return 1; }
      native_window_candidates "$n"
      native_window_candidate_runtime "$n" || { native_window_uncertain; return 1; }
      opened+=("$n")
    else
      if ! { sudo -n test ! -e "$window/$n.requesting" && sudo -n test ! -e "$window/$n.started" &&
        sudo -n test ! -e "$window/$n.job" && native_window_terminal "$n"; }; then
        native_window_uncertain; return 1
      fi
    fi
  done
  if [[ "$policy" != held ]]; then
    if [[ "$issuer_requested" == true ]]; then native_window_issuer_state brake >/dev/null || { native_window_uncertain; return 1; }; fi
    native_window_barriers rearm
    native_window_barriers check
  fi
  if [[ "$issuer_requested" == true || ${#opened[@]} -gt 0 ]]; then
    native_window_candidates "${opened[@]}"
    if [[ ${#opened[@]} -gt 0 && "${opened[-1]}" == "$unit" ]]; then collector_open=true; unset 'opened[-1]'; fi
    if [[ "$issuer_requested" == true ]]; then
      issuer_state="$(native_window_issuer_state brake)" || { native_window_uncertain; return 1; }
      if [[ "$issuer_state" == live ]]; then opened+=("$po_service"); fi
    fi
    if (( ${#opened[@]} )); then sudo -n systemctl --job-mode=fail stop "${opened[@]}" || { native_window_uncertain; return 1; }; fi
    if [[ "$collector_open" == true ]]; then
      id="$(sudo -n cat "$window/$unit.started")" || { native_window_uncertain; return 1; }
      native_window_candidates "$unit"
      since="@$EPOCHREALTIME"
      sudo -n systemctl --job-mode=fail stop "$unit" || { native_window_uncertain; return 1; }
      native_window_stop_receipt "$unit" "${id##* }" "$since" >/dev/null || { native_window_uncertain; return 1; }
    fi
  fi
  for n in "${window_units[@]}"; do native_window_terminal "$n" || { native_window_uncertain; return 1; }; done
  echo "후보 정지 확인, 구 publisher 복원 금지: $window" >&2
}

native_window_commit_policy() {
  native_window_lease_live || { native_window_uncertain; return 1; }
  local n pid
  native_window_probe_units "${window_units[@]}"
  pid="$(native_window_prop "$unit" MainPID)"; [[ "$pid" =~ ^[1-9][0-9]*$ ]]
  for n in "$po_socket" "$unit"; do native_window_candidate_runtime "$n"; done
  native_window_process_endpoint "$pid" 5433
  native_window_lease_live || { native_window_uncertain; return 1; }
  native_window_barriers observing "$@"
  native_window_issuer_ready || { native_window_uncertain; return 1; }
  native_window_lease_live || { native_window_uncertain; return 1; }
  native_window_barriers finish "$@"
  native_window_barriers permanent "$@"
  native_window_probe_units "${window_units[@]}"
}

native_apply_before_stop() {
  local n
  [[ "$(native_window_identity apply "$window_old" '')" == "${apply_origin_pin?}" && "$old_target" == "$releases_root/$window_old" &&
     "$(sudo -n cat "$native_recovery_dir/claim")" == "$native_claim_nonce" &&
     "$(sudo -n cat "$native_recovery_dir/owner-pid")" == "$BASHPID" && "$(sudo -n cat "$native_recovery_dir/release")" == "$release_dir" ]]
  sudo -n test ! -e "$native_recovery_dir/outcome"
  [[ "$(native_root_endpoint "$host_env")" == '100.100.1.8|5433' ]]
  local release_dir="$old_target"
  for n in "${window_units[@]}"; do native_window_candidate_runtime "$n"; done
  native_window_process_endpoint "$(native_window_prop "$unit" MainPID)" 5433
}

native_apply_observation_record() { printf '%s\n' "$native_release_window" "$release_id" "$EXPECTED_REVISION" "$1"; }

native_apply_record_start() {
  local n="$1" boot entry job
  boot="$(native_window_boot)"
  entry="$(native_window_start_receipt "$n" "$boot" "${apply_start_since?}")"
  read -r _ job _ <<< "$entry"
  printf '%s\n' "$job" | sudo -n tee "$window/$n.job" >/dev/null
  printf '%s\n' "$entry" | sudo -n tee "$window/$n.started" >/dev/null
  native_window_candidates "$n"
}

native_apply_unarm_issuer() {
  local n link="${unit_file%/*}/$po_service.d/90-native-window-$native_claim_nonce.conf"
  [[ "$(realpath -e "$current_link")" == "$release_dir" ]] || return 1
  sudo -n test ! -e "$window/$po_socket.requesting" || return 1
  for n in "${window_units[@]}"; do native_window_terminal "$n" || return 1; done
  if sudo -n test -L "$link"; then
    [[ "$(sudo -n readlink "$link")" == "$window/policy/$po_service.conf" ]] || return 1
    sudo -n rm -- "$link"
  else
    sudo -n test ! -e "$link"
  fi
}

native_apply_start_issuer() {
  native_window_barriers arm "$po_service"
  native_window_barriers check "$po_service"
  sudo -n mkdir -m 0700 -- "$window/$po_socket.requesting"
  sudo -n systemctl enable --now "$po_socket"
  native_apply_record_start "$po_socket"
  native_window_start "$po_service"
  native_window_open_rotation "$po_service"
}

native_apply_await_observation() {
  native_apply_record_start "$unit"
  native_window_lease_live || { native_window_uncertain; return 1; }
  native_window_candidates "${window_units[@]}"
  native_apply_observation_record "$native_claim_nonce" | sudo -n tee "$window/await-observation" >/dev/null
  sudo -n sync -f "$window"
  printf '%s\n' "$native_claim_nonce"
}

native_apply_close_observation() {
  local owner="$BASHPID"
  [[ "$(hostname -s)" == osaka1 && "$service" == "$slot_service" && "$window_nonce" =~ ^[0-9a-f-]{36}$ &&
     "$(realpath -e "$current_link")" == "$release_dir" && "$(sudo -n cat "$native_recovery_dir/claim")" == "$window_nonce" &&
     "$(sudo -n cat "$native_recovery_dir/release")" == "$release_dir" && "$(sudo -n cat "$native_recovery_dir/step")" == collector-restart &&
     "$(sudo -n cat "$native_recovery_dir/terminal")" == 0 ]]
  sudo -n test ! -e "$native_recovery_dir/outcome"
  native_apply_observation_record "$window_nonce" | sudo -n cmp -s - "$window/await-observation"
  sudo -n mkdir -m 0700 "$window/closing"
  native_claim_nonce="$window_nonce"
  printf '%s\n' "$owner" | sudo -n tee "$native_recovery_dir/owner-pid" >/dev/null
  sudo -n mkdir -m 0700 "$native_recovery_dir/accepting"
  sudo -n sync -f "$native_recovery_dir"
  native_cutover_run accept native_window_commit_policy "$po_service"
  native_window_accept_guard accept
}

native_initial_accept() {
  local n persistent reserved
  [[ "$(hostname -s)" == osaka1 && "$service" == "$slot_service" && "$(realpath -e "$current_link")" == "$release_dir" &&
     "$(sudo -n cat "$release_dir/po-sandbox/revision")" == "$EXPECTED_REVISION" ]]
  sudo -n test ! -e "$previous_link"
  sudo -n test ! -L "$previous_link"
  for reserved in accepted-window native-window native-observation; do
    sudo -n test ! -e "$release_dir/$reserved"
    sudo -n test ! -L "$release_dir/$reserved"
  done
  native_recovery_require_clear
  for n in "${window_units[@]}"; do
    [[ "$(native_window_prop "$n" ActiveState)" == active ]]
    persistent="$(native_window_prop "$n" UnitFileState)"
    [[ "$persistent" == enabled || ( "$n" == "$po_service" && "$persistent" == disabled ) ]]
  done
  collector_readiness_validate "$(collector_readiness_fetch)"
  sudo -n mkdir -m 0700 "$native_recovery_dir"
  native_window_identity stage "$release_id" "$EXPECTED_REVISION"
  sudo -n python3 -I -B - "$native_recovery_dir" "$release_dir" <<'PY'
from pathlib import Path
import os, sys
guard, release = map(Path, sys.argv[1:3])
assert not guard.is_symlink() and {p.name for p in guard.iterdir()} == {'accepted-window'}
window = release/'native-observation'
os.mkdir(window, 0o700)
os.link(guard/'accepted-window', release/'accepted-window')
os.rename(guard, window/'accepted-guard')
for path in (window, release, guard.parent):
    fd = os.open(path, os.O_RDONLY|os.O_DIRECTORY)
    try: os.fsync(fd)
    finally: os.close(fd)
PY
}

native_window_accept_guard() {
  local step="$1" dest="$window/accepted-guard" expires permit status=0
  native_window_identity stage "$release_id" "$EXPECTED_REVISION"
  native_window_lease_live || { native_window_uncertain; return 1; }
  native_window_issuer_ready || { native_window_uncertain; return 1; }
  expires="$(native_window_lease_deadline)" || { native_window_uncertain; return 1; }
  permit="$(native_window_permit rotation)"
  sudo -n python3 -I -B - "$native_recovery_dir" "$dest" "$native_claim_nonce" "$owner" "$release_dir" "$step" "$current_link" "$permit" "$expires" <<'PY' || status=$?
from pathlib import Path
import os, sys, time
g, dest = map(Path, sys.argv[1:3])
nonce, owner, release, step, current, permit, expires = sys.argv[3:]
assert Path(current).resolve(strict=True) == Path(release)
assert not os.path.lexists(dest) and not g.is_symlink()
files = {'owner-pid','worker-pid','claim','release','step','terminal','accepted-window'}
if step == 'accept':
    files.add('accepting')
    assert (g/'accepting').is_dir()
assert {p.name for p in g.iterdir()} == files and all(not p.is_symlink() for p in g.iterdir())
for name, value in zip(('claim','owner-pid','release','step','terminal'), (nonce,owner,release,step,'0')):
    assert (g/name).read_text() == value+'\n'
os.link(g/'accepted-window', Path(release)/'accepted-window')
fd=os.open(release,os.O_RDONLY|os.O_DIRECTORY)
try: os.fsync(fd)
finally: os.close(fd)
if not Path(permit).is_dir() or time.monotonic_ns()//1000 >= int(expires):
    sys.exit(75)
os.rename(g,dest)
for path in (dest.parent, g.parent):
    fd=os.open(path,os.O_RDONLY|os.O_DIRECTORY)
    try: os.fsync(fd)
    finally: os.close(fd)
PY
  if (( status == 75 )); then native_window_uncertain; fi
  return "$status"
}

native_window_close_observation() {
  local owner="$BASHPID"
  case "${window_action?}" in accepted|observation-failed) ;; *) return 2 ;; esac
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
  native_window_lease_live || { native_window_uncertain; return 1; }
  local status
  if [[ "$window_action" == observation-failed ]]; then
    set +e
    native_cutover_run observation-brake native_window_rehold
    status=$?
    set -e
    if (( status != 0 )); then native_window_uncertain; fi
  else
    sudo -n mkdir -m 0700 "$native_recovery_dir/accepting"
    sudo -n sync -f "$native_recovery_dir"
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
    native_window_accept_guard accept
  fi
}
