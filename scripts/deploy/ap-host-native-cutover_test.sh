#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT
python3 - "$ROOT_DIR" "$test_dir" <<'PY'
from pathlib import Path
import os
import signal
import subprocess
import sys
import time

root, work = map(Path, sys.argv[1:])
helper = root / 'scripts/deploy/lib/ap-host-native-cutover.sh'
if not helper.is_file():
    raise SystemExit('native cutover signal helper is missing')
source = (root / 'scripts/deploy/lib/ap-host-native-remote-apply.sh').read_text()
endpoint_libs = ''.join(f'. "{root / "scripts/deploy/lib" / name}"\n' for name in (
    'compose-env.sh', 'postgres-endpoint.sh', 'ap-host-native-rollback-check.sh',
))
function_start = source.index('stop_native_units_and_require_inactive()')
functions = source[function_start:source.index('\narm_native_cutover_restore\n', function_start)]
log_check = source[source.index('native_post_cutover_log_check()'):source.index('\nnative_post_cutover_log_check\n')]

def await_file(path, process):
    until = time.monotonic() + 5
    while time.monotonic() < until:
        if path.exists():
            return
        if process.poll() is not None:
            raise AssertionError(f'fixture exited before {path.name}: {process.returncode}: {path.parent.joinpath("err").read_text()}')
        time.sleep(.01)
    raise AssertionError(f'fixture did not reach {path.name}')

def run_case(name, action, target=None, signo=signal.SIGTERM, initial=True, mode='normal', prechange_running=True, prechange_enabled=True):
    case = work / name
    case.mkdir()
    (case / 'old/rollback-contract').mkdir(parents=True)
    (case / 'new').mkdir()
    endpoint_env = 'POSTGRES_HOST=100.100.1.8\nPOSTGRES_PORT=6433\n'
    (case / 'new/youtube-collector-host.env').write_text(endpoint_env)
    (case / 'old/rollback-contract/youtube-collector-host.env').write_text(endpoint_env)
    (case / 'bin').mkdir()
    command = case / 'bin/systemctl'
    command.write_text('''#!/usr/bin/env bash
set -euo pipefail
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
printf '%s\\n' "$*" >> "$CASE/calls"
case "$1" in
  is-active) exit 3 ;;
  cat) exit 0 ;;
  show)
    case "$4" in
      LoadState) echo loaded ;;
      ActiveState) echo inactive ;;
      UnitFileState) echo disabled ;;
      *) exit 1 ;;
    esac
    ;;
  restart)
    printf '%s\\n' "$BASHPID" > "$CASE/command.pid"
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
''')
    command.chmod(0o755)
    script = case / 'run.sh'
    script.write_text('''#!/usr/bin/env bash
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
    install|ln) printf 'sudo %s\\n' "$*" >> "$CASE/calls" ;;
    *) "$@" ;;
  esac
}
''' + f'. "{helper}"\n' + endpoint_libs + '''
native_recovery_dir="$CASE/recovery"
release_dir="$CASE/new"
old_target="$CASE/old"
[[ "$INITIAL" != false ]] || old_target=""
host_env="$CASE/host.env" unit_file="$CASE/unit"
current_link="$CASE/current" previous_link="$CASE/previous" releases_root="$CASE"
cp "$release_dir/youtube-collector-host.env" "$host_env"
printf 'new\\n' > "$unit_file"
ln -s "$release_dir" "$current_link"
unit=hololive-youtube-collector@youtube-collector-a.service
collector_prechange_running="${PRECHANGE_RUNNING:-true}" collector_prechange_enabled="${PRECHANGE_ENABLED:-true}" collector_prechange_state=inactive
po_service=hololive-youtube-po.service po_socket=hololive-youtube-po.socket
native_previous_link_restore() { printf 'previous restored\\n' >> "$CASE/calls"; }
native_rollback_validate() { test -d "$1/rollback-contract"; }
po_restore_previous() { printf 'po_restore_previous %s\\n' "$*" >> "$CASE/calls"; }
''' + functions + '\nnative_cutover_claim "$release_dir"\narm_native_cutover_restore\n' + action + '\n')
    env = dict(os.environ, CASE=str(case), INITIAL=str(initial).lower(), MODE=mode, PATH=f'{case / "bin"}:{os.environ["PATH"]}',
               PRECHANGE_RUNNING=str(prechange_running).lower(), PRECHANGE_ENABLED=str(prechange_enabled).lower())
    env['SIGNAL_PHASE'] = ('restore' if target.startswith('restore') else 'restart') if target else ''
    with (case / 'out').open('w') as out, (case / 'err').open('w') as err:
        proc = subprocess.Popen(['bash', str(script)], env=env, stdout=out, stderr=err, start_new_session=True)
        if target:
            await_file(case / ('restoring' if target.startswith('restore') else 'entered'), proc)
            if target.endswith('group') or target == 'worker':
                pidfile = case / 'recovery/worker-pid'
                await_file(pidfile, proc)
                until = time.monotonic() + 5
                while not pidfile.read_text().strip().isdecimal():
                    if time.monotonic() >= until:
                        raise AssertionError('원 worker PID를 발행하지 못함')
                    time.sleep(.01)
            if target.endswith('group'):
                # 변경 워커는 소유자가 만든 별도 작업 프로세스 그룹에서 실행된다.
                worker = int((case / 'recovery/worker-pid').read_text())
                assert os.getpgid(int((case/'command.pid').read_text())) == worker if (case/'command.pid').exists() else True
                os.killpg(worker, signo)
                os.kill(proc.pid, signo)
            elif target == 'child':
                os.kill(int((case / 'command.pid').read_text()), signo)
            elif target == 'worker':
                os.kill(int((case / 'recovery/worker-pid').read_text()), signo)
            else:
                os.kill(proc.pid, signo)
            (case / 'signal-delivered').touch()
        status = proc.wait(timeout=12)
    return case, status

