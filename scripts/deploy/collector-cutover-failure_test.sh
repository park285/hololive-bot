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
    # 운영 배포처럼 원격 본문을 bash -s의 stdin으로 보내 자식 명령의 stdin 소비까지 재현한다.
    result = subprocess.run(
        ['bash', '-s', '--', str(state), case, str(root)],
        input=body, capture_output=True, text=True, timeout=20,
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
        if case != 'success':
            require((state / 'host.env').read_text() == 'old' and (state / 'unit').read_text() == 'old', f'native {case}: installed contract restored', result)
        print(f'[PASS] native {case}: exit={result.returncode}, current={expected_current}, previous={expected_previous}, runtime={expected_runtime}')


# 중앙(c)과 iris-seoul(b)은 rootful Podman Quadlet이다. 실제 원격 본문을 가짜 podman·systemctl·pair tool로 실행해
# issuer 우선 교체, 이전 drop-in 복원, 후보 검사 실패의 무변경을 검증한다. b는 slot별 이름과 원장 증명 생략만 본다.
central_functions = '\n'.join(
    match.group() for match in re.finditer(r'^([a-z_]+)\(\) \{\n.*?^\}', central, re.M | re.S)
    if match.group(1) not in ('require_collector_lease_migration', 'restore_after_failed_cutover')
)
central_config = central[central.index('manifest="../'):central.index('[[ "$(stat')]
central_main = central[central.index('# 이 본문은 ssh bash -s'):]
old_revision, new_revision = 'a' * 40, 'b' * 40
old_id, new_id = 'sha256:' + 'a' * 64, 'sha256:' + 'b' * 64
change = '20261003T000000Z'
central_setup = r'''
set -Eeuo pipefail
fixture_root="$1" fixture_case="$2"
. "$3/scripts/deploy/lib/po-sandbox-image.sh"
current="$fixture_root/current" staging="$fixture_root/staging" backup="$fixture_root/po-$slot-20261003T000000Z"
unit_dir="$fixture_root/units"
revision=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb version=6.0.1
pair_tool="$staging/scripts/deploy/lib/worker-profile-pair.py"
cutover_started=false
mode=deploy
[[ "$fixture_case" != check_* ]] || mode=check
[[ "$fixture_case" != rollback* ]] || mode=rollback
ref_file() { printf '%s/images/%s\n' "$fixture_root" "${1//[\/:]/_}"; }
hex_of() { if [[ "$1" == old ]]; then printf '%064d\n' 0 | tr 0 a; else printf '%064d\n' 0 | tr 0 b; fi; }
revision_of() { if [[ "$1" == old ]]; then printf '%040d\n' 0 | tr 0 a; else printf '%040d\n' 0 | tr 0 b; fi; }
python3() {
  if [[ "${1:-}" == "$pair_tool" ]]; then
    printf 'pair %s %s\n' "$2" "$4" >>"$fixture_root/calls"
    case "$2" in
      capture) mkdir -p "$4"; printf 'retained profile\n' > "$4/profile.json" ;;
      candidate)
        cat >/dev/null
        # 후보 pair는 방금 적재해 검증한 :candidate image identity를 고정해야 한다.
        [[ "$5" == "sha256:$(hex_of new)" ]] || return 3
        mkdir -p "$4"; printf 'candidate profile\n' > "$4/profile.json" ;;
      rollback) test -f "$4/profile.json"; touch "$4/50-worker-profile.conf" ;;
      check)
        if [[ "$fixture_case" == *candidate_check_failure && "$4" == */candidate ]]; then return 7; fi
        test -f "$4/profile.json" ;;
      verify|verify-source|verify-live) test -f "$4/profile.json" ;;
      activate)
        [[ "$fixture_case" != activation_failure ]] || return 7
        test -f "$4/profile.json"
        basename "$4" > "$fixture_root/dropin" ;;
      *) return 2 ;;
    esac
  else
    command python3 "$@"
  fi
}
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
require_collector_lease_migration() { printf 'lease-proof\n' >>"$fixture_root/calls"; }
podman() {
  local object kind value format file
  printf 'podman %s\n' "$*" >>"$fixture_root/calls"
  case "$1 $2" in
    'image inspect')
      file="$(ref_file "${*: -1}")" format="$4"
      [[ -f "$file" ]] || return 125
      value="$(cat "$file")"
      case "$format" in
        '{{.Id}}') hex_of "$value" ;;
        *image.revision*) revision_of "$value" ;;
        *image.version*) if [[ "$value" == old ]]; then printf '6.0.0\n'; else printf '6.0.1\n'; fi ;;
        '{{.Os}}/{{.Architecture}}') printf 'linux/arm64\n' ;;
        *) return 2 ;;
      esac
      ;;
    'image exists')
      [[ "$fixture_case" != inventory_failure ]] || return 125
      [[ -f "$(ref_file "$3")" ]]
      ;;
    'image rm') rm "$(ref_file "$3")" ;;
    'tag '*)
      if [[ "$fixture_case" == restore_failure && "$2" == *:rollback-* ]]; then return 9; fi
      cp "$(ref_file "$2")" "$(ref_file "$3")"
      ;;
    'load --input')
      kind=po-sandbox
      [[ "$3" != */collector-image.tar ]] || kind=collector
      printf new > "$(ref_file "localhost/hololive-youtube-$kind:candidate")"
      ;;
    'inspect -f')
      object="${*: -1}" kind=collector format="$3"
      [[ "$object" != "$issuer_container" ]] || kind=issuer
      # Quadlet은 정지한 unit의 container를 지운다.
      [[ "$(cat "$fixture_root/$kind.running")" == true ]] || return 125
      value="$(cat "$fixture_root/$kind.runtime")"
      case "$format" in
        *State.Health*) printf 'healthy\n' ;;
        *image.revision*) revision_of "$value" ;;
        '{{.Image}}') hex_of "$value" ;;
        *Mounts*) printf '%s/socket\n' "$fixture_root" ;;
        '{{.State.StartedAt.Unix}}') command date -u -d 2026-10-03T00:01:01Z +%s ;;
        *) return 2 ;;
      esac
      ;;
    'exec '*) : ;;
    'logs --since')
      case "$fixture_case:${*: -1}" in
        *logs_collector*:"$collector_container") return 5 ;;
        *error_log*:"$collector_container") printf 'ERR injected runtime error\n' ;;
        *) printf 'runtime ready\n' ;;
      esac
      ;;
    *) return 2 ;;
  esac
}
systemctl() {
  local kind=collector value
  printf 'systemctl %s\n' "$*" >>"$fixture_root/calls"
  [[ "${*: -1}" != "$issuer_unit" ]] || kind=issuer
  case "$1" in
    daemon-reload) ;;
    is-active) [[ "$(cat "$fixture_root/$kind.running")" == true ]] ;;
    restart)
      # 재시작은 unit의 Image=를 다시 읽는다: collector는 drop-in이 고정한 pair, issuer는 :prod tag다.
      if [[ "$kind" == collector ]]; then
        case "$(cat "$fixture_root/dropin")" in
          candidate) value=new ;;
          previous) value=old ;;
          *) value="$(cat "$(ref_file "$collector")")" ;;
        esac
      else
        value="$(cat "$(ref_file "$issuer")")"
      fi
      printf '%s' "$value" > "$fixture_root/$kind.runtime"
      if [[ "$fixture_case" == *substitution_failure || "$fixture_case" == restore_failure ]] && [[ "$kind" == issuer && "$value" == new ]]; then
        printf false > "$fixture_root/$kind.running"
        return 1
      fi
      printf true > "$fixture_root/$kind.running"
      if [[ "$fixture_case" == signal_* && "$kind" == collector && "$value" == new ]]; then
        kill -s "${fixture_case#signal_}" "$cutover_restore_owner_pid"
      fi
      ;;
    stop) printf absent > "$fixture_root/$kind.runtime"; printf false > "$fixture_root/$kind.running" ;;
    *) return 2 ;;
  esac
}
if [[ "$fixture_case" == *grep_failure ]]; then grep() { return 2; }; fi
'''


def manifest(revision, identity):
    return json.dumps({'schema_version': 1, 'source_revision': revision, 'go': {'goarch': 'arm64'}, 'architecture': 'arm64', 'image_id': identity})


def image_file(state, ref):
    return state / 'images' / ref.replace('/', '_').replace(':', '_')


collector_prod, issuer_prod = 'localhost/hololive-youtube-collector:prod', 'localhost/hololive-youtube-po-sandbox:prod'
collector_rollback = f'localhost/hololive-youtube-collector:rollback-{change}'
issuer_rollback = f'localhost/hololive-youtube-po-sandbox:rollback-{change}'
mutations = ('podman tag ', 'podman load ', 'podman image rm ', 'systemctl restart ', 'systemctl stop ', 'systemctl daemon-reload', 'pair activate ')
central_cases = {
    # case: (exit status, restored to previous pair)
    'success': (0, False), 'absent_success': (0, False),
    'substitution_failure': (1, True), 'deploy_logs_collector': (5, True), 'deploy_error_log': (1, True),
    'deploy_grep_failure': (2, True), 'signal_TERM': (143, True), 'signal_HUP': (129, True),
    'restore_failure': (1, False), 'activation_failure': (7, False),
    'candidate_check_failure': (7, False), 'absent_candidate_check_failure': (7, False),
    'missing_receipt': (1, False), 'collector_stopped': (1, False), 'inventory_failure': (1, False),
    'rollback': (0, True), 'check_success': (0, False), 'check_error_log': (1, False),
}
slots = {
    'c': ('youtube-collector', 30025, 'central'),
    'b': ('youtube-collector-b', 30015, 'iris-seoul'),
}
runs = [('c', case) for case in central_cases] + [('b', case) for case in ('success', 'rollback', 'check_success')]
for slot, case in runs:
    expected_status, restored = central_cases[case]
    service, port, host_label = slots[slot]
    slot_vars = f'slot={slot} service={service} port={port} host_label={host_label}\n'
    collector_unit, issuer_unit = f'hololive-youtube-collector-{slot}.service', f'hololive-youtube-po-{slot}.service'
    label = f'{slot} {case}'
    with tempfile.TemporaryDirectory(prefix='central-cutover-test-') as raw:
        state = Path(raw)
        current, staging, backup = state / 'current', state / 'staging', state / f'po-{slot}-{change}'
        for directory in (current, staging, state / 'images', state / 'units', state / f'po-current-{slot}'):
            directory.mkdir()
        (state / f'units/hololive-youtube-collector-{slot}.container').write_text('[Container]\n')
        is_check, is_rollback = case.startswith('check_'), case.startswith('rollback')
        issuer_absent = 'absent' in case
        initial = 'new' if is_check or is_rollback else 'old'
        image_file(state, collector_prod).write_text(initial)
        if not issuer_absent:
            image_file(state, issuer_prod).write_text(initial)
        for kind in ('collector', 'issuer'):
            absent = kind == 'issuer' and issuer_absent
            (state / f'{kind}.runtime').write_text('absent' if absent else initial)
            (state / f'{kind}.running').write_text('false' if absent or (kind == 'collector' and case == 'collector_stopped') else 'true')
        (state / 'dropin').write_text('candidate' if is_check or is_rollback else 'initial')
        for prefix, issuer in (('', True), ('collector-', False)):
            tar = staging / (prefix + 'image.tar')
            tar.write_text('reviewed fixture image')
            digest = subprocess.check_output(['sha256sum', tar], text=True).split()[0]
            (staging / (prefix + 'image.tar.sha256')).write_text(f'{digest}  {tar.name}\n')
            (staging / (prefix + 'image-id')).write_text(new_id)
            (staging / ('rootfs-manifest.json' if issuer else 'collector-manifest.json')).write_text(manifest(new_revision, new_id))
        live_po = state / f'po-current-{slot}'
        if case != 'missing_receipt' and not issuer_absent:
            (live_po / 'rootfs-manifest.json').write_text(manifest(new_revision if initial == 'new' else old_revision, new_id if initial == 'new' else old_id))
            (live_po / 'image-id').write_text(new_id if initial == 'new' else old_id)
        if is_check or is_rollback:
            image_file(state, collector_rollback).write_text('old')
            image_file(state, issuer_rollback).write_text('old')
            backup.mkdir()
            (backup / 'snapshot-complete').touch()
            previous_pair = current / 'artifacts/worker-profile/releases' / backup.name / service / 'previous'
            previous_pair.mkdir(parents=True)
            (previous_pair / 'profile.json').write_text('retained profile')
            # 이전 image가 돌던 기본 unit. sync가 이미 새 revision의 unit을 설치한 상황을 만든다.
            (previous_pair / 'unit.container').write_text('[Container]\n# previous revision\n')
            (backup / 'issuer.state').write_text('present')
            (backup / 'collector.revision').write_text(old_revision)
            (backup / 'collector-prechange-manifest.json').write_text(manifest(old_revision, old_id))
            (backup / 'collector-prechange.image-id').write_text(old_id)
            (backup / 'po-prechange-manifest.json').write_text(manifest(old_revision, old_id))
            (backup / 'po-prechange.image-id').write_text(old_id)
            (backup / 'change-started-at').write_text('2026-10-03T00:00:00Z')
        result = run(slot_vars + central_setup + central_config + central_functions + '\n' + central_main, state, case)
        require(result.returncode == expected_status, f'{label}: original status', result)
        calls = (state / 'calls').read_text().splitlines() if (state / 'calls').exists() else []
        changed = [call for call in calls if call.startswith(mutations)]
        restarts = [call for call in calls if call.startswith('systemctl restart ')]
        expected_warning = case in ('restore_failure', 'activation_failure')
        require(('automatic restoration failed' in result.stderr) == expected_warning, f'{label}: restoration warning', result)
        if case in ('missing_receipt', 'collector_stopped', 'inventory_failure'):
            # 이전 pair 기준점을 만들 수 없으면 백업·태그·적재 전에 원인을 남기고 멈춘다.
            require(not backup.exists() and not changed, f'{label}: no partial backup or runtime change', result)
        elif is_check:
            require(not changed, f'{label}: check has no mutations', result)
        elif case in ('candidate_check_failure', 'absent_candidate_check_failure', 'activation_failure'):
            # 후보 검사·drop-in 게시 실패는 :prod tag·drop-in·실행 중 pair를 바꾸지 않는다.
            require(not restarts, f'{label}: no unit restart', result)
            require(image_file(state, collector_prod).read_text() == 'old', f'{label}: collector tag baseline', result)
            require((image_file(state, issuer_prod).exists()) != issuer_absent, f'{label}: issuer tag baseline', result)
            require((state / 'dropin').read_text() == 'initial', f'{label}: drop-in unchanged', result)
            require(image_file(state, 'localhost/hololive-youtube-collector:candidate').read_text() == 'new', f'{label}: candidate retained for inspection', result)
        if case in ('success', 'absent_success'):
            require(restarts == [f'systemctl restart {unit}' for unit in (issuer_unit, collector_unit)], f'{label}: issuer-first restart order', result)
            # 원장 증명은 중앙 주 서버의 관리 socket이 있는 c에서만 한다.
            require(('lease-proof' in calls) == (slot == 'c'), f'{label}: lease proof only on central', result)
            require((state / 'dropin').read_text().strip() == 'candidate', f'{label}: candidate drop-in active', result)
            require(json.loads((live_po / 'rootfs-manifest.json').read_text())['source_revision'] == new_revision, f'{label}: issuer receipt promoted', result)
            require(calls.index('systemctl daemon-reload') < calls.index(f'systemctl restart {issuer_unit}'), f'{label}: drop-in reloaded before restart', result)
        if restored:
            require((state / 'dropin').read_text().strip() == 'previous', f'{label}: previous drop-in restored', result)
            require(image_file(state, collector_prod).read_text() == 'old' and image_file(state, issuer_prod).read_text() == 'old', f'{label}: previous tags restored', result)
            require(json.loads((live_po / 'rootfs-manifest.json').read_text())['source_revision'] == old_revision, f'{label}: previous issuer receipt restored', result)
        if case == 'restore_failure':
            require((state / 'collector.runtime').read_text() == 'old', f'{label}: failed restore leaves the unrestarted collector', result)
        if case == 'rollback':
            # 이전 image를 새 unit 정의로 띄우지 않도록 보존한 기본 unit도 되돌리고, 바뀐 unit은 release에 남긴다.
            release_dir = current / 'artifacts/worker-profile/releases' / backup.name / service
            require((state / f'units/hololive-youtube-collector-{slot}.container').read_text() == '[Container]\n# previous revision\n', f'{label}: previous base unit restored', result)
            require((release_dir / f'rollback-replaced-hololive-youtube-collector-{slot}.container').read_text() == '[Container]\n', f'{label}: replaced base unit retained', result)
            require(calls.index('systemctl daemon-reload') > -1, f'{label}: units reloaded', result)
        expected_runtime = 'new' if is_check or case in ('success', 'absent_success') else 'old'
        require((state / 'collector.runtime').read_text() == expected_runtime, f'{label}: collector runtime', result)
        expected_issuer = 'new' if case == 'restore_failure' else 'absent' if case == 'absent_candidate_check_failure' else expected_runtime
        require((state / 'issuer.runtime').read_text() == expected_issuer, f'{label}: issuer runtime', result)
        print(f'[PASS] {label}: exit={result.returncode}, collector={(state / "collector.runtime").read_text()}, issuer={(state / "issuer.runtime").read_text()}')
PY
