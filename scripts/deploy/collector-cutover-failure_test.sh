#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# 실제 배포 함수와 실행 구간을 임시 runtime에 적용해 완료 판정과 복원 부작용을 검증합니다.
python3 - "$root" <<'PY'
import json
import re
import shlex
import subprocess
import sys
import tempfile
from pathlib import Path

root = Path(sys.argv[1])
native = (root / 'scripts/deploy/lib/ap-host-native-remote-apply.sh').read_text()
central = (root / 'scripts/deploy/lib/po-central-remote.sh').read_text()


def function(source, name):
    return re.search(r'^' + name + r'\(\) \{\n.*?^\}', source, re.M | re.S).group()


def run(body, state, case):
    result = subprocess.run(
        ['bash', '-c', body, 'cutover-fixture', str(state), case, str(root)],
        capture_output=True, text=True, timeout=20,
    )
    return result


def require(condition, message, result):
    if not condition:
        raise AssertionError(f'{message}\nexit={result.returncode}\n{result.stdout}\n{result.stderr}')


native_functions = '\n'.join(function(native, name) for name in (
    'stop_collector_unit_and_require_inactive', 'stop_native_units_and_require_inactive',
    'native_restore_recorded_runtime', 'restore_native_after_failed_cutover', 'native_cutover_failed', 'arm_native_cutover_restore',
))
native_journal = native[native.index('native_post_cutover_log_check()'):]
native_setup = r'''
set -Eeuo pipefail
state="$1" fixture_case="$2"
. "$3/scripts/deploy/lib/ap-host-native-release-path.sh"
. "$3/scripts/deploy/lib/ap-host-native-cutover.sh"
native_recovery_dir="$state/recovery"
releases_root="$state/releases" old_target="$state/releases/old"
current_link="$state/current" previous_link="$state/previous"
host_env="$state/host.env" unit_file="$state/unit"
unit=collector po_socket=issuer.socket po_service=issuer.service
collector_prechange_running=true collector_prechange_enabled=true
change_started_at=2026-10-03T00:00:00Z
# 이 fixture는 supervisor 복원 부작용을 검증한다. profile parser는 실제 binary smoke가 소유한다.
native_rollback_validate() { test -d "$1/rollback-contract"; }
sudo() {
  [[ "$1" != -n ]] || shift
  if [[ "$1" == install ]]; then
    shift
    while [[ "$1" == -* ]]; do shift 2; done
    cp "$1" "$2"
  else
    "$@"
  fi
}
systemctl() {
  printf 'systemctl %s\n' "$*" >>"$state/calls"
  case "$1" in
    cat|daemon-reload) return 0 ;;
    is-active) [[ "${*: -1}" == "$unit" && "$(cat "$state/runtime")" != stopped ]] ;;
    disable|stop) printf stopped >"$state/runtime" ;;
    enable) basename "$(readlink "$current_link")" >"$state/runtime" ;;
    *) return 1 ;;
  esac
}
po_restore_previous() {
  printf 'po_restore_previous\n' >>"$state/calls"
  if [[ "$fixture_case" == restore_failure ]]; then return 9; fi
}
journalctl() {
  case "$fixture_case" in
    journal_failure) return 5 ;;
    error_log|restore_failure) printf 'ERR injected post-cutover error\n' ;;
    *) printf 'collector ready\n' ;;
  esac
}
'''
for case in ('success', 'journal_failure', 'error_log', 'grep_failure', 'ordinary_failure', 'substitution_failure', 'restore_failure'):
    with tempfile.TemporaryDirectory(prefix='native-cutover-test-') as raw:
        state = Path(raw)
        for name in ('old', 'new', 'older'):
            (state / 'releases' / name).mkdir(parents=True)
        contract = state / 'releases/old/rollback-contract'
        contract.mkdir()
        for name in ('youtube-collector-host.env', 'hololive-youtube-collector@.service'):
            (contract / name).write_text('old')
        (contract / 'previous-before-cutover').write_text(str(state / 'releases/older') + '\n')
        (state / 'current').symlink_to(state / 'releases/new')
        (state / 'previous').symlink_to(state / 'releases/old')
        for name in ('host.env', 'unit', 'runtime'):
            (state / name).write_text('new')
        action = {'ordinary_failure': 'false', 'substitution_failure': 'ready="$(exit 7)"'}.get(case, native_journal)
        result = run(native_setup + native_functions + '\nnative_cutover_claim "$state/releases/new"\narm_native_cutover_restore\n' + ('grep() { return 2; };\n' if case == 'grep_failure' else '') + action, state, case)
        expected_status = 0 if case == 'success' else 7 if case == 'substitution_failure' else 2 if case == 'grep_failure' else 1
        require(result.returncode == expected_status, f'native {case}: original status', result)
        calls = (state / 'calls').read_text().splitlines() if (state / 'calls').exists() else []
        expected_current = 'new' if case == 'success' else 'old'
        expected_previous = 'old' if case == 'success' else 'older'
        expected_runtime = 'new' if case == 'success' else 'stopped' if case == 'restore_failure' else 'old'
        require((state / 'current').resolve().name == expected_current, f'native {case}: current restored', result)
        require((state / 'previous').resolve().name == expected_previous, f'native {case}: previous restored', result)
        require((state / 'runtime').read_text().strip() == expected_runtime, f'native {case}: runtime restored', result)
        require(calls.count('po_restore_previous') == (0 if case == 'success' else 1), f'native {case}: one restore', result)
        require(('could not be restored' in result.stderr) == (case == 'restore_failure'), f'native {case}: restoration warning', result)
        if case != 'success':
            require((state / 'host.env').read_text() == 'old' and (state / 'unit').read_text() == 'old', f'native {case}: installed contract restored', result)
        print(f'[PASS] native {case}: exit={result.returncode}, current={expected_current}, previous={expected_previous}, runtime={expected_runtime}')