for signo, status in [(signal.SIGHUP,129), (signal.SIGINT,130), (signal.SIGTERM,143)]:
    case, actual = run_case(f'owner-{status}', 'native_cutover_run restart sudo -n systemctl restart "$unit"; touch "$CASE/activated"', 'owner', signo)
    assert actual == status, (case.name, actual, (case/'err').read_text())
    assert (case/'completed').exists() and (case/'restored').exists()
    assert not (case/'activated').exists() and not (case/'recovery').exists()
    assert (case/'calls').read_text().count('enable --now ') == 1

for target, signo, status in [('child',signal.SIGTERM,143), ('child',signal.SIGINT,130), ('child',signal.SIGHUP,129), ('group',signal.SIGINT,130), ('worker',signal.SIGKILL,137)]:
    case, actual = run_case(f'{target}-{status}', 'native_cutover_run restart sudo -n systemctl restart "$unit"; touch "$CASE/activated"', target, signo)
    assert actual == status, (case.name, actual, (case/'err').read_text())
    assert not (case/'restored').exists() and not (case/'activated').exists()
    assert (case/'recovery').exists() and 'outcome_unknown' in (case/'err').read_text()
    assert oct((case/'recovery').stat().st_mode & 0o777) == '0o700'
    blocked = subprocess.run(['bash','-c',f'sudo() {{ shift; "$@"; }}; . "{helper}"; native_recovery_dir="{case}/recovery"; native_recovery_require_clear'],capture_output=True)
    assert blocked.returncode != 0

case, actual = run_case('known-failure', 'native_cutover_run failed bash -c "exit 7"')
assert actual == 7 and (case/'restored').exists() and not (case/'recovery').exists()
for load, state, persistent, previous, status, expected in (
    ('loaded', 'active', 'enabled', 'old', 0, 'true true active'),
    ('loaded', 'failed', 'disabled', 'old', 0, 'false false failed'),
    ('loaded', 'activating', 'enabled', 'old', 0, 'true true activating'),
    ('loaded', 'reloading', 'disabled', 'old', 0, 'true false reloading'),
    ('loaded', 'inactive', 'enabled', 'old', 0, 'false true inactive'),
    ('not-found', '', '', '', 4, 'false false not-found'),
    ('not-found', '', '', 'old', 4, None), ('loaded', 'active', 'enabled', '', 0, None),
    ('loaded', 'deactivating', 'enabled', 'old', 0, None), ('loaded', '', 'enabled', 'old', 0, None),
    ('loaded', 'inactive', 'masked', 'old', 0, None), ('loaded', 'inactive', '', 'old', 0, None),
    ('loaded', 'active', 'enabled', 'old', 1, None),
):
    probe = f'''set -eu
. "{helper}"
systemctl() {{
  case "$4" in
    LoadState) echo '{load}'; return {status} ;;
    ActiveState) echo '{state}' ;;
    UnitFileState) echo '{persistent}' ;;
    *) return 9 ;;
  esac
}}
native_capture_collector_state test.service '{previous}'
printf '%s %s %s\\n' "$collector_prechange_running" "$collector_prechange_enabled" "$collector_prechange_state"
'''
    result = subprocess.run(['bash', '-c', probe], capture_output=True, text=True)
    assert (result.returncode == 0) == (expected is not None), (load, state, persistent, result)
    if expected is not None:
        assert result.stdout.strip() == expected, result

