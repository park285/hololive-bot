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
    assert family == 'W3' and expected_id and expected_rev and not os.path.lexists(marker)
    owned(guard, stat.S_ISDIR)
    with os.fdopen(os.open(guard/'accepted-window', os.O_WRONLY|os.O_CREAT|os.O_EXCL, 0o600), 'wb') as f:
        f.write(identity(family, os.fstat(f.fileno())))
        f.flush()
        os.fsync(f.fileno())
else:
    assert action == 'apply' and family == 'W3'
    record = owned(marker, stat.S_ISREG)
    assert stat.S_IMODE(record.st_mode) == 0o600
    data = marker.read_bytes()
    assert data == identity(family, record), '현재 릴리스는 요청한 창에서 수용되지 않았습니다'
    print(' '.join(data.decode('ascii').splitlines()[1:]))
PY
}

native_apply_preflight() {
  [[ "$(hostname -s)" == osaka1 && "${service?}" == "${slot_service?}" ]] || {
    echo "native preflight: host/service mismatch ($(hostname -s)/${service:-})" >&2
    return 1
  }
  if ! native_recovery_require_clear || ! sudo -n test ! -L "$native_recovery_dir"; then
    echo "native preflight: recovery guard present or symlink: $native_recovery_dir" >&2
    return 1
  fi
  native_window_identity apply "${1:-}" ''
}

native_window_previous() {
  if [[ -L "${previous_link?}" ]]; then
    local previous
    previous="$(realpath -e "$previous_link")" || return 1
    [[ "${previous%/*}" == "$releases_root" && "$previous" != "${old_target?}" && -d "$previous" ]] || return 1
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
  ' sh "$old_target" "${slot_host_env##*/}" "${slot_collector_unit%%@*}@.service" "${po_service?}" "${po_socket?}"
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
  sudo -n touch "${window?}/blocked"
  native_cutover_unknown
  return 1
}

