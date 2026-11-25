package apnative

const cutoverSystemctlFake = `#!/usr/bin/env bash
set -euo pipefail
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
printf '%s\n' "$*" >> "$CASE/calls"
case "$1" in
  is-active) exit 3 ;;
  cat) exit 0 ;;
  restart)
    printf '%s\n' "$BASHPID" > "$CASE/command.pid"
    touch "$CASE/entered"
    if [[ "$MODE" == stalled ]]; then
      trap '' HUP INT TERM
      while :; do sleep 1; done
    fi
    if [[ "$SIGNAL_PHASE" == restart ]]; then
      while [[ ! -e "$CASE/signal-delivered" ]]; do sleep .01; done
    else sleep .3; fi
    touch "$CASE/completed"
    ;;
  enable)
    if [[ "$*" == 'enable --now '* ]]; then
      touch "$CASE/restoring"
      if [[ "$SIGNAL_PHASE" == restore ]]; then
        while [[ ! -e "$CASE/signal-delivered" ]]; do sleep .01; done
      else sleep .3; fi
      [[ "$MODE" != restore_failure ]] || exit 9
      touch "$CASE/restored"
    fi
    ;;
esac
`

const cutoverRunPrefix = `#!/usr/bin/env bash
set -Eeuo pipefail
sudo() {
  [[ "$1" != -n ]] || shift
  if [[ "$MODE" == write_failure && "$1" == sh && "$2" == -c && "$3" == *worker-pid* && ! -e "$CASE/write.failed" ]]; then
    touch "$CASE/write.failed"; return 9
  fi
  if [[ "$MODE" == read_failure && "$1" == cat && "$2" == */terminal ]]; then return 8; fi
  if [[ "$MODE" == restore_prelaunch && "$1" == sh && "${5:-}" == restore ]]; then kill -TERM "$cutover_restore_owner_pid"; fi
  if [[ "$MODE" == release_failure && "$1" == sh && "$3" == *'rmdir --'* ]]; then return 9; fi
  case "$1" in
    install|ln) printf 'sudo %s\n' "$*" >> "$CASE/calls" ;;
    *) "$@" ;;
  esac
}
`

const cutoverRunVars = `
native_recovery_dir="$CASE/recovery"
release_dir="$CASE/new"
old_target="$CASE/old"
host_env="$CASE/host.env" unit_file="$CASE/unit"
current_link="$CASE/current" previous_link="$CASE/previous" releases_root="$CASE"
cp "$release_dir/collector-host.env" "$host_env"
printf 'new\n' > "$unit_file"
ln -s "$release_dir" "$current_link"
unit=hololive-collector@collector-a.service
po_service=hololive-youtube-po.service po_socket=hololive-youtube-po.socket
`

const cutoverRunStubs = `
window="$CASE"
native_same_window_recoverable() { [[ "${native_pool_check_pending:-false}" == false ]]; }
native_same_window_restore() { sudo -n systemctl enable --now "$unit"; }
native_cutover_claim "$release_dir"
arm_native_cutover_restore
`

const cutoverHostCheck = `
set -Eeuo pipefail
. "$1"
cd "$2"
swapfile=/swapfile releases_root=/releases host_env=/host.env
unit_file=/collector po_unit_file=/issuer po_socket_file=/socket
sysctl() {
  [[ "$*" == '-n net.core.rmem_max net.core.wmem_max vm.swappiness' ]]
  if [[ "$FAULT" == runtime ]]; then echo 4096; else printf '7500000\n7500000\n10\n'; fi
}
swapon() { [[ "$*" == '--noheadings --raw --show=NAME' ]]; [[ "$FAULT" == inactive ]] || echo /swapfile; }
sudo() {
  [[ "$1" == -n ]]; shift
  case "$1" in
    cmp) command cmp -s - "${4##*/}" ;;
    file) [[ "$*" == 'file -b /swapfile' && "$FAULT" != signature ]] && echo 'Linux swap file' ;;
    python3) shift 4; python3 -I -B verify.py "$@" ;;
    *) printf '%s\n' "$1" >> unexpected-command; return 99 ;;
  esac
}
native_existing_host_check 7500000 2048
`