# 점검 창에서 멈춰 있던 collector는 이전 release로 되돌리되 시작하지 않고, 변경 전 활성화 상태만 복원합니다.
for enabled in [True, False]:
    case, actual = run_case(f'stopped-before-change-{enabled}', 'native_cutover_run failed bash -c "exit 7"', prechange_running=False, prechange_enabled=enabled)
    calls = (case/'calls').read_text().splitlines()
    assert actual == 7 and not (case/'recovery').exists(), (actual, (case/'err').read_text())
    assert not (case/'restored').exists() and not any(line.startswith(('enable --now ', 'start ', 'restart ')) for line in calls), calls
    assert (f'enable {"hololive-youtube-collector@youtube-collector-a.service"}' in calls) == enabled, calls
    assert 'previous restored' in calls and 'ActiveState=inactive' in (case/'err').read_text() and 'without starting it' in (case/'err').read_text()
case, actual = run_case('initial-signal', 'native_cutover_run restart sudo -n systemctl restart "$unit"', 'owner', initial=False)
assert actual == 143 and not (case/'restored').exists() and not (case/'recovery').exists()
assert not (case/'current').exists() and not (case/'host.env').exists() and not (case/'unit').exists()
assert 'po_restore_previous ' in (case/'calls').read_text()
case, actual = run_case('restore-owner', 'false', 'restore-owner')
assert actual == 143 and (case/'restored').exists() and not (case/'recovery').exists()
assert (case/'calls').read_text().count('enable --now ') == 1
case, actual = run_case('restore-failure-signal', 'false', 'restore-owner', mode='restore_failure')
assert actual == 143 and (case/'recovery').exists() and 'could not be restored' in (case/'err').read_text()
assert (case/'calls').read_text().count('enable --now ') == 1
case, actual = run_case('restore-prelaunch-signal', 'false', mode='restore_prelaunch')
assert actual == 143 and (case/'recovery').exists() and not (case/'restored').exists(), (actual,(case/'err').read_text())
assert 'could not be restored' in (case/'err').read_text()
case, actual = run_case('restore-release-failure', 'false', 'restore-owner', mode='release_failure')
assert actual == 143 and (case/'recovery').exists() and (case/'restored').exists()
assert 'could not be released' in (case/'err').read_text()
for mode, status in [('write_failure',9), ('read_failure',8)]:
    case, actual = run_case(mode, 'native_cutover_run restart sudo -n systemctl restart "$unit"; touch "$CASE/activated"', mode=mode)
    assert actual == status and (case/'recovery').exists()
    assert not (case/'restored').exists() and not (case/'activated').exists()
    assert 'outcome_unknown' in (case/'err').read_text()
    if (case/'recovery/terminal').exists():
        assert (case/'recovery/terminal').stat().st_mode & 0o777 == 0o600

# 미확정 복구 디렉터리가 있으면 후속 writer를 실행하지 않는다.
for occupied in (False, True):
    guard_case = work / ('guard-occupied' if occupied else 'guard-clear')
    guard_case.mkdir()
    if occupied:
        (guard_case / 'recovery').mkdir()
    prefix = f'sudo() {{ shift; "$@"; }}; . "{helper}"; native_recovery_dir="{guard_case}/recovery"; '
    result = subprocess.run(['bash', '-c', prefix + f'native_recovery_require_clear && touch "{guard_case}/changed"'], capture_output=True)
    assert (result.returncode != 0) == occupied
    assert (guard_case / 'changed').exists() != occupied
for kind, mutate in (
    ('foreign', 'printf other > "$native_recovery_dir/claim"'),
    ('unknown', 'touch "$native_recovery_dir/outcome"'),
    ('unexpected', 'touch "$native_recovery_dir/foreign"'),
    ('child', ':'),
):
    case = work / ('guard-' + kind)
    case.mkdir()
    guard = case / 'recovery'
    prefix = f'set -eu; sudo() {{ shift; "$@"; }}; . "{helper}"; native_recovery_dir="{guard}"; '
    action = '(native_cutover_release_guard)' if kind == 'child' else 'native_cutover_release_guard'
    result = subprocess.run(['bash', '-c', prefix + f'native_cutover_claim first; {mutate}; {action}'], capture_output=True)
    assert result.returncode != 0 and (guard/'claim').exists() and (guard/'release').read_text().strip() == 'first'

