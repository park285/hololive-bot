#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# 실제 배포 함수와 실행 구간을 임시 runtime에 적용해 완료 판정과 복원 부작용을 검증합니다.
python3 - "$root" <<'PY'
import hashlib
import fcntl
import json
import re
import shlex
import shutil
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

proof_setup = r'''
set -euo pipefail
fixture_root="$1" fixture_case="$2" staging="$3" collector_container=youtube-collector
awk() {
  if [[ "${@: -1}" == /etc/stack-secrets/hololive-bot/compose.env ]]; then
    printf 'hololive_migrator\n'
  else
    command awk "$@"
  fi
}
timeout() { [[ "$1" == 20 ]] || return 2; shift; "$@"; }
podman() {
  case "$1" in
    inspect)
      if [[ "$3" == *'.Config.Env'* ]]; then
        cat "$fixture_root/collector.env"
      elif [[ "$4" == holo-postgres ]]; then
        printf '%064d\n' 0
      else
        return 2
      fi ;;
    run)
      printf '%s\n' "$@" > "$fixture_root/query-args"
      case "$fixture_case" in
        query_failure) return 7 ;;
        writable_proof) printf 'off|%s\n' "$(<"$fixture_root/checksum")" ;;
        wrong_checksum) printf 'on|wrong\n' ;;
        *) printf 'on|%s\n' "$(<"$fixture_root/checksum")" ;;
      esac ;;
    ps) return 0 ;;
    *) return 2 ;;
  esac
}
'''
proof_function = function(central, 'require_collector_lease_migration')
proof_checksum = hashlib.sha256(
    (root / 'hololive/hololive-api/scripts/migrations/001_schema_epoch3_baseline.sql').read_bytes()
).hexdigest()
for case, endpoint, success, queries in (
    ('pooler', ('holo-pgbouncer', '6432', 'hololive'), True, True),
    ('direct', ('holo-postgres', '5432', 'hololive'), True, True),
    ('foreign', ('other-pooler', '6432', 'hololive'), False, False),
    ('wrong_port', ('holo-pgbouncer', '5432', 'hololive'), False, False),
    ('wrong_database', ('holo-pgbouncer', '6432', 'other'), False, False),
    ('query_failure', ('holo-pgbouncer', '6432', 'hololive'), False, True),
    ('writable_proof', ('holo-pgbouncer', '6432', 'hololive'), False, True),
    ('wrong_checksum', ('holo-pgbouncer', '6432', 'hololive'), False, True),
):
    with tempfile.TemporaryDirectory(prefix='collector-proof-test-') as raw:
        state = Path(raw)
        host, port, database = endpoint
        (state / 'collector.env').write_text(
            f'POSTGRES_HOST={host}\nPOSTGRES_PORT={port}\nPOSTGRES_DB={database}\n'
        )
        (state / 'checksum').write_text(proof_checksum)
        result = run(proof_setup + proof_function + '\nrequire_collector_lease_migration\n', state, case)
        require((result.returncode == 0) == success, f'{case}: primary route proof outcome', result)
        query_args = state / 'query-args'
        require(query_args.exists() == queries, f'{case}: query only after route validation', result)
        if queries:
            args = query_args.read_text().splitlines()
            for flag, value in (
                ('--network', 'none'),
                ('--volume', 'hololive-bot_holo-pg-admin-socket:/var/run/postgresql:ro'),
                ('--env', 'PGHOST=/var/run/postgresql'),
                ('--env', 'PGPORT=5432'),
                ('--env', 'PGUSER=hololive_migrator'),
                ('--env', 'PGOPTIONS=-c default_transaction_read_only=on -c statement_timeout=5s -c lock_timeout=1s'),
            ):
                require(any(args[i:i + 2] == [flag, value] for i in range(len(args) - 1)),
                        f'{case}: private read-only ledger transport {value}', result)
        print(f'[PASS] collector migration proof: {case}')