const cutoverHostVerifyPy = `
import os, stat, sys
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
code = sys.stdin.read()
fault = os.environ['FAULT']
def metadata(path):
    uid, gid, mode, kind = 0, 0, 0o644, stat.S_IFREG
    if path in ('/releases','/var/lib/hololive-bot','/etc/sysctl.d','/etc/hololive-bot','/var/log/hololive-bot','/var/log/hololive-bot/archive'):
        kind, mode = stat.S_IFDIR, 0o755
        if path == '/etc/hololive-bot' or path.startswith('/var/log/'):
            mode = 0o750
        if path.startswith('/var/log/'):
            uid, gid = 99, 77
        if fault == 'directory' and path == '/var/log/hololive-bot/archive':
            mode = 0o775
    elif path == '/host.env':
        mode = 0o640
    elif path == '/swapfile':
        mode = 0o640 if fault == 'swap-mode' else 0o600
    if path == '/collector' and fault == 'unit-owner':
        uid = 99
    if path == '/etc/fstab' and fault == 'fstab-link':
        kind = stat.S_IFLNK
    return SimpleNamespace(st_mode=kind|mode, st_gid=gid, st_uid=uid, st_size=(2048 - int(fault == 'swap-size'))*1048576)
fstab = b'/swapfile none swap sw 0 0\n'
if fault == 'fstab-duplicate': fstab *= 2
if fault == 'fstab-options': fstab = fstab.replace(b'sw ', b'noauto ')
user = SimpleNamespace(pw_uid=99, pw_gid=78 if fault=='user' else 77, pw_dir='/nonexistent', pw_shell='/usr/sbin/nologin')
with patch('pwd.getpwnam', return_value=user), patch('grp.getgrnam', return_value=SimpleNamespace(gr_gid=77)), patch('os.lstat', metadata), patch.object(Path,'read_bytes',return_value=fstab):
    exec(compile(code,'host-baseline','exec'))
`

const cutoverFailedDatabaseProof = `
sudo() {
  shift
  if [[ "$1" == systemd-run && "${!#}" == --check-database-endpoint ]]; then
    echo probe >> "$CASE/calls"
    return 7
  fi
  "$@"
}
EXPECTED_REVISION=fixture
`