gate = source[source.index('native_pool_check_pending=true\n'):source.index('\nsudo -n systemd-analyze verify "$unit_file"\n', source.index('native_pool_check_pending=true\n'))]
case, actual = run_case('failed-database-proof', '''
sudo() {
  shift
  if [[ "$1" == systemd-run && "${!#}" == --check-pooled-database ]]; then
    echo probe >> "$CASE/calls"
    return 7
  fi
  "$@"
}
po_install_release() { touch "$CASE/issuer-started"; }
EXPECTED_REVISION=fixture
''' + gate)
assert actual == 7 and (case/'recovery').exists() and not (case/'issuer-started').exists()
calls = (case/'calls').read_text().splitlines()
assert calls.count('probe') == 1 and not any(line.startswith(('enable ', 'restart ', 'start ', 'po_restore_previous')) for line in calls)
assert all('disable --now ' + unit in calls for unit in ('hololive-youtube-po.socket', 'hololive-youtube-po.service', 'hololive-youtube-collector@youtube-collector-a.service'))
case, actual = run_case('restore-group', 'false', 'restore-group')
assert actual == 143 and (case/'recovery').exists() and 'outcome_unknown' in (case/'err').read_text()
assert (case/'calls').read_text().count('enable --now ') == 1

for code in [0,1,2]:
    env = dict(os.environ, LOG_STATUS=str(code))
    command = log_check + '''
unit=test.service change_started_at=2026-10-03T00:00:00Z
journalctl() { printf 'ordinary startup\\n'; }
grep() { return "$LOG_STATUS"; }
native_post_cutover_log_check
'''
    result = subprocess.run(['bash','-Eeuo','pipefail','-c',command],env=env,capture_output=True)
    assert (result.returncode == 0) == (code == 1), (code,result.returncode)
result = subprocess.run(['bash','-Eeuo','pipefail','-c',log_check + '''
unit=test.service change_started_at=2026-10-03T00:00:00Z
journalctl() { printf 'ordinary startup\\n'; }
grep_count=0
grep() { grep_count=$((grep_count + 1)); if (( grep_count == 1 )); then return 1; else return 2; fi; }
native_post_cutover_log_check
'''],capture_output=True)
assert result.returncode == 2