central_functions = '\n'.join(
    match.group() for match in re.finditer(r'^([a-z_]+)\(\) \{\n.*?^\}', central, re.M | re.S)
    if match.group(1) not in ('compose', 'require_collector_lease_migration', 'restore_after_failed_cutover')
)
central_config = central[central.index('manifest=../'):central.index('[[ "$(stat')]
central_main = central[central.index('issuer_container_service=youtube-po-c'):]
files = shlex.split(re.search(r'^files=\((.*?)\)', central, re.M | re.S).group(1))
old_revision, new_revision = 'a' * 40, 'b' * 40
old_id, new_id = 'sha256:' + 'a' * 64, 'sha256:' + 'b' * 64
central_setup = r'''
set -Eeuo pipefail
fixture_root="$1" fixture_case="$2"
. "$3/scripts/deploy/lib/po-sandbox-image.sh"
current="$fixture_root/current" staging="$fixture_root/staging" backup="$fixture_root/po-c-20261003T000000Z"
revision=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb version=6.0.1
pair_tool="$staging/scripts/deploy/lib/worker-profile-pair.py"
cutover_started=false
python3() {
  if [[ "${1:-}" == "$pair_tool" ]]; then
    case "$2" in
      capture|candidate) mkdir -p "$4"; printf 'retained profile\n' > "$4/profile.json" ;;
      rollback) cat >/dev/null; test -f "$4/profile.json"; touch "$4/compose.yml" ;;
      verify|verify-source|verify-live)
        if [[ "$2" == verify-source && "$fixture_case" == prepare_* ]]; then return 7; fi
        test -f "$4/profile.json" ;;
      activate) [[ "$fixture_case" != activation_failure ]] || return 7; test -f "$4/profile.json" ;;
      *) return 2 ;;
    esac
  else
    command python3 "$@"
  fi
}
mode=deploy
[[ "$fixture_case" != check_* ]] || mode=check
[[ "$fixture_case" != rollback* ]] || mode=rollback
sudo() { [[ "$1" != -n ]] || shift; "$@"; }
install() {
  local args=()
  while (($#)); do
    case "$1" in -o|-g) shift 2 ;; *) args+=("$1"); shift ;; esac
  done
  command install "${args[@]}"
}
stat() { printf '65532:1000 770\n'; }
date() {
  if [[ "$*" == '-u +%Y-%m-%dT%H:%M:%SZ' ]]; then printf '2026-10-03T00:01:00Z\n'; else command date "$@"; fi
}
sleep() { :; }
require_collector_lease_migration() { :; }
docker() {
  local object kind value format
  printf 'docker %s\n' "$*" >>"$fixture_root/calls"
  case "$1 $2" in
    'image inspect')
      object="${*: -1}" kind=collector
      [[ "$object" != *po-sandbox* ]] || kind=issuer
      if [[ "$object" == *:rollback-* ]]; then value=old; else value="$(cat "$fixture_root/$kind.image")"; fi
      [[ "$value" != absent ]] || return 1
      format="${4:-}"
      case "$format" in
        '{{.Id}}') if [[ "$value" == old ]]; then printf 'sha256:%064d\n' 0 | tr 0 a; else printf 'sha256:%064d\n' 0 | tr 0 b; fi ;;
        *image.revision*) if [[ "$value" == old ]]; then printf '%040d\n' 0 | tr 0 a; else printf '%040d\n' 0 | tr 0 b; fi ;;
        *image.version*) if [[ "$value" == old ]]; then printf '6.0.0\n'; else printf '6.0.1\n'; fi ;;
        '{{.Os}}/{{.Architecture}}') printf 'linux/arm64\n' ;;
      esac
      ;;
    'inspect -f')
      object="${*: -1}" kind=collector
      [[ "$object" != "$issuer_container" ]] || kind=issuer
      value="$(cat "$fixture_root/$kind.runtime")" format="$3"
      case "$format" in
        *State.Health*)
          if [[ "$fixture_case" == *substitution_failure && "$kind" == issuer && "$value" == new ]]; then return 5; fi
          [[ "$(cat "$fixture_root/$kind.running")" == true ]] || return 1
          printf 'healthy\n' ;;
        *image.revision*) if [[ "$value" == old ]]; then printf '%040d\n' 0 | tr 0 a; else printf '%040d\n' 0 | tr 0 b; fi ;;
        '{{.Image}}') if [[ "$value" == old ]]; then printf 'sha256:%064d\n' 0 | tr 0 a; else printf 'sha256:%064d\n' 0 | tr 0 b; fi ;;
        *Mounts*) printf '%s/socket\n' "$fixture_root" ;;
        '{{.State.StartedAt}}') printf '2026-10-03T00:01:01Z\n' ;;
        '{{.State.Running}}') cat "$fixture_root/$kind.running" ;;
        *) return 2 ;;
      esac
      ;;
    'load --input')
      [[ "$fixture_case" != prepare_absent_before_load ]] || return 7
      if [[ "$fixture_case" == prepare_absent_collector_load_failure && "$3" == */collector-image.tar ]]; then return 7; fi
      kind=issuer
      [[ "$3" != */collector-image.tar ]] || kind=collector
      printf new >"$fixture_root/$kind.image"
      ;;
    'tag '*)
      if [[ "$2" == *:rollback-* ]]; then
        [[ "$fixture_case" != restore_failure ]] || return 9
        kind=collector
        [[ "$3" != "$issuer" ]] || kind=issuer
        printf old >"$fixture_root/$kind.image"
      fi
      ;;
    'stop '*|'rm '*) printf stopped >"$fixture_root/issuer.runtime"; touch "$fixture_root/issuer-interrupted" ;;
    'image ls')
      [[ "$fixture_case" != prepare_absent_inventory_failure ]] || return 9
      kind=collector
      [[ "$*" != *po-sandbox* ]] || kind=issuer
      [[ "$(cat "$fixture_root/$kind.image")" == absent ]] || printf 'candidate-image\n'
      ;;
    'image rm')
      [[ "$fixture_case" != prepare_absent_cleanup_failure ]] || return 9
      printf absent >"$fixture_root/issuer.image" ;;
    'compose --env-file') return 0 ;;
    'exec '*) : ;;
    'logs --since')
      case "$fixture_case:${*: -1}" in
        *logs_issuer*:"$issuer_container"|*logs_collector*:"$collector_container"|restore_failure:"$collector_container") return 5 ;;
        *error_log*:"$collector_container") printf 'ERR injected runtime error\n' ;;
        *) printf 'runtime ready\n' ;;
      esac
      ;;
    *) return 2 ;;
  esac
}
compose() {
  local kind
  printf 'compose %s\n' "$*" >>"$fixture_root/calls"
  [[ "$1" == up ]] || return 0
  kind=collector
  [[ "${*: -1}" != "$issuer_container_service" ]] || kind=issuer
  cp "$fixture_root/$kind.image" "$fixture_root/$kind.runtime"
  if [[ " $* " == *" --no-start "* ]]; then
    printf false >"$fixture_root/$kind.running"
  else
    printf true >"$fixture_root/$kind.running"
  fi
  if [[ "$kind" == collector && "$(cat "$fixture_root/collector.image")" == new && "$fixture_case" == signal_* ]]; then
    kill -s "${fixture_case#signal_}" "$cutover_restore_owner_pid"
  fi
}
if [[ "$fixture_case" == *grep_failure ]]; then grep() { return 2; }; fi
'''