const cutoverWindowScript = `
set -Eeuo pipefail
umask 077
cd "$CASE"
releases_root="$PWD/releases" release_id=new window_old=old
old_target="$releases_root/old" current_link="$PWD/current" previous_link="$PWD/previous"
payload="$PWD/payload" host_env="$PWD/host.env" unit_file="$PWD/system/collector@.service"
native_recovery_dir="$PWD/recovery"
service=collector-a unit=collector.service po_service=issuer.service po_socket=issuer.socket
window_name="$1" original="$2" fault="$3"
EXPECTED_REVISION=$(printf 'a%.0s' {1..40})
mkdir -p "$old_target/po-sandbox" "$payload" "$PWD/system"
printf '%040d\n' 1 > "$old_target/po-sandbox/revision"
ln -s "$old_target" "$current_link"
printf 'POSTGRES_HOST=100.100.1.8\nPOSTGRES_PORT=%s\n' "$original" > "$host_env"
window_digest="$(native_payload_digest "$payload")"
gen=$(printf '%032d' 1)
hostname() { echo osaka1; }
native_window_permit() { printf '%s/run/hololive-native-%s-%s\n' "$CASE" "$native_claim_nonce" "$1"; }
po_health_ready() {
  [[ ! -f broker-not-ready && ! -f "down-$po_service" ]] || return 1
  if [[ -f broker-warming ]]; then rm -- broker-warming; return 1; fi
  if [[ -f expire-during-health ]]; then rmdir "$(native_window_permit rotation)"; fi
}
collector_readiness_fetch() { printf '{"instance_id":"collector-a","status":"ready","helper":"ok","first_success":true,"handoff_status":"PROCESSED","handoff_processed":true,"pending_queue":0}\n'; }
rotation_fixture_event() {
  local mid=$1 id=$2
  shift 2
  printf '{"_BOOT_ID":"%s","_PID":"1","UNIT":"%s","INVOCATION_ID":"%s","MESSAGE_ID":"%s"%s}\n' \
    "$(native_window_boot)" "$po_service" "$id" "$mid" "${1:-}" >> issuer.journal
}
rotation_fixture_start() {
  local job=$1 id=$2
  rotation_fixture_event 39f53479d3a045ac8e11786248231fbf "$id" \
    ',"JOB_TYPE":"start","JOB_RESULT":"done","JOB_ID":"'"$job"'"'
}
retire_issuer() {
  local old counter next grant status=${2:-0}
  old=$(cat "live-$po_service")
  rotation_fixture_event 98e322203f7a4ed290d09fe03c09fe15 "$old" ',"EXIT_CODE":"exited","EXIT_STATUS":"'"$status"'"'
  rotation_fixture_event 7ad2d189f7e94e70a38c781354912448 "$old"
  if [[ $1 == stop ]]; then
    touch "down-$po_service"
    return
  fi
  counter=0
  if [[ -f rotations ]]; then counter=$(<rotations); fi
  counter=$((counter+1))
  printf '%s\n' "$counter" > rotations
  printf -v next '%032d' "$((200+counter))"
  if [[ $1 != foreign ]]; then
    grant=$((200+counter))
    if [[ $1 == canceled ]]; then grant=199; fi
    printf '%s %s\n' "$grant" "$old" >"$(native_window_permit rotation)/$next"
  fi
  printf '%s\n' "$next" > "live-$po_service"
  rotation_fixture_start "$((200+counter))" "$next"
}
sudo() {
  [[ "$1" == -n ]]; shift
  case "$1" in
    test) case "$2" in
      /proc/*/exe) [[ "$3" == -ef && ( "$4" == "$release_dir/bin/hololive-collector" || "$4" == "$release_dir/po-sandbox/rootfs/app/bin/po-broker" ) ]] ;;
      /proc/*/root) [[ "$3" == -ef && "$4" == "$release_dir/po-sandbox/rootfs" ]] ;;
      -e) if [[ "$3" == /proc/*/exe ]]; then [[ ! -f issuer-exited ]]; else "$@"; fi ;;
      *) "$@" ;;
    esac ;;
    install) mkdir -p -- "${!#}" ;;
    sync) return 0 ;;
    awk|sed|grep|python3|systemd-run|journalctl|systemctl|bash|sh|mkdir|tee|touch|sha256sum|ln|cmp|cat|readlink|rm|mv) "$@" ;;
    *) return 91 ;;
  esac
}
systemd-run() {
  local target="${!#}" id
  if [[ "$*" == *"--property=RuntimeMaxSec=900s "* ]]; then
    local permit
    permit=$(native_window_permit rotation)
    mkdir -p "$permit"
    printf '%032d\n' 104 >"live-${permit##*/}.service"
    return
  fi
  [[ "$*" == *"--property=RuntimeDirectory=hololive-native-$native_claim_nonce-$target "* && "$*" == *"systemctl --job-mode=fail --show-transaction start $target" ]]
  case "$target" in "$po_socket") id=101 ;; "$po_service") id=102 ;; "$unit") id=103 ;; *) return 8 ;; esac
  rm "down-$target"
  printf '%032d\n' "$id" > "live-$target"
  local name arg
  for arg in "$@"; do case "$arg" in --unit=*) name="${arg#*=}" ;; esac; done
  : >"request-${name:?}"
  if [[ "$fault" != missing_anchor ]]; then printf 'Enqueued anchor job %s %s/start.\n' "$id" "$target" >>"request-$name"; fi
  if [[ "$target" == "$po_service" ]]; then rotation_fixture_start "$id" "$(cat "live-$target")"; fi
}
systemctl() {
  if [[ "$1" == --job-mode=fail ]]; then shift; fi
  case "$1" in
    show) printf 'InvocationID=%s\n' "$gen" ;;
    daemon-reload) touch reload ;;
    enable)
      if [[ "$2" != --now ]]; then rm "disabled-$2"; return; fi
      [[ "$3" == "$po_socket" ]]
      rm "down-$po_socket"; printf '%032d\n' 101 >"live-$po_socket" ;;
    disable) touch "disabled-$2" ;;
    reset-failed) rm "failed-$2" ;;
    start)
      printf '%s\n' "$2" >> starts
      rm "down-$2"
      printf '%032d\n' 104 >"live-$2"
      [[ "$fault" != restore_lost || "$2" != "$po_socket" ]] ;;
    stop)
      shift; printf '%s\n' "$*" >> calls
      for n in "$@"; do touch "down-$n"; done
      [[ "$fault" != stop_client_error || "$1" != "$po_socket" ]] ;;
    *) return 92 ;;
  esac
}
native_window_prop() {
  case "$2" in
    InvocationID) if [[ -e "live-$1" ]]; then cat "live-$1"; elif [[ "$fault" == replaced && -e reload ]]; then printf '%032d\n' 2; else echo "$gen"; fi ;;
    Job)
      if [[ -e job-pending && "$1" == "$unit" ]] || [[ -e "down-$1" && "$fault" == "queued-$1" ]]; then echo 42; fi
      [[ "$fault" != "query-$1" || ! -e "down-$1" ]] ;;
    RestartForceExitStatus|SuccessExitStatus) echo ;;
    NRestarts) echo 0 ;;
    ActiveEnterTimestampMonotonic) python3 -I -B -c 'import time; print(time.monotonic_ns()//1000)' ;;
    ExecMainStartTimestampMonotonic) python3 -I -B -c 'import sys,time; print(time.monotonic_ns()//1000-100000000*int(sys.argv[1]))' "$([[ -f broker-not-ready ]] && echo 1 || echo 0)" ;;
    ExecMainCode) echo 1 ;;
    ExecMainStatus) echo 0 ;;
    ActiveState) if [[ -e "failed-$1" ]]; then echo failed; elif [[ -e "down-$1" ]]; then echo inactive; else echo active; fi ;;
    SubState) if [[ "$1" == "$po_socket" ]]; then echo listening; else echo running; fi ;;
    RootDirectory) printf '%s/po-sandbox/rootfs\n' "$release_dir" ;;
    RootDirectoryStartOnly) if [[ $(readlink "$window/policy") != held ]]; then echo yes; else echo no; fi ;;
    Restart)
      if [[ "$fault" == restart || $(readlink "$window/policy") == permanent ]]; then echo always
      else echo no; fi ;;
    DropInPaths)
      if [[ -d "$PWD/w1-adoption" ]]; then printf '%s ' "$PWD/system/$1.d/90-native-window-prior.conf"; fi
      printf '%s/%s.d/90-native-window-%s.conf\n' "${unit_file%/*}" "$1" "$native_claim_nonce" ;;
    MainPID) if [[ -f "live-$1" && ! -f "down-$1" ]]; then echo "$BASHPID"; else echo 0; fi ;;
    ControlPID) echo 0 ;;
    LoadState) echo loaded ;;
    NeedDaemonReload) echo no ;;
    UnitFileState) if [[ -f "disabled-$1" ]]; then echo disabled; else echo enabled; fi ;;
    Result) if [[ -e "failed-$1" ]]; then echo trigger-limit-hit; else echo success; fi ;;
    *) return 93 ;;
  esac
}
busctl() {
  local object="/org/freedesktop/systemd1/unit/${n//./_2e}"
  if [[ "$2" == call ]]; then
    [[ $# == 8 && "$1" == --system && "$3" == org.freedesktop.systemd1 && "$4" == /org/freedesktop/systemd1 &&
       "$5" == org.freedesktop.systemd1.Manager && "$7" == s && "$8" == "$n" ]] || return 1
    case "$8" in "$po_socket"|"$po_service"|"$unit") ;; *) return 1 ;; esac
    case "$6" in
      GetUnit) [[ ! -e "down-$8" ]] || return 1 ;;
      LoadUnit) [[ "$fault" != load_failure ]] || return 1 ;;
      *) return 1 ;;
    esac
    printf 'o "%s"\n' "$object"
    return
  fi
  [[ "$3" == get-property && "$4" == org.freedesktop.systemd1 && "$5" == "$object" ]] || return 1
  if [[ "${!#}" == ExecConditionEx ]]; then
    python3 -I -B - "$current_link/native-window" "$native_recovery_dir/accepting" "$([[ -d "$PWD/w1-adoption" ]] && echo 2 || echo 1)" <<'PY_CONDITION'
import json,sys
print(json.dumps({"type":"a(sasasttttuii)", "data":[["/usr/bin/test",["/usr/bin/test","!","-e",p],["privileged"],0,0,0,0,0,0,0] for p in sys.argv[1:3]]*int(sys.argv[3])}))
PY_CONDITION
    return
  fi
  if [[ "${!#}" == ExecStopPostEx ]]; then
    local lease
    lease=$(native_window_permit rotation)
    python3 -I -B - "$window" "$lease" "$n" <<'PY_HOOKS'
import json,sys
window,lease,unit=sys.argv[1:]
for action in ("consume","grant"):
    argv=["/usr/bin/bash",window+"/issuer-rotation.sh",action,lease,unit,window+"/blocked"]
    print(json.dumps({"type":"a(sasasttttuii)", "data":[[argv[0],argv,["privileged"],0,0,0,0,0,0,0]]}))
PY_HOOKS
    return
  fi
  local condition=blocked permit
  permit=$(native_window_permit "$n")
  if [[ "$n" == "$po_service" && $(readlink "$window/policy") == observing ]]; then permit=$(native_window_permit rotation)/next; fi
  if [[ "$fault" == missing ]]; then condition=wrong; fi
  printf '{"type":"a(sbbsi)","data":[["ConditionPathExists",false,true,"%s",0],["ConditionPathExists",false,true,"%s",0],["ConditionPathExists",false,true,"%s",0],["ConditionPathExists",false,false,"%s",0]]}\n' \
    "$current_link/native-window" "$native_recovery_dir/accepting" "$window/$condition" "$permit"
}
journalctl() {
  if [[ "$fault" == nojournal ]]; then return 1; fi
  if [[ "${!#}" == _SYSTEMD_UNIT=hololive-native-start-* ]]; then local name="${!#}"; cat "request-${name#*=}"; return; fi
  if [[ "$*" == *--lines=4096* ]]; then cat issuer.journal; return; fi
  if [[ "$*" == *' JOB_TYPE=start '* ]]; then
    local n job id
    for n in "${window_units[@]}"; do [[ " $* " != *" UNIT=$n "* ]] || break; done
    case "$n" in "$po_socket") job=101 ;; "$po_service") job=102 ;; "$unit") job=103 ;; esac
    id="$(cat "live-$n")"
    [[ " $* " == *" --boot=$(native_window_boot) "* ]]
    if [[ " $* " == *' JOB_ID='* ]]; then [[ " $* " == *" JOB_ID=$job "* ]]; fi
    if [[ "$fault" == competing_start ]]; then printf '{"_BOOT_ID":"%s","INVOCATION_ID":"%s","JOB_ID":"98"}\n' "$(native_window_boot)" "$gen"; fi
    if [[ "$fault" == wrong_start_job ]]; then job=777; fi
    if [[ "$fault" == foreign_start ]]; then id="$gen"; fi
    printf '{"_BOOT_ID":"%s","INVOCATION_ID":"%s","JOB_ID":"%s"}\n' "$(native_window_boot)" "$id" "$job"
    return
  fi
  if [[ "$*" == *--grep=* ]]; then
    [[ "$fault" != no_drain ]] || return 0
    local invocation="${!#}"
    printf '{"_SYSTEMD_INVOCATION_ID":"%s"}\n' "${invocation#_SYSTEMD_INVOCATION_ID=}"
  elif [[ "$*" == *MESSAGE_ID=d9b373ed55a64feb8242e02dbe79a49c* ]]; then
    [[ " $* " == *" UNIT=$po_socket "* && -f "tripped-$po_socket" ]] || return 0
    local selected
    for selected in "$@"; do
      if [[ "$selected" == INVOCATION_ID=* ]]; then printf '{"INVOCATION_ID":"%s","UNIT_RESULT":"trigger-limit-hit"}\n' "${selected#*=}"; fi
    done
  elif [[ "$*" == *JOB_RESULT=done* && "$*" == *MESSAGE_ID=9d1aaa27d60140bd96365438aad20286* && "$fault" != started_only ]]; then
    if [[ -n "$fault" && " $* " == *" UNIT=${fault#missing-receipt-} "* ]]; then return 0; fi
    local selected
    for selected in "$@"; do
      if [[ "$selected" == INVOCATION_ID=* ]]; then
        printf '{"INVOCATION_ID":"%s","JOB_ID":"42","JOB_RESULT":"done"}\n' "${selected#*=}"
        return
      fi
    done
  fi
}
native_window_verify_original() { test -L "$current_link"; }
native_snapshot_before_cutover() { mkdir "$old_target/rollback-contract"; cp "$host_env" "$old_target/rollback-contract/SHA256SUMS"; }
native_window_process_endpoint() { [[ "$2" == 5433 || "$2" == "$original_port" ]]; }

seed_accepted() (
  native_release_window="$1"
  mkdir -m 0700 "$native_recovery_dir"
  native_window_identity stage old "$(cat "$old_target/po-sandbox/revision")"
  mv "$native_recovery_dir" "$old_target/seed-guard"
  ln "$old_target/seed-guard/accepted-window" "$old_target/accepted-window"
)
seed_accepted W3

`