# PO phase 안의 첫 실패에서 다음 활성화로 넘어가지 않는지 실제 함수로 확인합니다.
po_case = work/'po-phase'
(po_case/'new/po-sandbox').mkdir(parents=True)
revision = 'a'*40
(po_case/'new/po-sandbox/revision').write_text(revision+'\n')
po_script = f'''
set -Eeuo pipefail
sudo() {{ shift; "$@"; }}
. "{helper}"
. "{root}/scripts/deploy/lib/ap-host-native-po.sh"
native_recovery_dir="{po_case}/recovery"
native_cutover_claim "{po_case}/new"
po_validate_release() {{ :; }}
po_verify_units() {{ :; }}
systemctl() {{ touch "{po_case}/activated"; }}
sudo() {{
  shift
  case "$1" in
    sha256sum) return 0 ;;
    install) return 9 ;;
    *) "$@" ;;
  esac
}}
native_cutover_run issuer po_install_release "{po_case}/new" "{revision}"
'''
result = subprocess.run(['bash','-c',po_script],capture_output=True)
assert result.returncode == 9 and not (po_case/'activated').exists()
assert (po_case/'recovery/terminal').read_text().strip() == '9'
po_restore = f'''
set -Eeuo pipefail
. "{root}/scripts/deploy/lib/ap-host-native-po.sh"
sudo() {{ if [[ "$2" == systemctl ]]; then return 143; fi; touch "{po_case}/restore-past-signal"; }}
po_restore_previous ""
'''
result = subprocess.run(['bash','-c',po_restore],capture_output=True)
assert result.returncode == 143 and not (po_case/'restore-past-signal').exists()
case, actual = run_case('stalled-owner', 'native_cutover_run restart sudo -n systemctl restart "$unit"; touch "$CASE/activated"', 'owner', mode='stalled')
assert actual == 143 and (case/'recovery').exists() and not (case/'restored').exists() and not (case/'activated').exists()
os.killpg(int((case/'recovery/worker-pid').read_text()), signal.SIGKILL)
window_lib = root / 'scripts/deploy/lib/ap-host-native-window.sh'
release_lib = root / 'scripts/deploy/lib/ap-host-native-release-path.sh'
release_window, old_port = 'W2', 6433
def window_case(name, args, scenario):
    case = work / name
    case.mkdir(mode=0o700)
    readiness = root / 'scripts/deploy/lib/ap-collector-readiness.sh'
    script = f'. "{helper}"\n. "{release_lib}"\n. "{window_lib}"\n. "{readiness}"\n' + endpoint_libs + r'''
set -Eeuo pipefail
umask 077
cd "$CASE"
releases_root="$PWD/releases" release_id=new window_old=old
old_target="$releases_root/old" current_link="$PWD/current" previous_link="$PWD/previous"
payload="$PWD/payload" host_env="$PWD/host.env" unit_file="$PWD/system/collector@.service"
native_recovery_dir="$PWD/recovery"
service=youtube-collector-a unit=collector.service po_service=issuer.service po_socket=issuer.socket
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
collector_readiness_fetch() { printf '{"instance_id":"youtube-collector-a","status":"ready","helper":"ok","first_success":true,"handoff_status":"PROCESSED","handoff_processed":true,"pending_queue":0}\n'; }
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
      /proc/*/exe) [[ "$3" == -ef && ( "$4" == "$release_dir/bin/youtube-collector" || "$4" == "$release_dir/po-sandbox/rootfs/app/bin/po-broker" ) ]] ;;
      /proc/*/root) [[ "$3" == -ef && "$4" == "$release_dir/po-sandbox/rootfs" ]] ;;
      *) "$@" ;;
    esac ;;
    install) mkdir -p -- "${!#}" ;;
    sync) return 0 ;;
    awk|python3|systemd-run|journalctl|systemctl|bash|sh|mkdir|tee|touch|sha256sum|ln|cmp|cat|grep|readlink|rm|mv) "$@" ;;
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
      [[ "$2" == --now && "$3" == "$po_socket" ]]
      rm "down-$po_socket"; printf '%032d\n' 101 >"live-$po_socket" ;;
    stop) shift; printf '%s\n' "$*" >> calls; for n in "$@"; do touch "down-$n"; rm -f "live-$n"; done ;;
    *) return 92 ;;
  esac
}
native_window_prop() {
  case "$2" in
    InvocationID) if [[ -e "live-$1" ]]; then cat "live-$1"; elif [[ "$fault" == replaced && -e reload ]]; then printf '%032d\n' 2; else echo "$gen"; fi ;;
    Job) if [[ -e job-pending && "$1" == "$unit" ]]; then echo 42; fi ;;
    RestartForceExitStatus|SuccessExitStatus) echo ;;
    NRestarts) echo 0 ;;
    ActiveEnterTimestampMonotonic) python3 -I -B -c 'import time; print(time.monotonic_ns()//1000)' ;;
    ExecMainStartTimestampMonotonic) python3 -I -B -c 'import sys,time; print(time.monotonic_ns()//1000-100000000*int(sys.argv[1]))' "$([[ -f broker-not-ready ]] && echo 1 || echo 0)" ;;
    ExecMainCode) echo 1 ;;
    ExecMainStatus) echo 0 ;;
    ActiveState) if [[ -e "down-$1" ]]; then echo inactive; else echo active; fi ;;
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
    UnitFileState) echo enabled ;;
    Result) echo success ;;
    *) return 93 ;;
  esac
}
busctl() {
  if [[ "$2" == call ]]; then echo 'o "/org/freedesktop/systemd1/unit/fixture"'; return; fi
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
    printf '{"_SYSTEMD_INVOCATION_ID":"%s"}\n' "$gen"
  elif [[ "$*" == *JOB_RESULT=done* && "$*" == *MESSAGE_ID=9d1aaa27d60140bd96365438aad20286* && "$fault" != started_only ]]; then
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
native_window_inputs() { sudo -n sha256sum --strict --check --status "$window/inputs.sha256"; }
native_window_process_endpoint() { [[ "$2" == 6433 ]]; }

seed_accepted() (
  native_release_window="$1"
  mkdir -m 0700 "$native_recovery_dir"
  native_window_identity stage old "$(cat "$old_target/po-sandbox/revision")"
  mv "$native_recovery_dir" "$old_target/seed-guard"
  ln "$old_target/seed-guard/accepted-window" "$old_target/accepted-window"
)
if [[ "$native_release_window" == W2 ]]; then seed_accepted W1; fi

''' + scenario
    result = subprocess.run(['bash', '-c', script, 'test', *args], env=dict(os.environ, CASE=str(case)),
                            capture_output=True, text=True, timeout=15)
    return case, result