for slot, host, port, accepted in (
    ('c', 'holo-pgbouncer', 6432, True), ('c', 'holo-postgres', 5432, False),
    ('c', 'foreign', 6432, False), ('b', '100.100.1.8', 6433, True),
    ('b', '100.100.1.8', 5433, False), ('b', '100x100x1x8', 6433, False),
):
    with tempfile.TemporaryDirectory(prefix='collector-endpoint-test-') as raw:
        state = Path(raw)
        (state / 'collector.env').write_text(
            f'POSTGRES_HOST={host}\nPOSTGRES_PORT={port}\nPOSTGRES_DB=hololive\n'
        )
        result = run(proof_setup + f'\nslot={slot}\n' + function(central, 'require_collector_pooler_endpoint') +
                     '\nrequire_collector_pooler_endpoint\n', state, 'endpoint')
        require((result.returncode == 0) == accepted, 'post-activation pooler endpoint', result)

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
. "$3/scripts/deploy/lib/compose-env.sh"
. "$3/scripts/deploy/lib/postgres-endpoint.sh"
. "$3/scripts/deploy/lib/ap-host-native-rollback-check.sh"
payload="$state/payload" release_dir="$state/releases/new"
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
for case in ('success', 'journal_failure', 'error_log', 'grep_failure', 'ordinary_failure', 'substitution_failure', 'restore_failure', 'endpoint_change'):
    with tempfile.TemporaryDirectory(prefix='native-cutover-test-') as raw:
        state = Path(raw)
        for name in ('old', 'new', 'older'):
            (state / 'releases' / name).mkdir(parents=True)
        contract = state / 'releases/old/rollback-contract'
        contract.mkdir()
        for name in ('youtube-collector-host.env', 'hololive-youtube-collector@.service'):
            (contract / name).write_text('old')
        old_env = 'POSTGRES_HOST=100.100.1.8\nPOSTGRES_PORT=' + ('5433' if case == 'endpoint_change' else '6433') + '\n'
        new_env = 'POSTGRES_HOST=100.100.1.8\nPOSTGRES_PORT=6433\n'
        (contract / 'youtube-collector-host.env').write_text(old_env)
        (state / 'payload').mkdir()
        (state / 'payload/youtube-collector-host.env').write_text(new_env)
        (contract / 'previous-before-cutover').write_text(str(state / 'releases/older') + '\n')
        (state / 'current').symlink_to(state / 'releases/new')
        (state / 'previous').symlink_to(state / 'releases/old')
        for name in ('host.env', 'unit', 'runtime'):
            (state / name).write_text('new')
        (state / 'host.env').write_text(new_env)
        action = {'ordinary_failure': 'false', 'endpoint_change': 'false', 'substitution_failure': 'ready="$(exit 7)"'}.get(case, native_journal)
        result = run(native_setup + native_functions + '\nnative_cutover_claim "$state/releases/new"\narm_native_cutover_restore\n' + ('grep() { return 2; };\n' if case == 'grep_failure' else '') + action, state, case)
        expected_status = 0 if case == 'success' else 7 if case == 'substitution_failure' else 2 if case == 'grep_failure' else 1
        require(result.returncode == expected_status, f'native {case}: original status', result)
        calls = (state / 'calls').read_text().splitlines() if (state / 'calls').exists() else []
        expected_current = 'new' if case in ('success', 'endpoint_change') else 'old'
        expected_previous = 'old' if case in ('success', 'endpoint_change') else 'older'
        expected_runtime = 'new' if case == 'success' else 'stopped' if case in ('restore_failure', 'endpoint_change') else 'old'
        require((state / 'current').resolve().name == expected_current, f'native {case}: current restored', result)
        require((state / 'previous').resolve().name == expected_previous, f'native {case}: previous restored', result)
        require((state / 'runtime').read_text().strip() == expected_runtime, f'native {case}: runtime restored', result)
        if case == 'endpoint_change':
            require((state / 'host.env').read_text() == new_env, 'cross-endpoint rollback leaves candidate env intact', result)
            require((state / 'recovery').exists() and (contract / 'youtube-collector-host.env').read_text() == old_env,
                    'cross-endpoint rollback preserves recovery guard and previous env', result)
            require(not any(call.startswith('systemctl enable') for call in calls), 'cross-endpoint rollback never restarts', result)
        elif case != 'success':
            require((state / 'host.env').read_text() == old_env and (state / 'unit').read_text() == 'old', f'native {case}: installed contract restored', result)
        print(f'[PASS] native {case}: exit={result.returncode}, current={expected_current}, previous={expected_previous}, runtime={expected_runtime}')


