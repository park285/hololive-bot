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
function_start = source.index('stop_collector_unit_and_require_inactive()')
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

def run_case(name, action, target=None, signo=signal.SIGTERM, initial=True, mode='normal'):
    case = work / name
    case.mkdir()
    (case / 'old/rollback-contract').mkdir(parents=True)
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
  restart)
    printf '%s\\n' "$BASHPID" > "$CASE/command.pid"
    touch "$CASE/entered"
    if [[ "$MODE" == stalled ]]; then
      trap '' HUP INT TERM
      while :; do sleep 1; done
    fi
    sleep .3
    touch "$CASE/completed"
    ;;
  enable)
    if [[ "$*" == 'enable --now '* ]]; then
      touch "$CASE/restoring"
      sleep .3
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
  if [[ "$MODE" == write_failure && "$1" == sh && "$3" == *worker-pid* && ! -e "$CASE/write.failed" ]]; then
    touch "$CASE/write.failed"; return 9
  fi
  if [[ "$MODE" == read_failure && "$1" == cat && "$2" == */terminal ]]; then return 8; fi
  if [[ "$MODE" == restore_prelaunch && "$1" == sh && "${5:-}" == restore ]]; then kill -TERM "$cutover_restore_owner_pid"; fi
  if [[ "$MODE" == release_failure && "$1" == rm && "$2" == -r ]]; then return 9; fi
  case "$1" in
    install|ln) printf 'sudo %s\\n' "$*" >> "$CASE/calls" ;;
    *) "$@" ;;
  esac
}
''' + f'. "{helper}"\n' + '''
native_recovery_dir="$CASE/recovery"
release_dir="$CASE/new"
old_target="$CASE/old"
[[ "$INITIAL" != false ]] || old_target=""
host_env="$CASE/host.env" unit_file="$CASE/unit"
current_link="$CASE/current" previous_link="$CASE/previous" releases_root="$CASE"
printf 'new\\n' > "$host_env"
printf 'new\\n' > "$unit_file"
ln -s "$release_dir" "$current_link"
unit=hololive-youtube-collector@youtube-collector-a.service
po_service=hololive-youtube-po.service po_socket=hololive-youtube-po.socket
native_previous_link_restore() { printf 'previous restored\\n' >> "$CASE/calls"; }
po_restore_previous() { printf 'po_restore_previous %s\\n' "$*" >> "$CASE/calls"; }
''' + functions + '\nnative_cutover_claim "$release_dir"\narm_native_cutover_restore\n' + action + '\n')
    env = dict(os.environ, CASE=str(case), INITIAL=str(initial).lower(), MODE=mode, PATH=f'{case / "bin"}:{os.environ["PATH"]}')
    with (case / 'out').open('w') as out, (case / 'err').open('w') as err:
        proc = subprocess.Popen(['bash', str(script)], env=env, stdout=out, stderr=err, start_new_session=True)
        if target:
            await_file(case / ('restoring' if target.startswith('restore') else 'entered'), proc)
            if target.endswith('group'):
                # The owner creates a separate job process group for its mutation worker.
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
        status = proc.wait(timeout=10)
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

# 실제 remote apply/rollback 본문이 같은 미해제 guard에서 변경 전에 거절하는지 실행합니다.
guard_case = case
prefix = f'sudo() {{ shift; "$@"; }}; . "{helper}"; native_recovery_dir="{guard_case}/recovery"; '
rollback = (root/'scripts/deploy/ap-host-native-rollback.sh').read_text()
rollback_body = rollback[rollback.index('set -euo pipefail\nservice="$1"\nrollback_started_at='):rollback.index('\nREMOTE\n} | ap_remote_bash')]
result = subprocess.run(['bash','-c',prefix + rollback_body,'fixture','youtube-collector-a','2026-10-03T00:00:00Z'],capture_output=True)
assert result.returncode != 0 and b'recovery guard exists' in result.stderr
po_prefix = 'po_service=issuer.service po_socket=issuer.socket po_unit_file=/nonexistent/po.service po_socket_file=/nonexistent/po.socket; '
result = subprocess.run(['bash','-c',prefix + po_prefix + source,'fixture','payload','release','youtube-collector-a','30005','2026-10-03T00:00:00Z','7500000','2048','a'*40],capture_output=True)
assert result.returncode != 0 and b'recovery guard exists' in result.stderr
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
print('[PASS] native cutover owner/child/group/restore signals, first install, bounded unknown, supervisor errors, apply guards and grep errors')
PY