const cutoverOrdinaryOrigin = `
rm -r "$old_target/seed-guard" "$old_target/accepted-window"
seed_accepted "$native_release_window"
release_dir="$releases_root/$release_id" window_action=apply-origin
window_units=("$po_socket" "$po_service" "$unit")
payload="$PWD/unused-stage" required_udp_buffer=7500000 swapfile_size_mib=2048
worker_profile="$PWD/desired"; touch "$worker_profile" "$old_target/worker-profile.json"
po_unit_file="$PWD/system/$po_service"; po_socket_file="$PWD/system/$po_socket"
touch "$unit_file" "$po_socket_file"
printf 'STACK_WORKER_PROFILE_FILE=%s/worker-profile.json\n' "$old_target" >> "$host_env"
printf 'RootDirectory=%s/po-sandbox/rootfs\n' "$old_target" > "$po_unit_file"
chmod 0640 "$host_env"; chmod 0644 "$po_unit_file"
for f in "$host_env" "$po_unit_file"; do cp "$f" "$old_target/${f##*/}"; done
pin_digest() { sed "s|$old_target|$release_dir|" "$1" | sha256sum | cut -d' ' -f1; }
set -- stage new a 30005 started 7500000 2048 "$EXPECTED_REVISION" apply-origin \
  "$(pin_digest "$host_env")" "$(native_file_digest "$unit_file")" "$(pin_digest "$po_unit_file")" "$(native_file_digest "$po_socket_file")"
native_existing_host_check() { [[ "$fault" != infra ]]; }
native_deployment_inputs() { [[ "$fault" != inputs ]]; }
native_window_prop() {
  case "$2" in
    DropInPaths|Job) : ;; ActiveState) echo active ;; InvocationID) echo "$gen" ;;
    ControlPID) echo 0 ;; UnitFileState) echo enabled ;; NeedDaemonReload) echo no ;;
    *) return 99 ;;
  esac
}
case "$fault" in
  env) echo OTHER=value >> "$host_env" ;;
  collector) echo drift >> "$unit_file" ;;
  issuer) echo drift >> "$po_unit_file" ;;
  socket) echo drift >> "$po_socket_file" ;;
  profile) echo drift >> "$worker_profile" ;;
  used-stage) mkdir "$payload" ;;
  used-release) mkdir "$release_dir" ;;
  drop-dir) mkdir -m 0777 "$PWD/system/$po_service.d" ;;
  prior-guard) mkdir "$native_recovery_dir"; echo preserved > "$native_recovery_dir/claim" ;;
esac
`