# 중앙(c)과 iris-seoul(b)은 rootful Podman Quadlet이다. 실제 원격 본문을 가짜 podman·systemctl·pair tool로 실행해
# issuer 우선 교체, 이전 drop-in 복원, 후보 검사 실패의 무변경과 b의 source checkout 없는 재개를 검증한다.
central_functions = '\n'.join(
    match.group() for match in re.finditer(r'^([a-z_]+)\(\) \{\n.*?^\}', central, re.M | re.S)
    if match.group(1) not in ('require_collector_lease_migration', 'restore_after_failed_cutover')
)
central_config = central[central.index('manifest="../'):central.index('[[ "$(stat')]
central_main = central[central.index('# 이 본문은 ssh bash -s'):]
central_source_gate = central[central.index('# 재개 입력 검증'):central.index('compose_env=')]
old_revision, new_revision = 'a' * 40, 'b' * 40
old_id, new_id = 'sha256:' + 'a' * 64, 'sha256:' + 'b' * 64
change = '20261003T000000Z'
central_setup = r'''
set -Eeuo pipefail
fixture_root="$1" fixture_case="$2"
# staged helper는 원격 본문과 같은 checksum gate 뒤에서만 읽는다.
compose_env="$fixture_root/compose.env"
remote_root="$fixture_root"
current="$fixture_root/current" staging="$fixture_root/staging" backup="$fixture_root/po-$slot-20261003T000000Z"
unit_dir="$fixture_root/units"
revision=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb version=6.0.1
pair_tool="$staging/scripts/deploy/lib/worker-profile-pair.py"
cutover_started=false
mode=deploy
[[ "$fixture_case" != check_* ]] || mode=check
[[ "$fixture_case" != rollback* ]] || mode=rollback
case "$fixture_case" in
  maintenance_quiesce*) mode=quiesce ;;
  maintenance_resume*) mode=resume ;;
  maintenance_check*) mode=check ;;
  maintenance_rollback*) mode=rollback ;;
esac
ref_file() { printf '%s/images/%s\n' "$fixture_root" "${1//[\/:]/_}"; }
hex_of() { if [[ "$1" == old ]]; then printf '%064d\n' 0 | tr 0 a; else printf '%064d\n' 0 | tr 0 b; fi; }
revision_of() { if [[ "$1" == old ]]; then printf '%040d\n' 0 | tr 0 a; else printf '%040d\n' 0 | tr 0 b; fi; }
python3() {
  if [[ "${1:-}" == "$pair_tool" ]]; then
    printf 'pair %s %s\n' "$2" "$4" >>"$fixture_root/calls"
    case "$2" in
      capture)
        mkdir -p "$4"; printf 'retained profile\n' > "$4/profile.json"
        cp "$unit_dir/$collector_container.container" "$4/unit.container" ;;
      candidate)
        mkdir -p "$4"
        cat >"$4/unit.container"
        # 후보 pair는 방금 적재해 검증한 :candidate image identity를 고정해야 한다.
        [[ "$5" == "sha256:$(hex_of new)" ]] || return 3
        printf 'Image=%s\n' "$5" > "$4/50-worker-profile.conf"
        if [[ -f "$fixture_root/source.profile" ]]; then
          cp "$fixture_root/source.profile" "$4/profile.json"
        else
          printf 'candidate profile\n' > "$4/profile.json"
        fi ;;
      rollback) test -f "$4/profile.json"; touch "$4/50-worker-profile.conf" ;;
      check)
        if [[ "$fixture_case" == *candidate_check_failure && "$4" == */candidate ]]; then return 7; fi
        test -f "$4/profile.json" ;;
      verify|verify-live) test -f "$4/profile.json" ;;
      verify-dropin)
        cmp "$4/50-worker-profile.conf" <(printf 'Image=sha256:%s\n' "$(hex_of new)") ;;
      verify-source)
        test -f "$4/profile.json"
        if [[ -f "$fixture_root/source.profile" ]]; then cmp "$fixture_root/source.profile" "$4/profile.json"; fi ;;
      activate)
        [[ "$fixture_case" != *activation_failure ]] || return 7
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
stat() {
  if [[ "$2" == '%d:%i' ]]; then command stat "$@"; return; fi
  case "${*: -1}" in
    "$fixture_root/socket") printf '65532:1000 770\n' ;;
    *) printf '0:0 %s\n' "$(command stat -c '%a' "${*: -1}")" ;;
  esac
}
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
        *Config.Env*)
          if [[ "$slot" == c ]]; then
            if [[ "$fixture_case" == maintenance_* && "$value" == old || "$fixture_case" == transition_* && "$value" == old || "$fixture_case" == transition_wrong_endpoint ]]; then
              printf 'POSTGRES_HOST=holo-postgres\nPOSTGRES_PORT=5432\nPOSTGRES_DB=hololive\n'
            else
              printf 'POSTGRES_HOST=holo-pgbouncer\nPOSTGRES_PORT=6432\nPOSTGRES_DB=hololive\n'
            fi
          else
            if [[ "$fixture_case" == maintenance_* && "$value" == old || "$fixture_case" == transition_* && "$value" == old || "$fixture_case" == transition_wrong_endpoint ]]; then
              printf 'POSTGRES_HOST=100.100.1.8\nPOSTGRES_PORT=5433\nPOSTGRES_DB=hololive\n'
            else
              printf 'POSTGRES_HOST=100.100.1.8\nPOSTGRES_PORT=6433\nPOSTGRES_DB=hololive\n'
            fi
          fi ;;
        *State.Health*) printf 'healthy\n' ;;
        *image.revision*) revision_of "$value" ;;
        '{{.Image}}') hex_of "$value" ;;
        *Mounts*) printf '%s/socket\n' "$fixture_root" ;;
        '{{.State.StartedAt.Unix}}') command date -u -d 2026-10-03T00:01:01Z +%s ;;
        *) return 2 ;;
      esac
      ;;
    'exec holo-pgbouncer')
      [[ "$*" == 'exec holo-pgbouncer sha256sum /etc/pgbouncer/pgbouncer.ini' ]] || return 2
      [[ ! -e "$fixture_root/pooler-query-failed" ]] || return 125
      value="$(sha256sum "$fixture_root/live-pgbouncer.ini")" || return
      printf '%s  /etc/pgbouncer/pgbouncer.ini\n' "${value%% *}" ;;
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
    show)
      case "$3" in
        LoadState)
          if [[ -L "$system_unit_dir/${*: -1}" ]]; then printf 'masked\n'; else printf 'loaded\n'; fi ;;
        ActiveState)
          if [[ "$(cat "$fixture_root/$kind.running")" == true ]]; then printf 'active\n'; else printf 'inactive\n'; fi ;;
        *) return 2 ;;
      esac ;;
    mask)
      [[ "$fixture_case" != *mask_failure || "$kind" != collector ]] || return 8
      ln -s /dev/null "$system_unit_dir/$2" ;;
    unmask)
      [[ "$fixture_case" != *unmask_failure || "$kind" != collector ]] || return 8
      rm "$system_unit_dir/$2" ;;
    is-active) [[ "$(cat "$fixture_root/$kind.running")" == true ]] ;;
    restart)
      [[ ! -L "$system_unit_dir/$2" ]] || return 8
      if [[ "$fixture_case" == maintenance_* ]]; then
        [[ ! -e "$fixture_root/db.stopped" ]] || return 9
        test -f "$profile_release/candidate/profile.json" || [[ "$mode" == rollback ]] || return 9
        test -f "$current/$manifest" && test -f "$current/$image_id_file" || return 9
        if [[ "$mode" == resume ]]; then
          [[ "$(<"$current/$image_id_file")" == "sha256:$(hex_of new)" ]] || return 9
        fi
        [[ "$(cat "$fixture_root/dropin")" == candidate || "$mode" == rollback ]] || return 9
      fi
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
    stop)
      [[ "$fixture_case" != *stop_failure || "$kind" != collector ]] || return 8
      printf absent > "$fixture_root/$kind.runtime"; printf false > "$fixture_root/$kind.running" ;;
    *) return 2 ;;
  esac
}
if [[ "$fixture_case" == *grep_failure ]]; then grep() { if [[ "$*" == *'ERR|panic'* ]]; then return 2; else command grep "$@"; fi; }; fi
'''