native_window_original_seal() {
  local n
  local -a files=("$window/pins" "$window/inputs.sha256" "$window/boot")
  for n in "${window_units[@]?}"; do files+=("$window/$n.before" "$window/$n.stopped"); done
  sudo -n sha256sum -- "${files[@]}"
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

native_window_permanent_dropin() {
  printf '%s/%s.d/90-native-window-permanent.conf\n' "${unit_file%/*}" "$1"
}

native_window_ensure_dropin_dir() {
  sudo -n sh -ec '
    test ! -L "$1"
    test "$(readlink -e -- "${1%/*}")" = "${1%/*}"
    if test -e "$1"; then
      test -d "$1" && test "$(stat -c "%u:%g:%a" "$1")" = "$(id -u):$(id -g):755"
    else
      mkdir -m 0755 -- "$1"
    fi
  ' sh "$1"
}

native_window_install_permanent_dropin() {
  local n="$1" directory permanent_file
  directory="${unit_file%/*}/$n.d"
  permanent_file="$(native_window_permanent_dropin "$n")"
  native_window_ensure_dropin_dir "$directory"
  native_window_barrier_body "$n" permanent | sudo -n python3 -I -B -c '
from pathlib import Path
import os, stat, sys, tempfile

target = Path(sys.argv[1])
body = sys.stdin.buffer.read()
if not body:
    sys.exit("empty permanent drop-in body")
uid, gid = os.geteuid(), os.getegid()
if os.path.lexists(target):
    s = target.lstat()
    if not stat.S_ISREG(s.st_mode) or s.st_nlink != 1 or (s.st_uid, s.st_gid) != (uid, gid) or stat.S_IMODE(s.st_mode) != 0o644:
        sys.exit(f"unexpected permanent drop-in file metadata: {target}")
    if target.read_bytes() == body:
        sys.exit(0)
with tempfile.NamedTemporaryFile(dir=target.parent, prefix=".90-native-window-permanent.", delete=False) as tmp:
    name = tmp.name
    try:
        tmp.write(body)
        tmp.flush()
        os.fchown(tmp.fileno(), uid, gid)
        os.fchmod(tmp.fileno(), 0o644)
        os.fsync(tmp.fileno())
        os.replace(name, target)
        fd = os.open(target.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
    finally:
        if os.path.lexists(name):
            os.unlink(name)
' "$permanent_file"
}

native_window_prune_legacy_dropins() {
  local n="$1" directory permanent_file
  directory="${unit_file%/*}/$n.d"
  permanent_file="$(native_window_permanent_dropin "$n")"
  native_window_barrier_body "$n" permanent | sudo -n python3 -I -B -c '
from pathlib import Path
import os, stat, sys

directory, permanent = Path(sys.argv[1]), Path(sys.argv[2])
expected = sys.stdin.buffer.read()
if not expected:
    sys.exit("empty permanent drop-in body")
for entry in sorted(directory.iterdir()):
    if entry == permanent or not (entry.name.startswith("90-native-window-") and entry.name.endswith(".conf")):
        continue
    if not stat.S_ISLNK(entry.lstat().st_mode):
        sys.exit(f"unexpected non-symlink window drop-in: {entry}")
    if entry.read_bytes() != expected:
        sys.exit(f"refusing to remove non-permanent window drop-in: {entry}")
    entry.unlink()
fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
try:
    os.fsync(fd)
finally:
    os.close(fd)
' "$directory" "$permanent_file"
}

native_window_verify_permanent_dropin() {
  local n="$1" directory permanent_file
  directory="${unit_file%/*}/$n.d"
  permanent_file="$(native_window_permanent_dropin "$n")"
  native_window_barrier_body "$n" permanent | sudo -n python3 -I -B -c '
from pathlib import Path
import os, stat, sys

directory, permanent = Path(sys.argv[1]), Path(sys.argv[2])
expected = sys.stdin.buffer.read()
if not expected:
    sys.exit("empty permanent drop-in body")
uid, gid = os.geteuid(), os.getegid()
s = permanent.lstat()
if not stat.S_ISREG(s.st_mode) or s.st_nlink != 1 or (s.st_uid, s.st_gid) != (uid, gid) or stat.S_IMODE(s.st_mode) != 0o644:
    sys.exit(f"permanent drop-in metadata mismatch: {permanent}")
if permanent.read_bytes() != expected:
    sys.exit(f"permanent drop-in content mismatch: {permanent}")
for entry in sorted(directory.iterdir()):
    if entry != permanent and entry.name.startswith("90-native-window-") and entry.name.endswith(".conf"):
        sys.exit(f"unexpected window drop-in remains: {entry}")
' "$directory" "$permanent_file"
}

native_window_barriers() {
  local action="$1" n file directory link permanent_file object policy force paths drop
  case "$action" in arm|check|rotate|observing|finish|permanent) ;; *) return 2 ;; esac
  shift
  if (( $# == 0 )); then set -- "${window_units[@]}"; fi
  local -a drops
  local -a conditions=("$current_link/native-window" "$native_recovery_dir/accepting")
  if [[ "$action" == arm ]]; then
    sudo -n mkdir -- "$window/held" "$window/observing" "$window/permanent"
    sudo -n ln -sT held "$window/policy"
    native_window_rotation_hook | sudo -n sh -ec 'umask 077; set -C; cat >"$1"' sh "$window/issuer-rotation.sh"
  elif [[ "$action" == finish ]]; then
    for n in "${window_units[@]}"; do
      native_window_install_permanent_dropin "$n"
    done
    sudo -n ln -sT permanent "$window/policy.permanent"
    sudo -n mv -T "$window/policy.permanent" "$window/policy"
    sudo -n systemctl daemon-reload
    for n in "${window_units[@]}"; do
      permanent_file="$(native_window_permanent_dropin "$n")"
      paths="$(native_window_prop "$n" DropInPaths)"
      [[ " $paths " == *" $permanent_file "* ]]
    done
    for n in "${window_units[@]}"; do
      native_window_prune_legacy_dropins "$n"
    done
    sudo -n systemctl daemon-reload
    return
  elif [[ "$action" == rotate ]]; then
    sudo -n ln -sT observing "$window/policy.observing"
    sudo -n mv -T "$window/policy.observing" "$window/policy"
    sudo -n systemctl daemon-reload
    return "$?"
  fi
  if [[ "$action" != permanent ]]; then conditions+=("$window/blocked"); fi
  for n in "$@"; do
    directory="${unit_file%/*}/$n.d"
    file="$window/policy/$n.conf"
    link="$directory/90-native-window-$native_claim_nonce.conf"
    permanent_file="$(native_window_permanent_dropin "$n")"
    if [[ "$action" == arm ]]; then
      native_window_ensure_dropin_dir "$directory"
      native_window_barrier_body "$n" held | sudo -n tee "$window/held/$n.conf" >/dev/null
      native_window_barrier_body "$n" observing | sudo -n tee "$window/observing/$n.conf" >/dev/null
      native_window_barrier_body "$n" permanent | sudo -n tee "$window/permanent/$n.conf" >/dev/null
      sudo -n ln -sT "$file" "$link"
    elif [[ "$action" == check || "$action" == observing || "$action" == permanent ]]; then
      policy="$action"
      if [[ "$action" == check ]]; then policy=held; fi
      [[ "$(sudo -n readlink "$window/policy")" == "$policy" ]]
      if [[ "$action" == permanent ]]; then
        if sudo -n test -e "$file"; then
          native_window_barrier_body "$n" permanent | sudo -n cmp -s - "$file"
        fi
        native_window_verify_permanent_dropin "$n"
        [[ "$(native_window_prop "$n" NeedDaemonReload)" == no ]]
        paths="$(native_window_prop "$n" DropInPaths)"
        [[ " $paths " == *" $permanent_file "* ]]
        read -r -a drops <<< "$paths"
        for drop in "${drops[@]}"; do
          sudo -n test ! -L "$drop"
          native_window_barrier_body "$n" permanent | sudo -n cmp -s - "$drop"
        done
      else
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
      fi
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
  pid="$(native_window_prop "${unit?}" MainPID)"; [[ "$pid" =~ ^[1-9][0-9]*$ ]]
  sudo -n test "/proc/$pid/exe" -ef "$old_target/bin/hololive-collector"
  native_window_process_endpoint "$pid" "${original_port?}"
  sudo -n python3 -I -B "$old_target/bin/worker-profile-pair.py" verify-native "$service" "$old_target" "$unit"
  pid="$(native_window_prop "$po_service" MainPID)"; [[ "$pid" =~ ^[1-9][0-9]*$ ]]
  root="$old_target/po-sandbox/rootfs"
  sudo -n test "/proc/$pid/root" -ef "$root"
  sudo -n test "/proc/$pid/exe" -ef "$root/app/bin/po-broker"
  for n in "${window_units[@]}"; do
    case "$n" in "$po_service") execfile="${po_unit_file?}" ;; "$po_socket") execfile="${po_socket_file?}" ;; *) execfile="$unit_file" ;; esac
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

native_window_live_generation() {
  local n="$1" job state id
  job="$(native_window_prop "$n" Job)" && state="$(native_window_prop "$n" ActiveState)" &&
    id="$(native_window_prop "$n" InvocationID)" && [[ -z "$job" && "$state" == active &&
    "$id" =~ ^[0-9a-f]{32}$ && "$id" != 00000000000000000000000000000000 ]] && printf '%s\n' "$id"
}

native_window_issuer_chain() (
  set -o pipefail
  local boot="$1" job="$2" first="$3" since="$4" current="$5" restarts="$6" lease
  [[ "$since" =~ ^@[0-9]+([.][0-9]+)?$ && "$restarts" == 0 ]] || return 1
  lease="$(native_window_permit rotation)"
  sudo -n journalctl --boot="$boot" --since="$since" --lines=4096 --quiet --no-pager -o json \
    --output-fields=_BOOT_ID,_PID,UNIT,INVOCATION_ID,MESSAGE_ID,JOB_ID,JOB_TYPE,JOB_RESULT,EXIT_CODE,EXIT_STATUS \
    _PID=1 "UNIT=$po_service" | sudo -n python3 -I -B -c '
from pathlib import Path
import json, os, re, stat, sys
boot, unit, first_job, first, current, lease = sys.argv[1:]
start, clean = "39f53479d3a045ac8e11786248231fbf", "7ad2d189f7e94e70a38c781354912448"
jobs, ids = set(), set()
state, last = "initial", None
for line in sys.stdin:
    r = json.loads(line)
    assert r["_BOOT_ID"] == boot and r["_PID"] == "1" and r["UNIT"] == unit
    mid, inv, job = r.get("MESSAGE_ID"), r.get("INVOCATION_ID"), r.get("JOB_ID")
    if mid == start:
        assert r["JOB_TYPE"] == "start" and r["JOB_RESULT"] == "done"
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
assert state == "live"
' "$boot" "$po_service" "$job" "$first" "$current" "$lease"
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
  local n="$1" boot job id receipt current since count
  boot="$(native_window_boot)" && receipt="$(sudo -n cat "$window/$n.started")" || return 1
  [[ "$receipt" =~ ^([0-9a-f]{32})[[:blank:]]([1-9][0-9]*)[[:blank:]]([0-9a-f]{32})$ ]] || return 1
  job="${BASH_REMATCH[2]}" id="${BASH_REMATCH[3]}"
  [[ "${BASH_REMATCH[1]}" == "$boot" ]] && printf '%s\n' "$job" | sudo -n cmp -s - "$window/$n.job" || return 1
  current="$(native_window_live_generation "$n")" || return 1
  if [[ "$n" != "$po_service" ]]; then [[ "$current" == "$id" ]]; return "$?"; fi
  if [[ "$current" != "$id" ]]; then sudo -n test -f "$window/issuer-lease" || return 1; fi
  count="$(native_window_prop "$n" NRestarts)" || return 1
  since="$(sudo -n cat "$window/$n.since")" || return 1
  native_window_issuer_chain "$boot" "$job" "$id" "$since" "$current" "$count" || return 1
  native_window_candidate_runtime "$n" && [[ "$(native_window_live_generation "$n")" == "$current" ]]
}

native_window_probe_units() {
  local n
  for n in "$@"; do
    if [[ "$n" == "$po_service" ]]; then
      native_window_issuer_state >/dev/null || return 1
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

native_window_candidate_runtime() {
  local n="$1" pid exe
  if [[ "$n" == "$po_socket" ]]; then
    sudo -n cmp -s "$po_socket_file" "$release_dir/$po_socket"
    return "$?"
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
      not_ready) sleep 2; continue ;;
    esac
    start="$(native_window_prop "$po_service" ExecMainStartTimestampMonotonic)" || return 1
    [[ "$start" =~ ^[1-9][0-9]*$ ]] || return 1
    python3 -I -B -c 'import sys,time; assert 0 <= time.monotonic_ns()//1000-int(sys.argv[1]) < 90_000_000' "$start" || return 1
    sleep 2
  done
  return 1
}

native_window_issuer_state() {
  local state attempt id pid
  for attempt in {1..6}; do
    state="$(native_window_prop "$po_service" ActiveState)" || return 1
    case "$state" in
      active)
        id="$(native_window_prop "$po_service" InvocationID)" || return 1
        if native_window_owned "$po_service"; then echo live; return 0; fi
        if [[ "$(native_window_prop "$po_service" ActiveState)" == active && "$(native_window_prop "$po_service" InvocationID)" == "$id" ]]; then
          pid="$(native_window_prop "$po_service" MainPID)" || return 1
          [[ "$pid" =~ ^[0-9]+$ ]] || return 1
          if [[ "$pid" != 0 ]] && sudo -n test -e "/proc/$pid/exe"; then return 1; fi
        fi
        ;;
      inactive) ;;
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

native_window_socket_trip() {
  local receipt
  receipt="$(sudo -n cat "$window/$po_socket.started")" || return 1
  [[ "$receipt" =~ ^[0-9a-f]{32}[[:blank:]][1-9][0-9]*[[:blank:]]([0-9a-f]{32})$ ]] || return 1
  local id="${BASH_REMATCH[1]}" record
  record="$(sudo -n journalctl --boot --quiet --no-pager -o json --output-fields=INVOCATION_ID,UNIT_RESULT \
    --since="@$(sudo -n stat -c %Y "$window/$po_socket.started")" \
    _PID=1 "UNIT=$po_socket" "INVOCATION_ID=$id" UNIT_RESULT=trigger-limit-hit MESSAGE_ID=d9b373ed55a64feb8242e02dbe79a49c)" || return 1
  python3 -I -B -c 'import json,sys; r=json.loads(sys.stdin.readline()); assert r["INVOCATION_ID"]==sys.argv[1] and r["UNIT_RESULT"]=="trigger-limit-hit"' "$id" <<< "$record"
}

native_window_issuer_down() {
  local attempt result
  for attempt in {1..6}; do
    result="$(native_window_prop "$po_service" Result)" || return 1
    if [[ -z "$(native_window_prop "$po_service" Job)" && "$(native_window_prop "$po_service" ControlPID)" == 0 &&
          "$(native_window_prop "$po_service" ActiveState)" == inactive && "$(native_window_prop "$po_service" MainPID)" == 0 ]]; then
      [[ "$result" == success || "$result" == exec-condition ]]
      return
    fi
    (( attempt == 6 )) || sleep 1
  done
  return 1
}

native_window_fenced_idle() {
  sudo -n test -d "$native_recovery_dir/accepting" && native_window_issuer_down || return 1
  case "$(native_window_prop "$po_socket" ActiveState)" in
    active) native_window_owned "$po_socket" ;;
    failed) [[ "$(native_window_prop "$po_socket" Result)" == trigger-limit-hit ]] && native_window_socket_trip ;;
    *) return 1 ;;
  esac
}

native_window_fence_probe() {
  native_window_probe_units "${window_units[@]}" || { native_window_fenced_idle && native_window_probe_units "$unit"; }
}

native_window_fenced_issuer_ready() { native_window_issuer_ready || native_window_fenced_idle; }

native_window_reopen_issuer() {
  local deadline=$((SECONDS+90))
  if [[ "$(native_window_prop "$po_socket" ActiveState)" == failed ]]; then
    [[ "$(native_window_prop "$po_socket" Result)" == trigger-limit-hit ]] && native_window_socket_trip || return 1
    sudo -n systemctl reset-failed "$po_socket"
    sudo -n systemctl start "$po_socket"
  fi
  while (( SECONDS < deadline )); do
    if [[ "$(native_window_prop "$po_socket" ActiveState)" == active ]] && po_health_ready "$release_dir" &&
        [[ "$(native_window_prop "$po_service" ActiveState)" == active ]] && native_window_candidate_runtime "$po_service"; then
      return 0
    fi
    sleep 2
  done
  echo "accepted window but the issuer did not reopen: $po_socket" >&2
  return 1
}

native_window_commit_policy() {
  native_window_lease_live || { native_window_uncertain; return 1; }
  local n pid
  native_window_fence_probe
  pid="$(native_window_prop "$unit" MainPID)"; [[ "$pid" =~ ^[1-9][0-9]*$ ]]
  for n in "$po_socket" "$unit"; do native_window_candidate_runtime "$n"; done
  native_window_process_endpoint "$pid" 5433
  native_window_lease_live || { native_window_uncertain; return 1; }
  native_window_barriers observing "$@"
  native_window_fenced_issuer_ready || { native_window_uncertain; return 1; }
  native_window_lease_live || { native_window_uncertain; return 1; }
  native_window_barriers finish "$@"
  native_window_barriers permanent "${window_units[@]}"
  native_window_fence_probe
}

native_apply_before_stop() {
  local n
  [[ "$(native_window_identity apply "${window_old?}" '')" == "${apply_origin_pin?}" && "$old_target" == "$releases_root/$window_old" &&
     "$(sudo -n cat "$native_recovery_dir/claim")" == "$native_claim_nonce" &&
     "$(sudo -n cat "$native_recovery_dir/owner-pid")" == "$BASHPID" && "$(sudo -n cat "$native_recovery_dir/release")" == "$release_dir" ]]
  sudo -n test ! -e "$native_recovery_dir/outcome"
  [[ "$(native_root_endpoint "$host_env")" == '100.100.1.8|5433' ]]
  local release_dir="$old_target"
  for n in "${window_units[@]}"; do native_window_candidate_runtime "$n"; done
  native_window_process_endpoint "$(native_window_prop "$unit" MainPID)" 5433
}

native_apply_observation_record() { printf '%s\n' "$native_release_window" "${release_id?}" "$EXPECTED_REVISION" "$1"; }

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
  [[ "$(hostname -s)" == osaka1 && "$service" == "$slot_service" && "${window_nonce?}" =~ ^[0-9a-f-]{36}$ &&
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
  native_window_reopen_issuer
}

native_window_accept_guard() {
  local step="$1" dest="$window/accepted-guard" expires permit status=0
  native_window_identity stage "$release_id" "$EXPECTED_REVISION"
  native_window_lease_live || { native_window_uncertain; return 1; }
  native_window_fenced_issuer_ready || { native_window_uncertain; return 1; }
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