def manifest(revision, identity):
    return json.dumps({'schema_version': 1, 'source_revision': revision, 'go': {'goarch': 'arm64'}, 'architecture': 'arm64', 'image_id': identity})


central_cases = (
    'success', 'stopped_substitution_failure', 'substitution_failure', 'deploy_logs_issuer', 'deploy_logs_collector',
    'prepare_present', 'prepare_absent_before_load', 'prepare_absent',
    'prepare_absent_collector_load_failure', 'prepare_absent_stopped',
    'prepare_absent_cleanup_failure', 'prepare_absent_inventory_failure',
    'deploy_error_log', 'deploy_grep_failure', 'restore_failure',
    'signal_INT', 'signal_TERM', 'signal_HUP', 'rollback', 'rollback_stopped', 'activation_failure', 'missing_receipt',
    'check_success', 'check_logs_issuer', 'check_logs_collector', 'check_error_log', 'check_grep_failure',
)
for case in central_cases:
    with tempfile.TemporaryDirectory(prefix='central-cutover-test-') as raw:
        state = Path(raw)
        current, staging, backup = state / 'current', state / 'staging', state / 'po-c-20261003T000000Z'
        current.mkdir()
        staging.mkdir()
        is_check = case.startswith('check_')
        is_rollback = case.startswith('rollback')
        is_preparation = case.startswith('prepare_')
        issuer_absent = is_preparation and 'absent' in case
        initial = 'new' if is_check or is_rollback else 'old'
        for kind in ('collector', 'issuer'):
            (state / f'{kind}.image').write_text(initial)
            (state / f'{kind}.runtime').write_text(initial)
            (state / f'{kind}.running').write_text('true')
        if issuer_absent:
            (state / 'issuer.image').write_text('absent')
            (state / 'issuer.runtime').write_text('absent')
            (state / 'issuer.running').write_text('false')
        if case in ('stopped_substitution_failure', 'prepare_absent_stopped'):
            (state / 'collector.running').write_text('false')
        for rel in files:
            for path, text in ((current / rel, initial), (staging / rel, 'new')):
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(text)
        for prefix, issuer in (('', True), ('collector-', False)):
            tar = staging / (prefix + 'image.tar')
            tar.write_text('reviewed fixture image')
            digest = subprocess.check_output(['sha256sum', tar], text=True).split()[0]
            (staging / (prefix + 'image.tar.sha256')).write_text(f'{digest}  {tar.name}\n')
            (staging / (prefix + 'image-id')).write_text(new_id)
            (staging / ('rootfs-manifest.json' if issuer else 'collector-manifest.json')).write_text(manifest(new_revision, new_id))
        live_po = state / 'po-current-c'
        live_po.mkdir()
        if case != 'missing_receipt' and not issuer_absent:
            (live_po / 'rootfs-manifest.json').write_text(manifest(new_revision if initial == 'new' else old_revision, new_id if initial == 'new' else old_id))
            (live_po / 'image-id').write_text(new_id if initial == 'new' else old_id)
        if is_check or is_rollback:
            backup.mkdir()
            (backup / 'snapshot-complete').touch()
            (backup / 'worker-profile-previous').mkdir()
            (backup / 'worker-profile-previous/profile.json').write_text('retained profile')
            (backup / 'issuer.state').write_text('present')
            (backup / 'collector-prechange.state').write_text('stopped' if case == 'rollback_stopped' else 'running')
            (backup / 'collector.revision').write_text(old_revision)
            (backup / 'collector-prechange-manifest.json').write_text(manifest(old_revision, old_id))
            (backup / 'collector-prechange.image-id').write_text(old_id)
            (backup / 'po-prechange-manifest.json').write_text(manifest(old_revision, old_id))
            (backup / 'po-prechange.image-id').write_text(old_id)
            (backup / 'change-started-at').write_text('2026-10-03T00:00:00Z')
            for rel in files:
                path = backup / 'previous-files' / rel
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('old')
        result = run(central_setup + central_config + central_functions + '\n' + central_main, state, case)
        expected_status = 0 if case in ('success', 'rollback', 'rollback_stopped', 'check_success') else 7 if is_preparation else {'signal_INT': 130, 'signal_TERM': 143, 'signal_HUP': 129, 'missing_receipt': 1, 'activation_failure': 7}.get(case, 2 if case.endswith('grep_failure') else 1 if 'error_log' in case else 5)
        require(result.returncode == expected_status, f'central {case}: original status', result)
        calls = (state / 'calls').read_text().splitlines()
        restore_count = 0 if is_check or case in ('success', 'missing_receipt') else 1
        if case == 'missing_receipt':
            # 영수증이 없으면 원격 상태를 바꾸기 전에 원인을 남기고 멈춘다.
            require('central issuer receipt missing' in result.stderr, f'central {case}: cause reported', result)
            require(not backup.exists(), f'central {case}: no partial backup', result)
            require(all(not call.startswith(('docker tag ', 'docker load ', 'compose up ')) for call in calls), f'central {case}: no rollback tag or cutover', result)
        expected_stops = 0 if is_preparation or case == 'activation_failure' else restore_count
        require(calls.count('docker stop hololive-youtube-po-c') == expected_stops, f'central {case}: bounded restoration', result)
        expected_warning = case in ('restore_failure', 'activation_failure', 'prepare_absent_cleanup_failure', 'prepare_absent_inventory_failure')
        require(('automatic restoration failed' in result.stderr) == expected_warning, f'central {case}: restoration warning', result)
        expected_runtime = 'new' if is_check or case == 'success' or case == 'restore_failure' else 'old'
        require((state / 'collector.runtime').read_text() == expected_runtime, f'central {case}: collector runtime', result)
        expected_running = case not in ('stopped_substitution_failure', 'rollback_stopped', 'prepare_absent_stopped')
        require((state / 'collector.running').read_text() == str(expected_running).lower(), f'central {case}: collector running state', result)
        expected_issuer = 'absent' if issuer_absent else 'stopped' if case == 'restore_failure' else expected_runtime
        require((state / 'issuer.runtime').read_text() == expected_issuer, f'central {case}: issuer runtime', result)
        if is_check:
            require(all(not call.startswith(('compose up ', 'docker tag ', 'docker stop ', 'docker rm ', 'docker load ')) for call in calls), f'central {case}: check has no mutations', result)
            require(all((current / rel).read_text() == 'new' for rel in files), f'central {case}: check preserves installed files', result)
        if restore_count and not is_preparation and case not in ('restore_failure', 'activation_failure'):
            require(all((current / rel).read_text() == ('new' if rel == 'scripts/deploy/compose.sh' else 'old') for rel in files), f'central {case}: previous files and paired boot wrapper restored', result)
            require(json.loads((live_po / 'rootfs-manifest.json').read_text())['source_revision'] == old_revision, f'central {case}: previous issuer manifest restored', result)
        if case == 'restore_failure':
            require(calls.count('compose up -d --no-build --no-deps --force-recreate youtube-collector') == 1, f'central {case}: failed restore stops before recreating collector', result)
        if case == 'activation_failure':
            require(not (state / 'issuer-interrupted').exists(), f'central {case}: failed preparation never interrupts issuer', result)
            require(all((current / rel).read_text() == 'new' for rel in files), f'central {case}: failed restoration leaves candidate files for explicit recovery', result)
        if is_preparation:
            cleanup_failed = case.endswith(('cleanup_failure', 'inventory_failure'))
            expected_issuer_image = 'new' if cleanup_failed else 'absent' if issuer_absent else 'old'
            require((state / 'issuer.image').read_text() == expected_issuer_image, f'central {case}: issuer tag baseline', result)
            require((state / 'collector.image').read_text() == 'old', f'central {case}: collector tag baseline', result)
            require(not (state / 'issuer-interrupted').exists(), f'central {case}: no runtime interruption', result)
            require(all((current / rel).read_text() == ('new' if cleanup_failed else 'old') for rel in files), f'central {case}: source recovery state', result)
            if issuer_absent:
                require(not (live_po / 'rootfs-manifest.json').exists() and not (live_po / 'image-id').exists(), f'central {case}: absent receipt baseline', result)
        print(f'[PASS] central {case}: exit={result.returncode}, runtime={(state / "collector.runtime").read_text()}, running={(state / "collector.running").read_text()}')
PY