def manifest(revision, identity):
    return json.dumps({'schema_version': 1, 'source_revision': revision, 'go': {'goarch': 'arm64'}, 'architecture': 'arm64', 'image_id': identity})


def image_file(state, ref):
    return state / 'images' / ref.replace('/', '_').replace(':', '_')


collector_prod, issuer_prod = 'localhost/hololive-youtube-collector:prod', 'localhost/hololive-youtube-po-sandbox:prod'
collector_rollback = f'localhost/hololive-youtube-collector:rollback-{change}'
issuer_rollback = f'localhost/hololive-youtube-po-sandbox:rollback-{change}'
mutations = ('podman tag ', 'podman load ', 'podman image rm ', 'systemctl restart ', 'systemctl stop ', 'systemctl mask ', 'systemctl unmask ', 'systemctl daemon-reload', 'pair activate ')
central_cases = {
    # case: (exit status, restored to previous pair)
    'success': (0, False), 'absent_success': (0, False),
    'substitution_failure': (1, True), 'deploy_logs_collector': (5, True), 'deploy_error_log': (1, True),
    'deploy_grep_failure': (2, True), 'signal_TERM': (143, True), 'signal_HUP': (129, True),
    'restore_failure': (1, False), 'activation_failure': (7, False),
    'candidate_check_failure': (7, False), 'absent_candidate_check_failure': (7, False),
    'missing_receipt': (1, False), 'collector_stopped': (1, False), 'inventory_failure': (1, False),
    'rollback': (0, True), 'check_success': (0, False), 'check_error_log': (1, False),
    'transition_success': (0, False), 'transition_error_log': (1, False),
    'transition_wrong_endpoint': (1, False), 'rollback_cross': (1, False),
    'capacity_drift': (1, False), 'capacity_missing': (1, False), 'capacity_query_failed': (1, False),
    'rollback_capacity_drift': (1, False),
}
slots = {
    'c': ('youtube-collector', 30025, 'central'),
    'b': ('youtube-collector-b', 30015, 'iris-seoul'),
}
runs = [('c', case) for case in central_cases] + [('b', case) for case in ('success', 'rollback', 'check_success', 'transition_success', 'transition_error_log', 'transition_wrong_endpoint', 'rollback_cross')]
maintenance_cases = (
    'success', 'reboot', 'mask_failure', 'stop_failure', 'activation_failure',
    'candidate_check_failure', 'substitution_failure', 'error_log', 'unmask_failure',
    'source_changed', 'identity_changed', 'inputs_changed', 'foreign_hold', 'lock_busy',
    'operator_mask', 'incomplete_hold', 'candidate_unit_changed', 'candidate_profile_changed', 'wrong_endpoint',
    'installed_unit_changed', 'helper_changed', 'migration_changed', 'source_manifest_changed',
    'candidate_image_overlay_changed', 'candidate_endpoint_overlay_changed',
    'rollback_cross', 'rollback_coordinated', 'capacity_drift', 'capacity_missing', 'capacity_query_failed',
)
runs += [(slot, 'maintenance_' + case) for slot in slots for case in maintenance_cases
         if (slot == 'c' or not case.startswith('capacity_')) and (slot == 'b' or case != 'installed_unit_changed')]