for name, win, port, fault, mutation, ok in (
    ('window-valid', release_window, old_port, 'normal', '', True),
    ('window-other', ('W2' if release_window == 'W1' else 'W1'), (6433 if old_port == 5433 else 5433), 'normal', '', False),
    ('window-endpoint', release_window, (6433 if old_port == 5433 else 5433), 'normal', '', False),
    ('window-guard', release_window, old_port, 'missing', '', False),
    ('window-restart', release_window, old_port, 'restart', '', False),
    ('window-generation', release_window, old_port, 'replaced', '', False),
    ('window-journal', release_window, old_port, 'nojournal', '', False),
    ('window-only-started', release_window, old_port, 'started_only', '', False),
    ('window-no-drain', release_window, old_port, 'no_drain', '', False),
    ('window-foreign', release_window, old_port, 'normal', 'window_nonce=$(printf "%036d" 1)', False),
    ('window-incomplete', release_window, old_port, 'normal', 'rm "$window/quiesced"', False),
    ('window-digest', release_window, old_port, 'normal', 'window_digest=$(printf "%064d" 1)', False),
    ('window-stage-drift', release_window, old_port, 'normal', 'touch "$payload/extra"', False),
    ('window-before', release_window, old_port, 'normal', 'echo other > "$window/$unit.before"', False),
    ('window-stopped', release_window, old_port, 'normal', 'cp "$window/$unit.before" "$window/$po_socket.stopped"', False),
    ('window-reuse', release_window, old_port, 'normal', 'native_window_resume_claim', False),
):
    case, result = window_case(name, [win, str(port), fault], 'native_window_quiesce\nwindow_nonce="$native_claim_nonce"\n'
                               + mutation + '\nnative_window_resume_claim\n')
    assert (result.returncode == 0) == ok, (name, result.stderr)
    held = case / 'releases/old/native-window'
    if ok:
        assert (held/'blocked').exists() and (held/'resuming').is_dir() and (held/'quiesced').is_file()
        assert (case/'calls').read_text().splitlines() == ['issuer.socket issuer.service', 'collector.service']
    elif fault != 'normal':
        assert not (held/'quiesced').exists() and not (held/'resuming').exists()
        if fault == 'no_drain':
            assert (case/'calls').read_text().splitlines() == ['issuer.socket issuer.service', 'collector.service']
        elif fault in ('nojournal', 'started_only'):
            assert (case/'calls').read_text().splitlines() == ['issuer.socket issuer.service']
        else:
            assert not (case/'calls').exists()

case, result = window_case('window-brake', [release_window, str(old_port), 'normal'], '''
native_window_quiesce
window_nonce="$native_claim_nonce"
native_window_resume_claim
release_dir="$releases_root/new"; mkdir "$release_dir"; ln -sfn "$release_dir" "$current_link"
rm "$window/blocked"
native_window_rehold
native_rollback_endpoint_validate "$old_target" "$host_env"
''')
assert result.returncode != 0 and '퇴역' in result.stderr and (case/'recovery/claim').is_file()
assert (case/'releases/old/native-window/blocked').is_file() and (case/'current').resolve() == case/'releases/new'
candidate_stage = '''
native_window_quiesce
window_nonce="$native_claim_nonce"
native_window_resume_claim
release_dir="$releases_root/new"; mkdir "$release_dir"; ln -sfn "$release_dir" "$current_link"
mkdir "$release_dir/po-sandbox"; echo "$EXPECTED_REVISION" > "$release_dir/po-sandbox/revision"
po_socket_file="$PWD/system/$po_socket"; touch "$po_socket_file"; cp "$po_socket_file" "$release_dir/$po_socket"
rm "$window/blocked"
for target in "$po_socket" "$po_service"; do native_cutover_run issuer native_window_start "$target"; done
native_window_open_rotation
native_cutover_run collector-start native_window_start "$unit"
native_window_await_observation
'''
adopted_policy = '''
mkdir -p w1-adoption/permanent w1-adoption/original-guard
printf 'outcome_unknown\\n' >w1-adoption/original-guard/outcome
ln -s permanent w1-adoption/policy
for n in "$po_socket" "$po_service" "$unit"; do
  mkdir -p "$PWD/system/$n.d"
  {
    printf '[Unit]\\nConditionPathExists=!%s/native-window\\nConditionPathExists=!%s/accepting\\n' "$current_link" "$native_recovery_dir"
    if [[ "$n" != "$po_socket" ]]; then
      printf '[Service]\\n'
      if [[ "$n" == "$po_service" ]]; then printf 'RootDirectoryStartOnly=yes\\n'; fi
      printf 'ExecCondition=+/usr/bin/test ! -e %s/native-window\\nExecCondition=+/usr/bin/test ! -e %s/accepting\\n' "$current_link" "$native_recovery_dir"
    fi
  } >"w1-adoption/permanent/$n.conf"
  ln -sT "$PWD/w1-adoption/policy/$n.conf" "$PWD/system/$n.d/90-native-window-prior.conf"
done
'''
case, result = window_case('adopted-w1-to-w2', [release_window, str(old_port), 'normal'], adopted_policy + candidate_stage +
                           'window_action=accepted\nnative_window_close_observation\n')
assert result.returncode == 0, result.stderr
assert (case/'current/accepted-window').read_text().splitlines()[0] == 'W2'
assert (case/'w1-adoption/original-guard/outcome').read_text() == 'outcome_unknown\n'
assert (case/'releases/old/accepted-window').samefile(case/'releases/old/seed-guard/accepted-window')