const cutoverApplyOrigin = `
if [[ -e "$old_target/accepted-window" ]]; then rm -r "$old_target/seed-guard" "$old_target/accepted-window"; fi
seed_accepted "$native_release_window"
apply_origin_pin="$(native_apply_preflight)"; window_old="${apply_origin_pin%% *}"
release_dir="$releases_root/new"
native_cutover_claim "$release_dir"
window_units=("$po_socket" "$po_service" "$unit")
po_socket_file="$PWD/system/$po_socket"; touch "$po_socket_file"; cp "$po_socket_file" "$old_target/$po_socket"
for n in "${window_units[@]}"; do printf '%032d
' 9 > "live-$n"; done
`

const cutoverOrdinary = `
window="$release_dir/native-observation"
mkdir -p "$window" "$old_target/rollback-contract"
native_window_boot > "$window/boot"
printf '%s\n' "$apply_origin_pin" "$release_dir" "$window_digest" "$native_claim_nonce" > "$window/pins"
mv "$host_env" "$PWD/collector-host.env"; host_env="$PWD/collector-host.env"
chmod 0640 "$host_env"
printf 'STACK_WORKER_PROFILE_FILE=%s/worker-profile.json\n' "$old_target" >> "$host_env"
cp "$host_env" "$old_target/${host_env##*/}"
cp "$host_env" "$old_target/rollback-contract/collector-host.env"
sed "s|$old_target/worker-profile.json|$release_dir/worker-profile.json|" "$host_env" > "$release_dir/${host_env##*/}"
profile_digest="$(native_file_digest "$release_dir/${host_env##*/}")"
touch "$unit_file"; cp "$unit_file" "$old_target/${unit_file##*/}"
po_unit_file="$PWD/system/$po_service"
printf 'RootDirectory=%s/po-sandbox/rootfs\n' "$old_target" > "$po_unit_file"
chmod 0644 "$po_unit_file"
cp "$po_unit_file" "$old_target/$po_service"
sed "s|$old_target|$release_dir|" "$po_unit_file" > "$release_dir/$po_service"
issuer_digest="$(native_file_digest "$release_dir/$po_service")"
printf 'absent\n' > "$old_target/rollback-contract/previous-before-cutover"
sha256sum "$host_env" > "$old_target/rollback-contract/SHA256SUMS"
sha256sum "$old_target/rollback-contract/SHA256SUMS" > "$window/inputs.sha256"
native_rollback_validate() { test -d "$old_target/rollback-contract"; }
po_wait_ready() { test -f "live-$po_service"; }
arm_native_cutover_restore
native_same_window_stop_phase
`