for slot, case in runs:
    expected_status, restored = central_cases.get(case, (0, False))
    service, port, host_label = slots[slot]
    slot_vars = f'slot={slot} service={service} port={port} host_label={host_label}\n'
    collector_unit, issuer_unit = f'hololive-youtube-collector-{slot}.service', f'hololive-youtube-po-{slot}.service'
    label = f'{slot} {case}'
    with tempfile.TemporaryDirectory(prefix='central-cutover-test-') as raw:
        state = Path(raw)
        current, staging, backup = state / 'current', state / 'staging', state / f'po-{slot}-{change}'
        for directory in (current, staging, state / 'images', state / 'units', state / f'po-current-{slot}'):
            directory.mkdir()
        host, db_port = ('holo-pgbouncer', 6432) if slot == 'c' else ('100.100.1.8', 6433)
        if case.startswith('maintenance_'):
            host, db_port = ('holo-postgres', 5432) if slot == 'c' else ('100.100.1.8', 5433)
        installed_definition = f'[Container]\nEnvironment=POSTGRES_HOST={host}\nEnvironment=POSTGRES_PORT={db_port}\n'
        if slot == 'c':
            installed_definition += 'Environment=POSTGRES_POOL_MAX_CONNS=${YOUTUBE_COLLECTOR_POSTGRES_POOL_MAX_CONNS}\n[Service]\nEnvironment=YOUTUBE_COLLECTOR_POSTGRES_POOL_MAX_CONNS=7\nEnvironmentFile=/etc/stack-secrets/hololive-bot/compose.env\n'
        else:
            installed_definition = (root / 'deploy/podman/iris-seoul/hololive-youtube-collector-b.container').read_text().replace(
                'Environment=POSTGRES_PORT=6433', f'Environment=POSTGRES_PORT={db_port}')
        previous_host, previous_port = (('holo-postgres', 5432) if slot == 'c' else ('100.100.1.8', 5433)) if case == 'rollback_cross' else (host, db_port)
        previous_definition = installed_definition.replace(f'POSTGRES_HOST={host}', f'POSTGRES_HOST={previous_host}').replace(f'POSTGRES_PORT={db_port}', f'POSTGRES_PORT={previous_port}') + '# previous revision\n'
        (state / f'units/hololive-youtube-collector-{slot}.container').write_text(installed_definition)
        (state / 'compose.env').write_text('APP_ENV=production\n')
        # capacity 경계는 앱 용량뿐 아니라 pooler의 설치 원천도 확인한다.
        for name in ('hololive-api', 'hololive-alarm-worker', 'holo-pgbouncer'):
            shutil.copyfile(root / f'deploy/quadlet/hololive-bot/{name}.container', state / f'units/{name}.container')
        source_files = (root / 'scripts/deploy/po-central-files.txt').read_text().splitlines()
        for name in source_files:
            target = staging / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(root / name, target)
        (staging / 'source-files.sha256').write_text(''.join(
            f'{hashlib.sha256((staging / name).read_bytes()).hexdigest()}  {name}\n' for name in source_files))
        subprocess.run(['bash', '-c', '. "$1/scripts/deploy/lib/postgres-capacity.sh"; postgres_capacity_write_pgbouncer_config "$1/scripts/ci/postgres-capacity-policy.tsv" "$2/compose.env" "$2/live-pgbouncer.ini"',
                        '_', str(root), str(state)], check=True, capture_output=True)
        if case in ('capacity_drift', 'rollback_capacity_drift'):
            (state / 'compose.env').write_text('YOUTUBE_COLLECTOR_POSTGRES_POOL_MAX_CONNS=6\n')
        elif case == 'capacity_missing':
            (state / 'live-pgbouncer.ini').unlink()
        elif case == 'capacity_query_failed':
            (state / 'pooler-query-failed').touch()
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
            (previous_pair / 'unit.container').write_text(previous_definition)
            (backup / 'issuer.state').write_text('present')
            (backup / 'collector.revision').write_text(old_revision)
            (backup / 'collector-prechange-manifest.json').write_text(manifest(old_revision, old_id))
            (backup / 'collector-prechange.image-id').write_text(old_id)
            (backup / 'po-prechange-manifest.json').write_text(manifest(old_revision, old_id))
            (backup / 'po-prechange.image-id').write_text(old_id)
            (backup / 'change-started-at').write_text('2026-10-03T00:00:00Z')
        body = slot_vars + central_setup + central_config + '\nsystem_unit_dir="$fixture_root/system-units"\n' + central_functions + '\n' + central_source_gate + '\n' + central_main
        (state / 'system-units').mkdir()
        if case.startswith('maintenance_'):
            scenario = case.removeprefix('maintenance_')
            hold = state / f'po-{slot}.hold'
            masks = [state / 'system-units' / name for name in (issuer_unit, collector_unit)]
            release = current / 'artifacts/worker-profile/releases' / backup.name / service
            (state / 'source.profile').write_text('reviewed candidate profile\n')
            if slot == 'c':
                (current / '.source-revision').write_text(old_revision)

            def invoke(mode, failure=''):
                return run(body, state, 'maintenance_' + mode + ('_' + failure if failure else ''))

            def calls_now():
                return (state / 'calls').read_text().splitlines() if (state / 'calls').exists() else []

            def assert_held(result):
                require(hold.is_dir() and (hold / 'backup').read_text().strip() == str(backup),
                        f'{slot} {scenario}: hold ownership retained', result)
                require(all(mask.is_symlink() and str(mask.readlink()) == '/dev/null' for mask in masks),
                        f'{slot} {scenario}: persistent masks', result)
                require(all((state / f'{kind}.running').read_text() == 'false' for kind in ('issuer', 'collector')),
                        f'{slot} {scenario}: writers inactive', result)

            if scenario == 'operator_mask':
                masks[0].symlink_to('/dev/null')
                result = invoke('quiesce')
                require(result.returncode != 0 and not backup.exists() and not hold.exists(),
                        f'{slot}: refuse unrelated mask before snapshot', result)
                require(masks[0].is_symlink() and not any(c.startswith(mutations) for c in calls_now()),
                        f'{slot}: unrelated mask untouched', result)
                continue
            if scenario == 'lock_busy':
                with (state / f'po-{slot}.lock').open('w') as lock_file:
                    (state / f'po-{slot}.lock').chmod(0o600)
                    fcntl.flock(lock_file, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    result = invoke('quiesce')
                    require(result.returncode != 0 and not backup.exists() and not calls_now(),
                            f'{slot}: competing operation rejected by slot lock', result)
                continue
            (state / 'live-pgbouncer.ini').rename(state / 'stopped-pgbouncer.ini')
            result = invoke('quiesce', scenario if scenario in ('mask_failure', 'stop_failure') else '')
            if scenario in ('mask_failure', 'stop_failure'):
                require(result.returncode != 0 and hold.exists() and (backup / 'snapshot-complete').exists(),
                        f'{slot} {scenario}: partial failure retains recovery', result)
                require(not any(c.startswith('systemctl restart ') for c in calls_now()),
                        f'{slot} {scenario}: no old writer restart', result)
                require((state / 'issuer.running').read_text() == 'false' and masks[0].is_symlink(),
                        f'{slot} {scenario}: issuer stopped first', result)
                require((state / 'collector.running').read_text() == ('true' if scenario == 'stop_failure' else 'false'),
                        f'{slot} {scenario}: stop outcome preserved', result)
                require(masks[1].is_symlink() == (scenario == 'stop_failure'),
                        f'{slot} {scenario}: mask outcome preserved', result)
                before = len(calls_now())
                rejected = invoke('resume')
                require(rejected.returncode != 0 and not any(c.startswith(mutations) for c in calls_now()[before:]),
                        f'{slot} {scenario}: incomplete hold cannot resume', rejected)
                continue
            require(result.returncode == 0, f'{slot} {scenario}: live direct pair quiesced', result)
            require(not any(c.startswith('podman exec holo-pgbouncer ') for c in calls_now()),
                    f'{slot}: first quiesce does not require a pooler', result)
            (state / 'stopped-pgbouncer.ini').rename(state / 'live-pgbouncer.ini')
            assert_held(result)
            require((release / 'previous/unit.container').read_text() == installed_definition and not (release / 'candidate').exists(),
                    f'{slot} {scenario}: old unit preserved without premature candidate', result)
            require(image_file(state, collector_prod).read_text() == 'old' and (state / 'dropin').read_text() == 'initial',
                    f'{slot} {scenario}: quiesce does not publish candidate', result)
            stops = [c for c in calls_now() if c.startswith('systemctl stop ')]
            require(stops == [f'systemctl stop {u}' for u in (issuer_unit, collector_unit)],
                    f'{slot} {scenario}: issuer-first stop', result)
            (state / 'db.stopped').touch()
            before = len(calls_now())
            checked = invoke('check')
            competed = invoke('quiesce')
            require(checked.returncode != 0 and competed.returncode != 0 and
                    not any(c.startswith(mutations) for c in calls_now()[before:]),
                    f'{slot} {scenario}: held state is not completed or replaceable', checked)
            if scenario == 'reboot':
                # 새 supervisor가 디스크의 persistent mask로 기동 여부를 결정한다.
                for kind, mask in zip(('issuer', 'collector'), masks):
                    (state / f'{kind}.running').write_text('false' if mask.is_symlink() else 'true')
                assert_held(result)
            pool_host, pool_port = ('holo-pgbouncer', 6432) if slot == 'c' else ('100.100.1.8', 6433)
            candidate_definition = installed_definition.replace(f'POSTGRES_HOST={host}', f'POSTGRES_HOST={pool_host}').replace(f'POSTGRES_PORT={db_port}', f'POSTGRES_PORT={pool_port}')
            unit_path = state / f'units/hololive-youtube-collector-{slot}.container'
            unit_path.write_text(candidate_definition)
            if slot == 'c':
                (current / '.source-revision').write_text(new_revision)
            else:
                # b는 checkout·revision marker 없이 staged unit만 수동 설치한다.
                shutil.copyfile(staging / 'deploy/podman/iris-seoul/hololive-youtube-collector-b.container', unit_path)
                require(unit_path.read_text() == candidate_definition and not (current / '.source-revision').exists(),
                        'b: actual staged candidate installed without source marker', result)
            (state / 'db.stopped').unlink()
            if scenario == 'capacity_drift':
                (state / 'compose.env').write_text('YOUTUBE_COLLECTOR_POSTGRES_POOL_MAX_CONNS=6\n')
            elif scenario == 'capacity_missing':
                (state / 'live-pgbouncer.ini').unlink()
            elif scenario == 'capacity_query_failed':
                (state / 'pooler-query-failed').touch()
            if scenario == 'wrong_endpoint':
                unit_path.write_text(installed_definition)
            if scenario == 'source_changed':
                if slot == 'c':
                    (current / '.source-revision').write_text('c' * 40)
                else:
                    staged_unit = staging / 'deploy/podman/iris-seoul/hololive-youtube-collector-b.container'
                    staged_unit.write_text(staged_unit.read_text() + '# unreviewed source\n')
            elif scenario == 'installed_unit_changed':
                unit_path.write_text(candidate_definition + 'Environment=EXTRA=changed\n')
            elif scenario == 'helper_changed':
                helper = staging / 'scripts/deploy/lib/compose-env.sh'
                helper.write_text(helper.read_text() + f'\ntouch "{state}/unreviewed-helper-ran"\n')
            elif scenario == 'migration_changed':
                (staging / 'hololive/hololive-api/scripts/migrations/001_schema_epoch3_baseline.sql').write_text('-- changed\n')
            elif scenario == 'source_manifest_changed':
                (staging / 'source-files.sha256').write_text(
                    f'{hashlib.sha256(unit_path.read_bytes()).hexdigest()}  deploy/podman/iris-seoul/hololive-youtube-collector-b.container\n')
            elif scenario == 'identity_changed':
                image_file(state, 'localhost/hololive-youtube-collector:candidate').write_text('old')
            elif scenario == 'inputs_changed':
                (staging / 'collector-image-id').write_text(old_id)
            elif scenario == 'foreign_hold':
                (hold / 'backup').write_text(str(backup) + '-foreign\n')
            elif scenario == 'incomplete_hold':
                masks[1].unlink()
            elif scenario in ('candidate_unit_changed', 'candidate_profile_changed',
                              'candidate_image_overlay_changed', 'candidate_endpoint_overlay_changed'):
                failed = invoke('resume', 'candidate_check_failure')
                require(failed.returncode != 0, f'{slot}: retain candidate after validation failure', failed)
                assert_held(failed)
                if scenario == 'candidate_unit_changed':
                    unit_path.write_text(candidate_definition + 'Environment=EXTRA=changed\n')
                elif scenario == 'candidate_profile_changed':
                    (state / 'source.profile').write_text('unreviewed profile\n')
                else:
                    overlay = release / 'candidate/50-worker-profile.conf'
                    changed_overlay = overlay.read_text() + ('Image=unreviewed\n' if scenario == 'candidate_image_overlay_changed'
                                                             else 'Environment=POSTGRES_PORT=5432\n')
                    overlay.write_text(changed_overlay)
            before = len(calls_now())
            if scenario.startswith('rollback_'):
                result = invoke('rollback')
                require(result.returncode != 0 and not any(c.startswith(mutations) for c in calls_now()[before:]),
                        f'{slot}: cross-endpoint rollback refused under hold', result)
                assert_held(result)
                if scenario == 'rollback_cross':
                    continue
                unit_path.write_text(installed_definition)
                (state / 'live-pgbouncer.ini').unlink()
                result = invoke('rollback')
                require(result.returncode == 0 and not hold.exists() and all(not m.is_symlink() for m in masks),
                        f'{slot}: coordinated rollback releases owned masks', result)
                require(all((state / f'{k}.runtime').read_text() == 'old' for k in ('issuer', 'collector')),
                        f'{slot}: rollback restores original pair', result)
                require(unit_path.read_text() == installed_definition and
                        (release / 'previous/unit.container').read_text() == installed_definition,
                        f'{slot}: coordinated rollback retains the exact previous unit', result)
            else:
                failure = scenario if scenario in ('activation_failure', 'candidate_check_failure', 'substitution_failure', 'error_log', 'unmask_failure') else ''
                result = invoke('resume', failure)
                success = scenario in ('success', 'reboot')
                require((result.returncode == 0) == success, f'{slot} {scenario}: resume outcome', result)
                subsequent = calls_now()[before:]
                require(not any(c.startswith('podman load ') or c.startswith('pair capture ') for c in subsequent),
                        f'{slot} {scenario}: resume neither reloads nor recaptures', result)
                if success:
                    require(not hold.exists() and all(not m.is_symlink() for m in masks),
                            f'{slot}: successful resume releases ownership', result)
                    require((release / 'candidate/unit.container').read_text() == candidate_definition,
                            f'{slot}: candidate uses actual installed unit', result)
                    require([c for c in subsequent if c.startswith('systemctl restart ')] ==
                            [f'systemctl restart {u}' for u in (issuer_unit, collector_unit)],
                            f'{slot}: issuer-first resume', result)
                    require(all((state / f'{k}.runtime').read_text() == 'new' for k in ('issuer', 'collector')),
                            f'{slot}: candidate pair running', result)
                    checked = invoke('check')
                    require(checked.returncode == 0, f'{slot}: completion check after resume', checked)
                else:
                    require(hold.exists() and not any(c.startswith('pair activate ') and c.endswith('/previous') for c in subsequent),
                            f'{slot} {scenario}: no automatic direct restoration', result)
                    require((release / 'previous/unit.container').read_text() == installed_definition and
                            not (state / 'unreviewed-helper-ran').exists(),
                            f'{slot}: recovery unit retained and unverified helper never executed', result)
                    if scenario not in ('foreign_hold', 'incomplete_hold'):
                        assert_held(result)
                    if scenario in ('candidate_image_overlay_changed', 'candidate_endpoint_overlay_changed'):
                        require(not any(c.startswith(mutations) for c in subsequent) and
                                overlay.read_text() == changed_overlay,
                                f'{slot}: modified overlay refused without publishing or repairing it', result)
                    if scenario.startswith('capacity_'):
                        require(not any(c.startswith(mutations) for c in subsequent) and (release / 'previous/profile.json').exists(),
                                f'{slot}: capacity refusal preserves hold and recovery before effects', result)
                    if scenario not in ('substitution_failure', 'error_log', 'unmask_failure'):
                        require(not any(c.startswith(('systemctl restart ', 'systemctl unmask ')) for c in subsequent),
                                f'{slot} {scenario}: validation fails before unmask or activation', result)
            print(f'[PASS] {slot} maintenance {scenario}')
            continue
        result = run(body, state, case)
        require(result.returncode == expected_status, f'{label}: original status', result)
        calls = (state / 'calls').read_text().splitlines() if (state / 'calls').exists() else []
        changed = [call for call in calls if call.startswith(mutations)]
        restarts = [call for call in calls if call.startswith('systemctl restart ')]
        endpoint_blocked = case in ('transition_error_log', 'transition_wrong_endpoint')
        if endpoint_blocked:
            require((state / 'collector.running').read_text() == 'false' and (state / 'issuer.running').read_text() == 'false',
                    f'{label}: both services stopped', result)
            require((backup / 'snapshot-complete').exists() and (backup / 'collector.endpoint').exists(),
                    f'{label}: recovery records retained', result)
            require(not any(call.startswith('pair activate ') and call.endswith('/previous') for call in calls),
                    f'{label}: previous pair never activated', result)
        if case == 'rollback_cross':
            require(not changed, f'{label}: manual cross-endpoint rollback has no runtime mutations', result)
        if case.startswith('capacity_') or case == 'rollback_capacity_drift':
            require(not changed, f'{label}: capacity rejection has no runtime mutations', result)
            if is_rollback:
                require((previous_pair / 'profile.json').exists() and
                        (state / f'units/hololive-youtube-collector-{slot}.container').read_text() == installed_definition,
                        f'{label}: rollback recovery and installed unit retained', result)
            else:
                require(not backup.exists(), f'{label}: capacity rejected before snapshot', result)
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
        if case in ('success', 'absent_success', 'transition_success'):
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
            require((state / f'units/hololive-youtube-collector-{slot}.container').read_text() == previous_definition, f'{label}: previous base unit restored', result)
            require((release_dir / f'rollback-replaced-hololive-youtube-collector-{slot}.container').read_text() == installed_definition, f'{label}: replaced base unit retained', result)
            require(calls.index('systemctl daemon-reload') > -1, f'{label}: units reloaded', result)
        expected_runtime = 'absent' if endpoint_blocked else 'new' if is_check or case in ('success', 'absent_success', 'transition_success', 'rollback_cross', 'rollback_capacity_drift') else 'old'
        require((state / 'collector.runtime').read_text() == expected_runtime, f'{label}: collector runtime', result)
        expected_issuer = 'new' if case == 'restore_failure' else 'absent' if case == 'absent_candidate_check_failure' else expected_runtime
        require((state / 'issuer.runtime').read_text() == expected_issuer, f'{label}: issuer runtime', result)
        print(f'[PASS] {label}: exit={result.returncode}, collector={(state / "collector.runtime").read_text()}, issuer={(state / "issuer.runtime").read_text()}')
PY