for name, action, mutation, expected in (
    ('accepted', 'accepted', '', 'accepted'),
    ('failed', 'observation-failed', '', 'stopped'),
    ('issuer-rotated', 'accepted', 'retire_issuer rotate; retire_issuer rotate', 'accepted'),
    ('issuer-failed-then-replaced', 'accepted', 'retire_issuer rotate 23', 'unknown'),
    ('issuer-unowned-replacement', 'accepted', 'retire_issuer foreign', 'unknown'),
    ('issuer-canceled-job-replaced', 'accepted', 'retire_issuer canceled', 'unknown'),
    ('issuer-retired-before-brake', 'observation-failed', 'retire_issuer stop', 'stopped'),
    ('expired-issuer-lease', 'accepted', 'rmdir "$(native_window_permit rotation)"', 'unknown'),
    ('expired-during-final-health', 'accepted', 'touch expire-during-health', 'unknown'),
    ('socket-open-issuer-not-ready', 'accepted', 'touch broker-not-ready', 'unknown'),
    ('owned-cold-issuer-warms', 'accepted', 'retire_issuer rotate; touch broker-warming', 'accepted'),
    ('issuer-changed', 'accepted', 'printf "%032d\\n" 9 > "live-$po_service"', 'unknown'),
    ('socket-changed', 'observation-failed', 'printf "%032d\\n" 9 > "live-$po_socket"', 'unknown'),
    ('collector-changed', 'observation-failed', 'printf "%032d\\n" 9 > "live-$unit"', 'unknown'),
    ('reboot', 'accepted', 'native_window_boot() { printf "%032d\\n" 999; }', 'unknown'),
    ('partial', 'observation-failed', 'rm "$window/$po_socket.job"', 'unknown'),
    ('queued', 'observation-failed', 'touch job-pending', 'unknown'),
    ('closing-owned', 'observation-failed', 'mkdir "$window/closing"', 'busy'),
):
    case, result = window_case('observe-' + name, [release_window, str(old_port), 'normal'], candidate_stage + mutation +
                               '\nwindow_action=' + action + '\nnative_window_close_observation\n')
    hold = case/'releases/old/native-window'
    assert (result.returncode == 0) == (expected in ('accepted','stopped')), (name, result.stderr)
    assert (hold/'accepted-guard').is_dir() == (expected == 'accepted'), (name, result.stderr)
    assert (case/'current/accepted-window').is_file() == (expected == 'accepted'), (name, result.stderr)
    assert (case/'recovery').is_dir() != (expected == 'accepted'), (name, result.stderr)
    assert (case/'recovery/outcome').exists() == (expected == 'unknown'), (name, result.stderr)
    assert (hold/'blocked').exists() == (expected in ('unknown','stopped')), (name, result.stderr)
    stops = (case/'calls').read_text().splitlines()[2:]
    expected_stops = ['issuer.socket issuer.service','collector.service'] if expected == 'stopped' else []
    if name == 'issuer-retired-before-brake':
        expected_stops[0] = 'issuer.socket'
    assert stops == expected_stops, (name, stops)

for fault in ('wrong_start_job','foreign_start','missing_anchor'):
    case, result = window_case(fault, [release_window, str(old_port), fault], candidate_stage)
    assert result.returncode != 0 and not (case/'releases/old/native-window/await-observation').exists(), (fault, result.stderr)
    assert not (case/'releases/old/native-window/issuer.socket.started').exists()

for name, mutation, valid in (
    ('same', '', True),
    ('different', 'native_release_window="$other"', False),
    ('unaccepted', 'rm "$old_target/accepted-window"', False),
    ('no-current', 'rm "$current_link"', False),
    ('guard', 'mkdir "$native_recovery_dir"', False),
    ('broken-guard', 'ln -s missing "$native_recovery_dir"', False),
    ('retired', 'mkdir "$old_target/native-window"', False),
    ('copied', 'cp -a "$old_target" "$releases_root/other"; ln -sfn "$releases_root/other" "$current_link"', False),
    ('recreated', 'mv "$old_target" "$releases_root/moved"; cp -a "$releases_root/moved" "$old_target"', False),
    ('copied-file', 'cp -p "$old_target/accepted-window" "$old_target/copy"; mv -T "$old_target/copy" "$old_target/accepted-window"', False),
    ('old-revision', 'echo "$EXPECTED_REVISION" > "$old_target/po-sandbox/revision"', False),
    ('writable', 'chmod 0666 "$old_target/accepted-window"', False),
):
    case, result = window_case('apply-origin-' + name, [release_window, '6433', 'normal'], '''
if [[ -e "$old_target/accepted-window" ]]; then rm -r "$old_target/seed-guard" "$old_target/accepted-window"; fi
seed_accepted "$native_release_window"
other=W1; if [[ "$native_release_window" == W1 ]]; then other=W2; fi
''' + mutation + '\nnative_apply_preflight\n')
    assert (result.returncode == 0) == valid and (not valid or result.stdout.split()[:2] == ['old', f'{1:040d}']), (name, result.stderr)