const cutoverOrdinaryStopped = `
ln -s "$old_target" "$previous_link"
native_release_pin apply profile "$host_env" "$old_target" "$release_dir" "$profile_digest"
native_release_pin apply issuer "$po_unit_file" "$old_target" "$release_dir" "$issuer_digest"
ln -sfn "$release_dir" "$current_link"
`

const cutoverApplyStage = `
native_apply_before_stop
mkdir -p "$release_dir/po-sandbox"; printf '%s
' "$EXPECTED_REVISION" > "$release_dir/po-sandbox/revision"
cp "$po_socket_file" "$release_dir/$po_socket"; ln -sfn "$release_dir" "$current_link"
window="$release_dir/native-observation"; mkdir -m 0700 "$window"; apply_start_since=@1
for n in "${window_units[@]}"; do touch "down-$n"; rm "live-$n"; done
printf 'InvocationID=%s
' "$gen" >"$window/$po_service.before"
native_window_boot >"$window/boot"
native_apply_start_issuer
rm "down-$unit"; printf '%032d
' 103 >"live-$unit"
native_cutover_run collector-restart true
native_apply_await_observation
window_nonce="$native_claim_nonce"
`

const cutoverApplyOriginScenario = `
if [[ -e "$old_target/accepted-window" ]]; then rm -r "$old_target/seed-guard" "$old_target/accepted-window"; fi
seed_accepted "$native_release_window"
`