for origin in ('direct', 'W1', 'W2'):
    case, result = window_case('quiesce-origin-' + origin, [release_window, str(old_port), 'normal'], '''
if [[ -e "$old_target/accepted-window" ]]; then rm -r "$old_target/seed-guard" "$old_target/accepted-window"; fi
''' + ('' if origin == 'direct' else f'seed_accepted {origin}\n') + 'native_window_quiesce\n')
    allowed = (origin == 'direct') == (release_window == 'W1')
    assert (result.returncode == 0) == allowed and (case/'calls').exists() == allowed, (origin, result.stderr)

apply_origin = '''
if [[ -e "$old_target/accepted-window" ]]; then rm -r "$old_target/seed-guard" "$old_target/accepted-window"; fi
seed_accepted "$native_release_window"
apply_origin_pin="$(native_apply_preflight)"; window_old="${apply_origin_pin%% *}"
release_dir="$releases_root/new"
native_cutover_claim "$release_dir"
window_units=("$po_socket" "$po_service" "$unit")
po_socket_file="$PWD/system/$po_socket"; touch "$po_socket_file"; cp "$po_socket_file" "$old_target/$po_socket"
for n in "${window_units[@]}"; do printf '%032d\n' 9 > "live-$n"; done
'''
for name, mutation in (
    ('replaced', 'rm "$old_target/accepted-window"; mv "$old_target/seed-guard" "$old_target/first-guard"; mv "$native_recovery_dir" "$PWD/claim"; seed_accepted "$native_release_window"; mv "$PWD/claim" "$native_recovery_dir"'),
    ('guard', 'echo foreign > "$native_recovery_dir/claim"'),
    ('incomplete', 'touch "$native_recovery_dir/outcome"'),
):
    case, result = window_case('apply-stop-' + name, [release_window,'6433','normal'], apply_origin + mutation +
                               '\nnative_apply_before_stop\ntouch stopped\n')
    assert result.returncode != 0 and not (case/'stopped').exists(), (name, result.stderr)

apply_stage = apply_origin + '''
native_apply_before_stop
mkdir -p "$release_dir/po-sandbox"; printf '%s\n' "$EXPECTED_REVISION" > "$release_dir/po-sandbox/revision"
cp "$po_socket_file" "$release_dir/$po_socket"; ln -sfn "$release_dir" "$current_link"
window="$release_dir/native-observation"; mkdir -m 0700 "$window"; apply_start_since=@1
for n in "${window_units[@]}"; do touch "down-$n"; rm "live-$n"; done
printf 'InvocationID=%s\n' "$gen" >"$window/$po_service.before"
native_window_boot >"$window/boot"
native_apply_start_issuer
rm "down-$unit"; printf '%032d\n' 103 >"live-$unit"
native_cutover_run collector-restart true
native_apply_await_observation
window_nonce="$native_claim_nonce"
'''
for name, mutation, fault, accepted in (
    ('normal', '', 'normal', True),
    ('issuer-rotation', 'retire_issuer rotate', 'normal', True),
    ('issuer-failed', 'retire_issuer rotate 23', 'normal', False),
    ('permit-expired', 'rmdir "$(native_window_permit rotation)"', 'normal', False),
    ('unobserved', 'rm "$window/await-observation"', 'normal', False),
    ('foreign-nonce', 'window_nonce=$(printf "%036d" 7)', 'normal', False),
    ('new-release-old-nonce', 'release_id=other; release_dir="$releases_root/other"; cp -a "$releases_root/new" "$release_dir"; ln -sfn "$release_dir" "$current_link"; window="$release_dir/native-observation"', 'normal', False),
    ('different-generation', 'printf "%032d\\n" 11 > "live-$po_socket"', 'normal', False),
    ('different-family', 'if [[ "$native_release_window" == W1 ]]; then native_release_window=W2; else native_release_window=W1; fi', 'normal', False),
    ('missing-start', 'rm "$window/$unit.started"', 'normal', False),
    ('missing-journal', '', 'nojournal', False),
    ('competing-start', '', 'competing_start', False),
    ('foreign-start', '', 'foreign_start', False),
):
    case, result = window_case('apply-close-' + name, [release_window,'6433',fault], apply_stage + mutation +
                               '\nnative_apply_close_observation\nnative_apply_preflight new\n')
    assert (result.returncode == 0) == accepted, (name, result.stderr)
    assert (case/'current/accepted-window').is_file() == accepted and (case/'recovery').exists() != accepted

print('[PASS] native cutover owner/child/group/restore signals, first install, bounded unknown, supervisor errors, apply guards and grep errors')
PY
