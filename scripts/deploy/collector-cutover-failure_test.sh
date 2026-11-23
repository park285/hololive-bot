#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

python3 - "$root" <<'PY'
import hashlib
import fcntl
import json
import socket
import threading
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path

root = Path(sys.argv[1])
native = (root / 'scripts/deploy/lib/ap-host-native-remote-apply.sh').read_text()
central = (root / 'scripts/deploy/lib/po-central-remote.sh').read_text()
slot_lines = (root / 'scripts/deploy/lib/collector-slots.tsv').read_text().splitlines()
slot_table = {row[0]: dict(zip(slot_lines[0].split('\t'), row)) for row in (line.split('\t') for line in slot_lines[1:])}


def slot_preamble(slot):
    return subprocess.run(['bash', '-c', '. "$1/scripts/deploy/lib/collector-slots.sh"; collector_slot_preamble "$2"', '_', str(root), slot],
                          check=True, capture_output=True, text=True).stdout


def container(slot, kind='collector'):
    return slot_table[slot][kind + '_unit'].removesuffix('.service')


def function(source, name):
    return re.search(r'^' + name + r'\(\) \{\n.*?^\}', source, re.M | re.S).group()


def run(body, state, case):
    result = subprocess.run(
        ['bash', '-s', '--', str(state), case, str(root)],
        input=body, capture_output=True, text=True, timeout=20,
        start_new_session=case.endswith('_group_TERM'),
    )
    return result


def require(condition, message, result):
    if not condition:
        raise AssertionError(f'{message}\nexit={result.returncode}\n{result.stdout}\n{result.stderr}')

proof_setup = r'''
set -euo pipefail
fixture_root="$1" fixture_case="$2" staging="$3" collector_container=collector
. "$staging/scripts/deploy/lib/postgres-endpoint.sh"
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
        writable_proof) printf 'off|t\n' ;;
        wrong_checksum) printf 'on|wrong\n' ;;
        *) printf 'on|t\n' ;;
      esac ;;
    ps) return 0 ;;
    *) return 2 ;;
  esac
}
'''
proof_setup += '\n' + function(central, 'collector_database_endpoint') + '\n'
proof_function = function(central, 'require_collector_lease_migration')
for case, endpoint, success, queries in (
    ('direct', ('holo-postgres', '5432', 'hololive'), True, True),
    ('retired_pooler', ('holo-pgbouncer', '6432', 'hololive'), False, False),
    ('foreign', ('other-postgres', '5432', 'hololive'), False, False),
    ('wrong_port', ('holo-postgres', '6432', 'hololive'), False, False),
    ('wrong_database', ('holo-postgres', '5432', 'other'), False, False),
    ('socket_override', ('holo-postgres', '5432', 'hololive'), False, False),
    ('ambiguous_host', ('holo-postgres', '5432', 'hololive'), False, False),
    ('query_failure', ('holo-postgres', '5432', 'hololive'), False, True),
    ('writable_proof', ('holo-postgres', '5432', 'hololive'), False, True),
    ('wrong_checksum', ('holo-postgres', '5432', 'hololive'), False, True),
):
    with tempfile.TemporaryDirectory(prefix='collector-proof-test-') as raw:
        state = Path(raw)
        host, port, database = endpoint
        (state / 'collector.env').write_text(
            f'POSTGRES_HOST={host}\nPOSTGRES_PORT={port}\nPOSTGRES_DB={database}\n'
        )
        if case in ('socket_override', 'ambiguous_host'):
            with (state / 'collector.env').open('a') as env_file:
                env_file.write('POSTGRES_SOCKET_PATH=/other\n' if case == 'socket_override' else 'POSTGRES_HOST=other\n')
        result = run(proof_setup + proof_function + '\nrequire_collector_lease_migration applied\n', state, case)
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
                ('--env', 'PGOPTIONS=-c default_transaction_read_only=on -c statement_timeout=30s -c lock_timeout=3s'),
                ('-v', 'ON_ERROR_STOP=1'), ('-f', '-'),
            ):
                require(any(args[i:i + 2] == [flag, value] for i in range(len(args) - 1)),
                        f'{case}: private read-only ledger transport {value}', result)
        print(f'[PASS] collector migration proof: {case}')

for slot, host, port, accepted in (
    ('c', 'holo-postgres', 5432, True), ('c', 'holo-pgbouncer', 6432, False),
    ('c', 'foreign', 5432, False), ('b', '100.100.1.8', 5433, True),
    ('b', '100.100.1.8', 6433, False), ('b', '100x100x1x8', 5433, False),
):
    with tempfile.TemporaryDirectory(prefix='collector-endpoint-test-') as raw:
        state = Path(raw)
        (state / 'collector.env').write_text(
            f'POSTGRES_HOST={host}\nPOSTGRES_PORT={port}\nPOSTGRES_DB=hololive\n'
        )
        result = run(proof_setup + f'\nslot={slot}\n' + function(central, 'require_collector_database_endpoint') +
                     '\nrequire_collector_database_endpoint\n', state, 'endpoint')
        require((result.returncode == 0) == accepted, 'post-activation database endpoint', result)

native_functions = '\n'.join(function(native, name) for name in (
    'stop_native_units_and_require_inactive',
    'native_restore_recorded_runtime', 'restore_native_after_failed_cutover', 'native_cutover_failed', 'arm_native_cutover_restore',
))
native_journal = native[native.index('native_post_cutover_log_check()'):]
native_host_env = slot_table['a']['host_env'].rsplit('/', 1)[1]
native_unit_template = slot_table['a']['collector_unit'].split('@')[0] + '@.service'
native_setup = slot_preamble('a') + r'''
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
    show)
      case "$4" in
        LoadState) printf 'loaded\n' ;;
        ActiveState) if [[ "$(<"$state/runtime")" == stopped ]]; then printf 'inactive\n'; else printf 'active\n'; fi ;;
        UnitFileState) printf 'disabled\n' ;;
        *) return 2 ;;
      esac ;;
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
        for name in (native_host_env, native_unit_template):
            (contract / name).write_text('old')
        old_env = 'POSTGRES_HOST=100.100.1.8\nPOSTGRES_PORT=' + ('6433' if case == 'endpoint_change' else '5433') + '\n'
        new_env = 'POSTGRES_HOST=100.100.1.8\nPOSTGRES_PORT=5433\n'
        (contract / native_host_env).write_text(old_env)
        (state / 'payload').mkdir()
        (state / 'payload' / native_host_env).write_text(new_env)
        (state / 'releases/new' / native_host_env).write_text(new_env)
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
            require((state / 'recovery').exists() and (contract / native_host_env).read_text() == old_env,
                    'cross-endpoint rollback preserves recovery guard and previous env', result)
            require(not any(call.startswith('systemctl enable') for call in calls), 'cross-endpoint rollback never restarts', result)
        elif case != 'success':
            require((state / 'host.env').read_text() == old_env and (state / 'unit').read_text() == 'old', f'native {case}: installed contract restored', result)
        print(f'[PASS] native {case}: exit={result.returncode}, current={expected_current}, previous={expected_previous}, runtime={expected_runtime}')


central_functions = '\n'.join(
    match.group() for match in re.finditer(r'^([a-z_]+)\(\) \{\n.*?^\}', central, re.M | re.S)
    if match.group(1) not in ('require_collector_lease_migration', 'restore_after_failed_cutover')
)
central_config = central[central.index('manifest="../'):central.index('[[ "$(stat')]
central_main = central[central.index('{\nclaim_slot\n'):]
central_source_gate = central[central.index('cd "$current"\n'):central.index('compose_env=')]
old_revision, new_revision = 'a' * 40, 'b' * 40
old_id, new_id = 'sha256:' + 'a' * 64, 'sha256:' + 'b' * 64
change = '20261003T000000Z'
central_setup = r'''
set -Eeuo pipefail
fixture_root="$1" fixture_case="$2"
if [[ "$slot" == b && "$fixture_case" == maintenance_quiesce ]]; then
  # 실제 b의 uutils date는 .018375123초를 %6N에서 .183751초로 렌더링한다.
  unset EPOCHREALTIME
  EPOCHREALTIME=1794975991.018375
  date() {
    if [[ "$*" == '-u +%Y-%m-%d %H:%M:%S.%6N UTC' ]]; then
      printf '2026-11-18 04:26:31.183751 UTC\n'
    else
      command date "$@"
    fi
  }
fi
# staged helper는 원격 본문과 같은 checksum gate 뒤에서만 읽는다.
compose_env="$fixture_root/compose.env"
remote_root="$fixture_root"
current="$fixture_root/current" staging="$fixture_root/staging" backup="$fixture_root/card-po-$slot-20261003T000000Z"
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
transport=-
if [[ -f "$fixture_root/staged-pair" ]]; then
  if [[ "$mode" != deploy ]]; then backup="$fixture_root/staged-po-b-20261003T000000Z"; fi
  if [[ "$mode" != deploy ]]; then transport="$(<"$fixture_root/trusted-seal")"; fi
elif [[ "$mode" == quiesce && -f "$fixture_root/trusted-seal" ]]; then
  mode=quiesce-verified transport="$(<"$fixture_root/trusted-seal")"
fi
set -- "$slot" "$mode" "$staging" "$backup" "$revision" "$version" "$transport"
if [[ "$backup" == "$fixture_root/staged-po-b-"* ]]; then
  read -r -a origin_fields < "$fixture_root/approved-origin"
  set -- "$@" "${origin_fields[@]}"
fi
ref_file() { printf '%s/images/%s\n' "$fixture_root" "${1//[\/:]/_}"; }
hex_of() { if [[ "$1" == old ]]; then printf '%064d\n' 0 | tr 0 a; else printf '%064d\n' 0 | tr 0 b; fi; }
revision_of() { if [[ "$1" == old ]]; then printf '%040d\n' 0 | tr 0 a; else printf '%040d\n' 0 | tr 0 b; fi; }
fixture_exit_status() {
  local unit="$1" cid="$2" since="$3" until="$EPOCHREALTIME" id at code found=''
  [[ "$since" =~ ^@[0-9]+\.[0-9]{6}$ && "$cid" =~ ^[0-9a-f]{64}$ &&
     -f "$fixture_root/$unit.died" && "$(<"$fixture_root/$unit.since")" == "$since" ]] || return 8
  if [[ "$slot" == b && "$fixture_case" == maintenance_quiesce ]]; then until=1794975991.020000; fi
  while read -r id at code; do
    [[ "$id" == "$cid" && ! "$at" < "${since#@}" && ! "$at" > "$until" ]] || continue
    [[ -z "$found" && "$code" =~ ^(0|[1-9][0-9]{0,2})$ && "$code" -le 255 ]] || return 8
    found="$code"
  done < "$fixture_root/$unit.died"
  [[ -n "$found" ]] && printf '%s\n' "$found"
}
sleep() {
  if [[ "$fixture_case" != *late_notify_client || "$1" != 0.25 || ! -e "$fixture_root/notify.pending" ]]; then
    command sleep "$@"; return $?
  fi
  if [[ ! -e "$fixture_root/notify.246s" ]]; then
    SECONDS=$((SECONDS + 246))
    : > "$fixture_root/notify.246s"
  else
    rm -- "$fixture_root/notify.pending"
    printf true > "$fixture_root/collector.running"
  fi
}
guard_cli() {
  local op="$1" unit="$2" anchor="$3" inode="$4" mask="$system_unit_dir/$2" guard="$system_unit_dir/$2.guard"
  printf 'guard %s %s\n' "$op" "$unit" >> "$fixture_root/calls"
  if [[ "$op" == exit-status ]]; then fixture_exit_status "$unit" "$3" "$4" || return 8; return 0; fi
  [[ "$(readlink "$anchor")" == /dev/null && "$(command stat -c '%d:%i' "$anchor")" == "$inode" ]] || return 8
  if [[ "$op" == arm && "$fixture_case" == maintenance_quiesce_mask_race && "$unit" == "$collector_unit" ]]; then
    command ln -s /dev/null "$mask" || return 8
  fi
  [[ ! -e "$mask" && ! -L "$mask" ]] || return 8
  case "$op" in
    arm)
      (set -C; printf '%s\n' "$anchor" > "$guard") || return 8
      [[ "$(systemctl show -p LoadState --value "$unit")" == loaded ]] || return 8
      systemctl daemon-reload || return 8
      if [[ "$unit" == "$collector_unit" && "$fixture_case" == *arm_collector_profile ]]; then
        printf 'other profile\n' > "$fixture_root/loaded.profile"
      fi
      [[ "$unit" != "$collector_unit" || "$fixture_case" != *guard_failure ]] ;;
    undo-unmask)
      [[ "$(systemctl show -p LoadState --value "$unit")" == masked && ! -e "$guard" ]] || return 8
      ln -PT -- "$anchor" "$mask" && systemctl daemon-reload || return 8 ;;
    start-budget)
      [[ -f "$guard" && "$(<"$guard")" == "$anchor" ]] || return 8
      if [[ "$unit" == "$collector_unit" ]]; then printf '300\n'; else printf '120\n'; fi ;;
    check|seal)
      [[ -f "$guard" && "$(<"$guard")" == "$anchor" ]] || return 8
      [[ "$unit" != "$collector_unit" || "$fixture_case" != *guard_check_failure ]] || return 8
      if [[ "$op" == seal ]]; then
        [[ "$(systemctl show -p ActiveState --value "$unit")" =~ ^(inactive|failed)$ &&
           "$(systemctl show -p MainPID --value "$unit")" == 0 && "$(systemctl show -p ControlPID --value "$unit")" == 0 ]] || return 8
        ln -PT -- "$anchor" "$mask" && systemctl daemon-reload && rm -- "$guard" || return 8
      fi ;;
    *) return 2 ;;
  esac
}
admitted() {
  local mask="$system_unit_dir/$1" guard="$system_unit_dir/$1.guard"
  [[ ! -L "$mask" ]] && [[ ! -f "$guard" || ! -e "$(<"$guard")" ]]
}
python3() {
  if [[ "${1-} ${2-} ${3-}" == "-I -B $staging/scripts/deploy/lib/systemd-stop-guard.py" ]]; then
    shift 3; guard_cli "$@"
  elif [[ "${1:-}" == "$pair_tool" ]]; then
    printf 'pair %s %s\n' "$2" "$4" >>"$fixture_root/calls"
    case "$2" in
      capture)
        mkdir -p "$4"; printf 'retained profile\n' > "$4/profile.json"
        cp "$fixture_root/collector.generation" "$4/pair.json"
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
      verify) test -f "$4/profile.json" ;;
      verify-live)
        if [[ "$fixture_case" == *rollback_replaced && "$4" == */previous ]]; then
          printf 9 > "$fixture_root/collector.cid"; printf '%032x\n' 3 > "$fixture_root/collector.invocation"; return 7
        fi
        [[ "$5" == "$collector_container" ]] && cmp -s "$4/profile.json" "$fixture_root/loaded.profile" ;;
      verify-captured)
        [[ "$5" == "$collector_container" ]] && cmp -s "$4/pair.json" "$fixture_root/collector.generation" &&
          cmp -s "$4/profile.json" "$fixture_root/loaded.profile" ;;
      check-database)
        [[ "$fixture_case" != *database_query_failure && "$3" == "$service" && "$4" == "$profile_release/candidate" ]] &&
          cmp -s "$4/unit.container" "$unit_dir/$collector_container.container" ;;
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
busctl() {
  local bus=org.freedesktop.systemd1
  [[ "$*" == "--system --timeout=45s --json=short get-property $bus $6 $bus.Service TimeoutStartUSec" ]] || return 2
  case "$6" in
    "$(systemd_unit_object "$collector_unit")") printf '{"type":"t","data":300000000}\n' ;;
    "$(systemd_unit_object "$issuer_unit")") printf '{"type":"t","data":120000000}\n' ;;
    *) return 2 ;;
  esac
}
sudo() { [[ "$1" != -n ]] || shift; "$@"; }
install() {
  local args=()
  while (($#)); do
    case "$1" in -o|-g) shift 2 ;; *) args+=("$1"); shift ;; esac
  done
  command install "${args[@]}"
}
rm() {
  command rm "$@" || return
  if [[ "$fixture_case" == *receipt_signal* && "$*" == "-- $hold/quiesced" ]]; then
    if [[ "$fixture_case" == *receipt_signal_changed ]]; then printf '137\n' > "$fixture_root/issuer.exit"; fi
    kill -TERM "$maintenance_owner_pid"
  fi
}
sync() {
  if [[ "$fixture_case" == *error_log_evidence_failure && "$2" == "$backup/guarded-stop.log" ]]; then return 8; fi
  if [[ "$fixture_case" == maintenance_resume_expired_late_notify_client &&
        "$2" == "$backup/resume-$collector_unit.start-request" && -f "$fixture_root/notify.pending" ]]; then
    unset EPOCHSECONDS; EPOCHSECONDS=$expires
  fi
  command sync "$@"
}
ln() {
  if [[ "${*: -1}" == "$system_unit_dir/"* ]]; then printf 'mask link %s\n' "${*: -1}" >>"$fixture_root/calls"; fi
  if [[ "$1" == -PT && "$4" == "$system_unit_dir/$collector_unit" ]]; then
    [[ "$fixture_case" != maintenance_quiesce_mask_failure && "$fixture_case" != *error_log_seal_failure ]] || return 8
  fi
  command ln "$@"
}
stat() {
  case "$2:${*: -1}" in
    '%d:%i:'*) command stat "$@" ;;
    '%u:%g:'*) printf '0:0\n' ;;
    *:"$fixture_root/socket") printf '65532:1000 770\n' ;;
    *) printf '0:0 %s\n' "$(command stat -c '%a' "${*: -1}")" ;;
  esac
}
find() {
  local arg
  local -a args=()
  while (( $# )); do
    arg="$1"; shift; args+=("$arg")
    case "$arg" in
      -uid) [[ "$1" == 0 ]] || return 2; args+=("$EUID"); shift ;;
      -gid) [[ "$1" == 0 ]] || return 2; args+=("$(id -g)"); shift ;;
    esac
  done
  command find "${args[@]}"
}
date() {
  if [[ "$*" == '-u +%Y-%m-%dT%H:%M:%SZ' ]]; then printf '2026-10-03T00:01:00Z\n'; else command date "$@"; fi
}
require_collector_lease_migration() {
  printf 'lease-proof %s\n' "$1" >>"$fixture_root/calls"
  [[ "$1" != target || ! -e "$fixture_root/pending-suffix" ]]
}
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
    'container exists')
      [[ ! -f "$fixture_root/inventory-unavailable" ]] || return 125
      if [[ -f "$fixture_root/staged-pair" ]]; then
        local cid
        for kind in collector issuer; do
          cid=3; [[ "$kind" != issuer ]] || cid=4
          if [[ -f "$fixture_root/$kind.cid" ]]; then cid="$(<"$fixture_root/$kind.cid")"; fi
          local name="$collector_container"
          [[ "$kind" != issuer ]] || name="$issuer_container"
          if [[ "$3" == "$name" || "$3" == "$(printf '%064d' "$cid")" ]]; then
            [[ "$(<"$fixture_root/$kind.running")" == true ]]; return
          fi
        done
        return 1
      fi
      if [[ "$fixture_case" == *stop_leftover_id && "$3" == "$(printf '%064d' 3)" ]]; then return 0; fi
      if [[ "$3" == "$(printf '%064d' 4)" && -e "$fixture_root/issuer.old-cid" ]]; then return 0; fi
      kind=collector
      [[ "$3" != "$issuer_container" && "$3" != "$(printf '%064d' 4)" ]] || kind=issuer
      [[ "$(cat "$fixture_root/$kind.running")" == true || -e "$fixture_root/$kind.leftover" ]]
      ;;
    'image rm') rm "$(ref_file "$3")" ;;
    'tag '*)
      if [[ "$fixture_case" == restore_failure && "$2" == *:rollback-* ]]; then return 9; fi
      cp "$(ref_file "$2")" "$(ref_file "$3")"
      ;;
    'load --input')
      kind=po-sandbox
      [[ "$3" != */collector-image.tar ]] || kind=collector
      local image="$issuer_candidate"
      [[ "$kind" != collector ]] || image="$collector_candidate"
      printf new > "$(ref_file "$image")"
      if [[ "$kind" == collector ]]; then
        case "$fixture_case" in
          *load_collector_replaced) printf '{"container_id":"%064d","pid":42}\n' 2 > "$fixture_root/collector.generation" ;;
          *load_collector_restarted) printf '{"container_id":"%064d","pid":43}\n' 1 > "$fixture_root/collector.generation" ;;
          *load_issuer_invocation) printf '%032x\n' 2 > "$fixture_root/issuer.invocation" ;;
          *load_issuer_image) printf new > "$fixture_root/issuer.runtime"; printf new > "$(ref_file "$issuer")" ;;
          *load_issuer_restarted) printf 2 > "$fixture_root/issuer.started" ;;
          *load_issuer_unhealthy) touch "$fixture_root/issuer.unhealthy" ;;
          *load_issuer_socket) touch "$fixture_root/issuer.socket-failed" ;;
        esac
      fi
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
            if [[ -f "$fixture_root/direct-origin" && "$value" == old || "$fixture_case" == transition_* && "$value" == old || "$fixture_case" == transition_wrong_endpoint ]]; then
              printf 'POSTGRES_HOST=holo-pgbouncer\nPOSTGRES_PORT=6432\nPOSTGRES_DB=hololive\n'
            else
              printf 'POSTGRES_HOST=holo-postgres\nPOSTGRES_PORT=5432\nPOSTGRES_DB=hololive\n'
            fi
          else
            if [[ -f "$fixture_root/direct-origin" && "$value" == old || "$fixture_case" == transition_* && "$value" == old || "$fixture_case" == transition_wrong_endpoint ]]; then
              printf 'POSTGRES_HOST=100.100.1.8\nPOSTGRES_PORT=6433\nPOSTGRES_DB=hololive\n'
            else
              printf 'POSTGRES_HOST=100.100.1.8\nPOSTGRES_PORT=5433\nPOSTGRES_DB=hololive\n'
            fi
          fi ;;
        *State.Health*)
          if [[ -e "$fixture_root/$kind.unhealthy" ]]; then printf 'unhealthy\n'; else printf 'healthy\n'; fi ;;
        *State.StartedAt.UnixNano*)
          local cid=3 conmon=42; [[ "$kind" != issuer ]] || cid=4
          if [[ -f "$fixture_root/$kind.cid" ]]; then cid="$(<"$fixture_root/$kind.cid")"; fi
          if [[ -f "$fixture_root/$kind.foreign-conmon" ]]; then conmon=99; fi
          printf '%064d %s %s 43 %d\n' "$cid" "$(hex_of "$value")" "$(<"$fixture_root/$kind.started")" "$conmon"
          if [[ "$fixture_case" == *capture_cid_changed && "$kind" == issuer ]]; then printf 9 > "$fixture_root/$kind.cid"; fi ;;
        *image.revision*) revision_of "$value" ;;
        '{{.Image}}') hex_of "$value" ;;
        *Mounts*) printf '%s/socket\n' "$fixture_root" ;;
        '{{.State.StartedAt.Unix}}') command date -u -d 2026-10-03T00:01:01Z +%s ;;
        *) return 2 ;;
      esac
      ;;
    'exec '*)
      if [[ "$2" == "$issuer_container" ]]; then
        [[ ! -e "$fixture_root/issuer.socket-failed" ]] || return 1
        if [[ "$fixture_case" == maintenance_resume_expire_after_issuer ]]; then unset EPOCHSECONDS; EPOCHSECONDS=$expires; fi
      else
        [[ "$2" == "$collector_container" && "$4" == --body ]] || return 2
        local instance="collector-$slot" handoff=PROCESSED processed=true
        if [[ "$fixture_case" == check_wrong_instance ]]; then instance=collector-a; fi
        if [[ "$fixture_case" == check_waiting_handoff ]]; then handoff=PENDING; processed=false; fi
        printf '{"status":"ready","instance_id":"%s","helper":"ok","first_success":true,"handoff_status":"%s","handoff_processed":%s,"pending_queue":0}\n' \
          "$instance" "$handoff" "$processed"
      fi ;;
    'logs --since')
      case "$fixture_case:${*: -1}" in
        *logs_collector*:"$collector_container") return 5 ;;
        *error_log_replaced:"$collector_container") printf 9 > "$fixture_root/collector.cid"; printf 'ERR replaced\n' ;;
        *error_log*:"$collector_container") printf 'ERR injected runtime error\n' ;;
        *) printf 'runtime ready\n' ;;
      esac
      ;;
    *) return 2 ;;
  esac
}
journalctl() {
  if [[ "${11-}" == JOB_TYPE=start ]]; then
    local unit="${9#UNIT=}" id invocation
    [[ "$8" == _PID=1 && "${12}" == JOB_RESULT=done && "${13}" == MESSAGE_ID=39f53479d3a045ac8e11786248231fbf ]] || return 2
    read -r id invocation < "$fixture_root/$unit.start-job" || return 8
    [[ "${10}" == "JOB_ID=$id" ]] || return 8
    printf '{"INVOCATION_ID":"%s"}\n' "$invocation"
    return 0
  fi
  local unit="${11#UNIT=}" invocation="${12#INVOCATION_ID=}" since="${9#--since=}" id at
  [[ "${10}" == _PID=1 ]] || return 2
  printf '%s\n' "$since" > "$fixture_root/$unit.since"
  since="$(command date -u -d "$since" +%s.%N)" || return 2
  since="${since%???}"
  [[ "$unit" == "$issuer_unit" || "$unit" == "$collector_unit" ]] || return 2
  [[ "${13}" == JOB_TYPE=stop && "${14}" == MESSAGE_ID=de5b426a63be47a7b6ac3eaac82e2f6f ]] || return 2
  if [[ -f "$fixture_root/$unit.stopping" ]] && read -r id at < "$fixture_root/$unit.stopping" &&
     [[ "$id" == "$invocation" && ! "$at" < "$since" ]]; then
    printf '{"INVOCATION_ID":"%s"}\n' "$id"
  fi
}
systemctl() {
  local kind=collector value
  printf 'systemctl %s\n' "$*" >>"$fixture_root/calls"
  [[ "${*: -1}" != "$issuer_unit" ]] || kind=issuer
  if [[ "$1" == show && "$2" == --all ]]; then
    local prop answer unit="${*: -1}"
    shift 2
    while [[ "$1" == -p ]]; do
      prop="$2"; shift 2
      answer="$(systemctl show -p "$prop" --value "$unit")" || return
      printf '%s=%s\n' "$prop" "$answer"
    done
    return 0
  fi
  case "$1" in
    start)
      admitted "$2" || return 8
      printf true > "$fixture_root/$kind.running" ;;
    daemon-reload)
      if [[ "$fixture_case" == *collector_143 && -L "$system_unit_dir/$collector_unit" ]]; then
        rm -f "$fixture_root/collector.exit"
      fi ;;
    show)
      if [[ "$2" == --property=Job && "$3" == --property=ActiveState ]]; then
        [[ "$fixture_case" != manager_probe_failure || "$cutover_started" != true ]] || return 8
        if [[ -f "$fixture_root/manager-job" ]] && [[ "$(cat "$fixture_root/manager-job")" == "${*: -1}" ]]; then
          printf 'Job=17\nActiveState=activating\n'
        elif [[ "$(cat "$fixture_root/$kind.running")" == true ]]; then
          printf 'Job=\nActiveState=active\n'
        else
          printf 'Job=\nActiveState=failed\n'
        fi
        return 0
      fi
      case "$3" in
        Job)
          [[ ! -e "$fixture_root/$kind.probe-failed" ]] || return 8
          if [[ "$kind" == collector && -e "$fixture_root/notify.pending" ]]; then printf '18\n'; else printf '\n'; fi ;;
        NRestarts)
          if [[ "$fixture_case" == *capture_restarted && "$kind" == issuer ]]; then printf '1\n'; else printf '0\n'; fi ;;
        LoadState)
          if [[ -L "$system_unit_dir/${*: -1}" || -e "$fixture_root/$kind.masked-cache" ]]; then printf 'masked\n'; else printf 'loaded\n'; fi ;;
        ActiveState)
          if [[ "$kind" == collector && -e "$fixture_root/notify.pending" ]]; then printf 'activating\n'; return 0; fi
          if [[ "$(cat "$fixture_root/$kind.running")" == true ]]; then printf 'active\n'
          elif [[ -f "$fixture_root/$kind.exit" ]]; then printf 'failed\n'; else printf 'inactive\n'; fi ;;
        InvocationID)
          cat "$fixture_root/$kind.invocation" ;;
        Result) if [[ -f "$fixture_root/$kind.exit" ]]; then printf 'exit-code\n'; else printf 'success\n'; fi ;;
        ExecMainCode) if [[ -s "$fixture_root/$kind.invocation" ]]; then printf '1\n'; else printf '0\n'; fi ;;
        ExecMainStatus) if [[ -f "$fixture_root/$kind.exit" ]]; then cat "$fixture_root/$kind.exit"; else printf '0\n'; fi ;;
        MainPID|ControlPID)
          if [[ "$3" == ControlPID && "$fixture_case" == *stop_control_pid && "$kind" == collector ]]; then printf '42\n'
          elif [[ "$3" == MainPID && "$(<"$fixture_root/$kind.running")" == true ]]; then printf '42\n'
          else printf '0\n'; fi ;;
        *) return 2 ;;
      esac ;;
    unmask)
      [[ "$fixture_case" != *unmask_failure || "$kind" != collector ]] || return 8
      if [[ "$fixture_case" == *peer_probe_failure && "$kind" == collector ]]; then
        touch "$fixture_root/$kind.probe-failed"; return 8
      fi
      rm "$system_unit_dir/$2" || return 8
      if [[ "$fixture_case" == maintenance_resume_expire_after_unmask && "$kind" == issuer ]]; then unset EPOCHSECONDS; EPOCHSECONDS=$expires; fi
      if [[ "$fixture_case" == *unmask_reload* && "$kind" == issuer ]]; then
        touch "$fixture_root/$kind.masked-cache"
        if [[ "$fixture_case" == *unmask_reload_old_cid ]]; then touch "$fixture_root/issuer.old-cid"; fi
        return 8
      fi ;;
    is-active) [[ "$(cat "$fixture_root/$kind.running")" == true ]] ;;

    restart|--show-transaction)
      if [[ "$1" == --show-transaction ]]; then
        shift; [[ "$1" == start ]] || return 2
        local job=17; [[ "$kind" == issuer ]] || job=18
        printf 'Enqueued anchor job %s %s/start.\n' "$job" "$2" >&2
      fi
      admitted "$2" || return 8
      rm -f "$fixture_root/$kind.exit"
      printf '%032x\n' 2 > "$fixture_root/$kind.invocation"
      if [[ -f "$fixture_root/staged-pair" ]]; then
        local cid=5 invocation=2
        if [[ "$mode" == rollback ]]; then cid=7 invocation=3; fi
        [[ "$kind" != issuer ]] || cid=$((cid+1))
        if [[ "$fixture_case" == *old_start_cid && "$kind" == collector ]]; then cid=3; fi
        printf '%d' "$cid" > "$fixture_root/$kind.cid"
        printf '%032x\n' "$invocation" > "$fixture_root/$kind.invocation"
      fi
      if [[ "$fixture_case" == maintenance_* ]]; then
        [[ ! -e "$fixture_root/db.stopped" ]] || return 9
        test -f "$profile_release/candidate/profile.json" || [[ "$mode" == rollback ]] || return 9
        test -f "$current/$manifest" && test -f "$current/$image_id_file" || return 9
        if [[ "$mode" == resume ]]; then
          [[ "$(<"$current/$image_id_file")" == "sha256:$(hex_of new)" ]] || return 9
        fi
        [[ "$(cat "$fixture_root/dropin")" == candidate || "$mode" == rollback ]] || return 9
      fi
      if [[ "$fixture_case" == client_lost_pending && "$kind" == issuer && ! -f "$fixture_root/manager-job" ]]; then
        local client_status=0
        command python3 "$fixture_root/manager-client.py" "$fixture_root/manager.sock" "$issuer_unit" || client_status=$?
        return "$client_status"
      fi
      # 재시작은 unit의 Image=를 다시 읽는다: collector는 drop-in이 고정한 pair, issuer는 :prod tag다.
      if [[ "$kind" == collector ]]; then
        case "$(cat "$fixture_root/dropin")" in
          candidate) value=new ;;
          previous) value=old ;;
          *) value="$(cat "$(ref_file "$collector")")" ;;
        esac
        cp "$profile_release/$(cat "$fixture_root/dropin")/profile.json" "$fixture_root/loaded.profile"
        if [[ "$fixture_case" == *rollback_profile ]]; then printf 'other profile\n' > "$fixture_root/loaded.profile"; fi
        if [[ "$fixture_case" == *rollback_unhealthy ]]; then touch "$fixture_root/collector.unhealthy"; fi
        if [[ "$fixture_case" == *rollback_image ]]; then value=new; fi
      else
        value="$(cat "$(ref_file "$issuer")")"
      fi
      printf '%s' "$value" > "$fixture_root/$kind.runtime"
      if [[ "$fixture_case" == *substitution_failure || "$fixture_case" == restore_failure || "$fixture_case" == manager_probe_failure ]] && [[ "$kind" == issuer && "$value" == new ]]; then
        printf false > "$fixture_root/$kind.running"
        printf '143\n' > "$fixture_root/issuer.exit"
        return 1
      fi
      printf true > "$fixture_root/$kind.running"
      if [[ "$fixture_case" == *rollback_issuer_failure && "$kind" == issuer ]]; then touch "$fixture_root/issuer.socket-failed"; fi
      if [[ "$1" == start ]]; then
        if [[ "$fixture_case" == *missing_start_record && "$kind" == collector ]]; then return 0; fi
        printf '%s %s\n' "$job" "$(<"$fixture_root/$kind.invocation")" > "$fixture_root/$2.start-job"
        if [[ "$kind" == collector && "$fixture_case" == *late_notify_client ]]; then
          : > "$fixture_root/notify.pending"; printf false > "$fixture_root/collector.running"; return 9
        fi
        if [[ "$kind" == issuer ]]; then
          case "$fixture_case" in
            *capture_inv_changed) printf '%032x\n' 3 > "$fixture_root/$kind.invocation" ;;
            *client_unknown) return 9 ;;
            *foreign_named_cid) printf 9 > "$fixture_root/$kind.cid"; : > "$fixture_root/$kind.foreign-conmon"; return 9 ;;
            maintenance_resume_signal_group_TERM) kill -s TERM -- "-$maintenance_owner_pid" ;;
            maintenance_resume_signal_*) kill -s "${fixture_case##*_}" "$maintenance_owner_pid" ;;
          esac
        fi
      fi
      if [[ "$fixture_case" == signal_* && "$kind" == collector && "$value" == new ]]; then
        kill -s "${fixture_case#signal_}" "$cutover_restore_owner_pid"
      fi
      ;;
    stop)
      if [[ "$fixture_case" == maintenance_* || "$fixture_case" == transition_* ]]; then
        [[ ! -L "$system_unit_dir/$2" && -f "$system_unit_dir/$2.guard" ]] || return 8
        if [[ "$mode" == quiesce ]] && { admitted "$issuer_unit" || admitted "$collector_unit"; }; then return 8; fi
      fi
      [[ "$fixture_case" != *stop_failure || "$kind" != collector ]] || return 8
      if [[ "$fixture_case" == *stop_peer_restarted && "$kind" == issuer ]]; then
        printf 2 > "$fixture_root/collector.started"
      fi
      if [[ "$fixture_case" == *stop_other_invocation && "$kind" == collector ]]; then
        printf '%032x\n' 2 > "$fixture_root/$kind.invocation"
      fi
      local at="$EPOCHREALTIME" cid=3 code=0
      if [[ "$slot" == b && "$fixture_case" == maintenance_quiesce ]]; then at=1794975991.019117; fi
      if [[ "$fixture_case" != *stop_missing_job || "$kind" != issuer ]]; then
        if [[ "$fixture_case" == *stop_stale_job && "$kind" == issuer ]]; then at=0; fi
        printf '%s %s\n' "$(<"$fixture_root/$kind.invocation")" "$at" > "$fixture_root/$2.stopping"
      fi
      if [[ "$kind" == issuer && "$mode" == quiesce ]]; then printf '143\n' > "$fixture_root/issuer.exit"; fi
      if [[ "$fixture_case" == *collector_143 && "$kind" == collector ]]; then printf '143\n' > "$fixture_root/collector.exit"; fi
      if [[ "$kind" == issuer ]]; then cid=4; fi
      if [[ -f "$fixture_root/staged-pair" && -f "$fixture_root/$kind.cid" ]]; then cid="$(<"$fixture_root/$kind.cid")"; fi
      if [[ -f "$fixture_root/$kind.exit" ]]; then code="$(<"$fixture_root/$kind.exit")"; fi
      if [[ "$kind" == collector ]]; then
        case "$fixture_case" in
          *died_stale) at=0 ;;
          *died_other_cid) cid=9 ;;
          *died_duplicate) printf '%064d %s %s\n' "$cid" "$at" "$code" >> "$fixture_root/$2.died" ;;
          *collected_143) code=143 ;;
        esac
      fi
      printf '%064d %s %s\n' "$cid" "$at" "$code" >> "$fixture_root/$2.died"
      if [[ "$fixture_case" == *stop_terminal_invocation && "$kind" == collector ]]; then
        printf '%032x\n' 2 > "$fixture_root/$kind.invocation"
      elif [[ ! -f "$fixture_root/$kind.exit" ]]; then
        : > "$fixture_root/$kind.invocation"
      fi
      printf absent > "$fixture_root/$kind.runtime"; printf false > "$fixture_root/$kind.running"
      if [[ "$kind" == issuer && "$fixture_case" == maintenance_quiesce_expire_after_stop ]]; then unset EPOCHSECONDS; EPOCHSECONDS=$expires; fi ;;
    *) return 2 ;;
  esac
}
if [[ "$fixture_case" == *grep_failure ]]; then grep() { if [[ "$*" == *'ERR|panic'* ]]; then return 2; else command grep "$@"; fi; }; fi
'''
central_setup += central[central.index('[[ $#'):central.index('[[ "${slot_slot:-}"')]
for slot in ('c', 'b'):
    remote = slot_table[slot]['root']
    for prefix, staged_at, accepted in (('card-po', change, True), ('po', change, False), ('card-po', '20261002T000000Z', False)):
        args = [slot, 'resume', f'{remote}/po-staging-{staged_at}', f'{remote}/backups/{prefix}-{slot}-{change}',
                new_revision, '6.0.1', '-']
        result = run(slot_preamble(slot) + 'set -- ' + shlex.join(args) + '\n' + central[:central.index('install -d -m 0755 /run/lock')], root, 'paths')
        require((result.returncode == 0) == accepted, f'{slot}: W2 resume requires its card backup and staging', result)

with tempfile.TemporaryDirectory(prefix='b-staged-cli-') as raw:
    state = Path(raw)
    tool = state / 'coordinator'
    candidate = state / 'candidate'
    for name in ('scripts/deploy/po-central-cutover.sh', 'scripts/deploy/lib/source-revision.sh', 'scripts/deploy/lib/po-central-remote.sh',
                 'scripts/deploy/lib/collector-slots.sh', 'scripts/deploy/lib/collector-slots.tsv', 'scripts/deploy/lib/migration-receipts.sh'):
        (tool / name).parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(root / name, tool / name)
    version_file = candidate / 'hololive/hololive-api/VERSION'
    version_file.parent.mkdir(parents=True)
    version_file.write_text('19.2.1\n')
    source_name = 'hololive/hololive-api/VERSION'
    (candidate / 'scripts/deploy').mkdir(parents=True)
    (candidate / 'scripts/deploy/po-central-files.txt').write_text(source_name + '\n')
    migrations = candidate / 'hololive/hololive-api/scripts/migrations'
    (migrations / 'contracts').mkdir(parents=True)
    (migrations / 'manifest.txt').write_text('1 001_schema_epoch4_baseline.sql\n2 002_epoch4_fixture.sql\n')
    for name in ('001_schema_epoch4_baseline.sql', '002_epoch4_fixture.sql', 'contracts/002_epoch4_fixture.sql'):
        (migrations / name).write_text(f'SELECT {name!r};\n')
    migration_sources = [f'hololive/hololive-api/scripts/migrations/{n}' for n in ('002_epoch4_fixture.sql', 'contracts/002_epoch4_fixture.sql')]
    shutil.copytree(tool, state / 'objects/coordinator')
    shutil.copytree(candidate, state / 'objects/candidate')
    archive = state / 'arm64'
    archive.mkdir()
    def sums(directory, names):
        return ''.join(f'{hashlib.sha256((directory / n).read_bytes()).hexdigest()}  {n}\n' for n in names)
    (archive / 'source-files.sha256').write_text(sums(candidate, [source_name] + migration_sources))
    for prefix, meta in (('', 'rootfs-manifest.json'), ('collector-', 'collector-manifest.json')):
        name = prefix + 'image.tar'
        (archive / name).write_bytes(name.encode())
        (archive / (name + '.sha256')).write_text(sums(archive, [name]))
        (archive / (prefix + 'image-id')).write_text(new_id + '\n')
        (archive / meta).write_text(json.dumps({'source_revision': new_revision, 'architecture': 'arm64', 'go': {'goarch': 'arm64'}}))
    names = ('source-files.sha256', 'rootfs-manifest.json', 'collector-manifest.json', 'image-id', 'collector-image-id',
             'image.tar.sha256', 'collector-image.tar.sha256')
    (archive / 'transport-files.sha256').write_text(sums(archive, names))
    seal = hashlib.sha256((archive / 'transport-files.sha256').read_bytes()).hexdigest()
    tool_revision = '1'.zfill(40)
    expiry = int(time.time()) + 3600
    window, origin_digest = '1' * 32, '2' * 64
    w2 = state / 'w2'
    w2.mkdir()
    w2_issuer = 'sha256:' + '9' * 64
    (w2 / 'collector-image-id').write_text(old_id + '\n')
    (w2 / 'image-id').write_text(w2_issuer + '\n')
    (w2 / 'collector-manifest.json').write_text(json.dumps({'source_revision': old_revision, 'go': {'goarch': 'arm64'}}))
    (w2 / 'rootfs-manifest.json').write_text(json.dumps({'source_revision': old_revision, 'architecture': 'arm64'}))
    def grant(mode, grant_window=window, issuer=w2_issuer, tool_id=tool_revision, stage=change, expires=expiry):
        tmp = state / 'grant.tmp'
        tmp.write_text(f'PO_B_PAIR_V1 iris-seoul {grant_window} {mode} {stage} {tool_id} {new_revision} {seal} '
                       f'{old_revision} {old_id} {issuer} {origin_digest} {expires}\n')
        tmp.chmod(0o600)
        tmp.replace(state / 'b.current-grant')
    (state / 'b.window-clear').write_text(f'IRIS_WINDOW_CLEAR {new_revision}\n')
    (state / 'c.accepted').write_text(f'{new_revision} {seal}\n')
    (state / 'c.accepted').chmod(0o600)
    entry = r'''
set -eu
state="$1"
hostname() { printf 'kapu\n'; }
git() {
  local repo="$PWD"
  if [[ "$1" == -C ]]; then repo="$2"; shift 2; fi
  case "$repo" in "$state/coordinator"*) repo=coordinator ;; "$state/candidate"*) repo=candidate ;; *) return 2 ;; esac
  local rev
  if [[ "$repo" == coordinator ]]; then rev="$(printf '%040d' 1)"; else rev="$(printf '%040d' 0 | tr 0 b)"; fi
  case "$1" in
    status) [[ ! -f "$state/dirty" ]] || printf ' M caller\n' ;;
    rev-parse)
      if [[ "$2" == --show-toplevel ]]; then printf '%s/%s\n' "$state" "$repo"; else printf '%s\n' "$rev"; fi ;;
    cat-file)
      [[ "$2" == blob && "$3" == "$rev:"* ]] || return 2
      cat -- "$state/objects/$repo/${3#*:}" ;;
    *) return 2 ;;
  esac
}
ssh() {
  printf '%s\n' "$*" >> "$state/calls"
  cmp -s - <(bash -c '. "$1/scripts/deploy/lib/collector-slots.sh"; collector_slot_preamble b; cat -- "$1/scripts/deploy/lib/po-central-remote.sh"' _ "$state/objects/coordinator") || return 2
  [[ ! -f "$state/lost-reply" ]] || return 255
}
rsync() { printf 'unexpected rsync\n' >> "$state/calls"; return 7; }
podman() { printf 'unexpected build\n' >> "$state/calls"; return 7; }
export -f hostname git ssh rsync podman
export state PO_B_SSH_TARGET=iris-seoul APPROVE_PO_B_DEPLOY="${deploy_approval:-true}" APPROVE_PO_B_ROLLBACK="${rollback_approval:-false}"
bash "${tool_root:-$state/coordinator}/scripts/deploy/po-central-cutover.sh" b "$2" "${product_root:-$state/candidate}" "${receipt_arg:-$state}" "${stage_id:-20261003T000000Z}" \
  "${window_id:-11111111111111111111111111111111}" "$state/w2"
'''
    stage_receipt = state / f'b.stage.{change}.finished'
    stage_receipt.write_text(f'{new_revision} {seal} {change}\n')
    stage_receipt.chmod(0o600)
    for reason in ('legacy-clear', 'old-window', 'expired', 'wrong-phase', 'wrong-tool', 'wrong-W2-payload'):
        if reason == 'old-window':
            grant('quiesce', '3' * 32)
        elif reason == 'expired':
            grant('quiesce', expires=1)
        elif reason == 'wrong-phase':
            grant('resume')
        elif reason == 'wrong-tool':
            grant('quiesce', tool_id='e' * 40)
        elif reason == 'wrong-W2-payload':
            grant('quiesce', issuer=old_id)
        refused = run(entry, state, 'quiesce')
        require(refused.returncode != 0 and not (state / 'calls').exists(),
                f'b: {reason} has no SSH authority', refused)
    grant('quiesce')
    for label, path, variable in (('tool', tool, 'tool_root'), ('product', candidate, 'product_root')):
        nested = path / 'tmp/ignored'
        shutil.copytree(state / 'objects' / path.name, nested)
        refused = run(f'{variable}={shlex.quote(str(nested))}\n' + entry, state, 'quiesce')
        require(refused.returncode != 0 and not (state / 'calls').exists(), f'b: ignored nested {label} is not the checkout', refused)
    for path in (tool / 'scripts/deploy/po-central-cutover.sh', tool / 'scripts/deploy/lib/source-revision.sh', version_file,
                 candidate / 'scripts/deploy/po-central-files.txt'):
        original = path.read_bytes()
        path.write_bytes(original + b'\n')
        refused = run(entry, state, 'quiesce')
        require(refused.returncode != 0 and not (state / 'calls').exists(), f'b: diverging Git blob rejected: {path.name}', refused)
        path.write_bytes(original)
    stage_receipt.write_text(f'{new_revision} {"d" * 64} {change}\n')
    refused = run(entry, state, 'quiesce')
    require(refused.returncode != 0 and not (state / 'calls').exists(), 'b: foreign archive seal refused locally', refused)
    stage_receipt.write_text(f'{new_revision} {seal} {change}\n')
    (archive / 'image.tar').write_bytes(b'unaccepted archive')
    refused = run(entry, state, 'quiesce')
    require(refused.returncode != 0 and not (state / 'calls').exists(), 'b: locally drifted archive refused before SSH', refused)
    (archive / 'image.tar').write_bytes(b'image.tar')
    good_issuer_sum, good_transport, good_seal = (archive / 'image.tar.sha256').read_text(), (archive / 'transport-files.sha256').read_text(), seal
    (archive / 'image.tar.sha256').write_text(good_issuer_sum.replace('  image.tar', '  ./image.tar'))
    (archive / 'transport-files.sha256').write_text(sums(archive, names))
    seal = hashlib.sha256((archive / 'transport-files.sha256').read_bytes()).hexdigest()
    stage_receipt.write_text(f'{new_revision} {seal} {change}\n')
    grant('quiesce')
    for mode in ('preflight', 'quiesce'):
        refused = run('deploy_approval=false\n' + entry if mode == 'preflight' else entry, state, mode)
        require(refused.returncode != 0 and not (state / 'calls').exists() and (state / 'b.current-grant').exists(),
                f'b: {mode} refuses a checksum line the remote rejects before consuming the grant', refused)
    (archive / 'image.tar.sha256').write_text(good_issuer_sum)
    (archive / 'transport-files.sha256').write_text(good_transport)
    seal = good_seal
    stage_receipt.write_text(f'{new_revision} {seal} {change}\n')
    grant('quiesce')
    before = sorted(p.name for p in state.iterdir())
    checked = run('deploy_approval=false\n' + entry, state, 'preflight')
    require(checked.returncode == 0 and not (state / 'calls').exists() and sorted(p.name for p in state.iterdir()) == before
            and (state / 'b.current-grant').stat().st_nlink == 1, 'b: preflight runs the quiesce checks without SSH or grant consumption', checked)
    refused = run(f'receipt_arg={shlex.quote(str(state))}/\ndeploy_approval=false\n' + entry, state, 'preflight')
    require(refused.returncode != 0 and not (state / 'calls').exists(), 'b: preflight refuses the receipt path that quiesce refuses', refused)
    with open(state / 'receipts.lock', 'a') as held:
        fcntl.flock(held, fcntl.LOCK_EX)
        refused = run(entry, state, 'quiesce')
    require(refused.returncode != 0 and not (state / 'calls').exists() and (state / 'b.current-grant').exists()
            and not (state / f'b.quiesce.{change}.requested').exists(), 'b: grant consumption refuses a concurrent publication', refused)
    replaced = run('flock() { python3 -c "import sys; p = sys.argv[1]; open(p + \'.new\', \'w\').write(open(p).read().replace(\' quiesce \', \' quiesce \').rsplit(\' \', 1)[0] + \' 9999999999\\n\')" "$state/b.current-grant" && chmod 600 "$state/b.current-grant.new" && mv -f "$state/b.current-grant.new" "$state/b.current-grant"; }\nexport -f flock\n' + entry, state, 'quiesce')
    require(replaced.returncode != 0 and not (state / 'calls').exists() and (state / 'b.current-grant').stat().st_nlink == 1
            and not (state / f'b.quiesce.{change}.requested').exists() and not (state / f'b.{window}.quiesce.used').exists(),
            'b: a grant replaced before the lock is revalidated and left unconsumed', replaced)
    grant('quiesce')
    (archive / 'elsewhere').mkdir()
    shutil.copyfile(archive / 'image.tar', archive / 'elsewhere/image.tar')
    (archive / 'image.tar').unlink()
    (archive / 'image.tar').symlink_to(archive / 'elsewhere/image.tar')
    for mode in ('preflight', 'quiesce'):
        refused = run(entry, state, mode)
        require(refused.returncode != 0 and not (state / 'calls').exists() and (state / 'b.current-grant').exists(),
                f'b: {mode} refuses a symlinked archive that resume would rehash', refused)
    (archive / 'image.tar').unlink()
    shutil.move(archive / 'elsewhere/image.tar', archive / 'image.tar')
    (archive / 'elsewhere').rmdir()
    lost_window, lost_change = 'f' * 32, '20261002T000000Z'
    lost_entry = f'window_id={lost_window}\nstage_id={lost_change}\n' + entry
    lost_stage = state / f'b.stage.{lost_change}.finished'
    lost_stage.write_text(f'{new_revision} {seal} {lost_change}\n')
    lost_stage.chmod(0o600)
    (state / 'lost-reply').touch()
    grant('quiesce', lost_window, stage=lost_change)
    unknown = run(lost_entry, state, 'quiesce')
    require(unknown.returncode == 255 and (state / 'calls').exists() and
            not (state / f'b.{lost_window}.quiesce.finished').exists(), 'b: no completed phase on lost transport', unknown)
    (state / 'calls').unlink()
    (state / 'lost-reply').unlink()
    for mode in ('preflight', 'quiesce', 'resume'):
        grant('quiesce' if mode == 'preflight' else mode, lost_window, stage=lost_change)
        refused = run(lost_entry, state, mode)
        require(refused.returncode != 0 and not (state / 'calls').exists(), f'b: unknown outcome permits neither replay nor resume ({mode})', refused)
    for mode in ('preflight', 'quiesce'):
        grant('quiesce', stage=lost_change)
        refused = run(f'stage_id={lost_change}\n' + entry, state, mode)
        require(refused.returncode != 0 and not (state / 'calls').exists(), f'b: a different window cannot retry unknown stage effects ({mode})', refused)
    accepted_pair = (state / 'c.accepted').read_bytes()
    grant('resume')
    (state / 'c.accepted').unlink()
    refused = run(entry, state, 'resume')
    require(refused.returncode != 0 and not (state / 'calls').exists(), 'b: resume requires the accepted c pair', refused)
    for mode in ('quiesce', 'resume'):
        if mode == 'resume':
            (state / 'c.accepted').write_bytes(accepted_pair)
            (state / 'c.accepted').chmod(0o600)
        grant(mode)
        result = run(entry, state, mode)
        expected = (f'iris-seoul sudo -n bash -s -- b {mode} {slot_table["b"]["root"]}/po-staging-{change} '
                    f'{slot_table["b"]["root"]}/backups/staged-po-b-{change} {new_revision} 19.2.1 {seal} '
                    f'{window} {old_revision} {old_id} {w2_issuer} {origin_digest} {expiry} {tool_revision}')
        require(result.returncode == 0 and (state / 'calls').read_text().splitlines() == [expected],
                'b: coordinator revision stays distinct; only already-staged accepted candidate addresses the writer', result)
        (state / 'calls').unlink()
        require(not (state / 'b.current-grant').exists(), 'b: grant slot vacated for independent no-replace issuance', result)
        grant(mode)
        replay = run(entry, state, mode)
        require(replay.returncode != 0 and not (state / 'calls').exists(), 'b: consumed phase grant cannot be replayed', replay)
    checked = run('deploy_approval=false\n' + entry, state, 'check')
    require(checked.returncode == 0 and (state / 'calls').read_text().splitlines() ==
            [expected.replace(' b resume ', ' b check ')], 'b: pinned completion takes no writer grant', checked)
    (state / 'calls').unlink()
    grant('rollback')
    refused = run(entry, state, 'rollback')
    require(refused.returncode != 0 and not (state / 'calls').exists(), 'b: rollback requires its own approval', refused)
    for op in ('check', 'rollback'):
        path = tool / 'scripts/deploy/lib/po-central-remote.sh'
        original = path.read_bytes()
        path.write_bytes(original + b'\n')
        refused = run('rollback_approval=true\n' + entry, state, op)
        require(refused.returncode != 0 and not (state / 'calls').exists(), f'b: {op} refuses changed remote tool', refused)
        path.write_bytes(original)
    authorized = run('rollback_approval=true\ndeploy_approval=false\n' + entry, state, 'rollback')
    require(authorized.returncode == 0 and (state / 'calls').read_text().splitlines() ==
            [expected.replace(' b resume ', ' b rollback ')], 'b: original tool executes separately granted rollback', authorized)
    (state / 'calls').unlink()
    for name in ('scripts/deploy/po-central-cutover.sh', 'scripts/deploy/lib/source-revision.sh', 'scripts/deploy/lib/po-central-remote.sh',
                 'scripts/deploy/lib/collector-slots.sh', 'scripts/deploy/lib/collector-slots.tsv', 'scripts/deploy/lib/migration-receipts.sh'):
        (candidate / name).parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(root / name, candidate / name)
    verify = entry[:entry.index('bash "${tool_root')] + 'bash "$state/candidate/scripts/deploy/po-central-cutover.sh" c verify-pair "$state/arm64" "$2"\n'
    for label, damaged, pair_seal in (('verified pair', None, seal), ('foreign seal', None, 'd' * 64),
                                      ('truncated issuer archive', 'image.tar', seal), ('missing collector archive', 'collector-image.tar', seal),
                                      ('symlinked issuer archive', 'image.tar', seal), ('symlinked collector manifest', 'collector-manifest.json', seal)):
        original = (archive / damaged).read_bytes() if damaged else None
        if label.startswith('symlinked'):
            elsewhere = state / 'elsewhere' / damaged
            elsewhere.parent.mkdir(exist_ok=True)
            elsewhere.write_bytes(original)
            (archive / damaged).unlink()
            (archive / damaged).symlink_to(elsewhere)
        elif damaged == 'image.tar':
            (archive / damaged).write_bytes(original[:-1])
        elif damaged:
            (archive / damaged).unlink()
        result = run(verify, state, pair_seal)
        require((result.returncode == 0) == (label == 'verified pair') and not (state / 'calls').exists(),
                f'c: {label} is judged before any stop without SSH', result)
        if damaged:
            (archive / damaged).unlink(missing_ok=True)
            (archive / damaged).write_bytes(original)
    good_version = version_file.read_text()
    good_sources, good_transport = (archive / 'source-files.sha256').read_text(), (archive / 'transport-files.sha256').read_text()
    version_file.write_text('19.2.1+meta\n')
    (archive / 'source-files.sha256').write_text(sums(candidate, [source_name] + migration_sources))
    (archive / 'transport-files.sha256').write_text(sums(archive, names))
    result = run(verify, state, hashlib.sha256((archive / 'transport-files.sha256').read_bytes()).hexdigest())
    require(result.returncode != 0 and not (state / 'calls').exists(), 'c: a VERSION that quiesce rejects is judged before any stop', result)
    version_file.write_text(good_version)
    (archive / 'source-files.sha256').write_text(good_sources)
    (archive / 'transport-files.sha256').write_text(good_transport)
    good_issuer_sum = (archive / 'image.tar.sha256').read_text()
    (archive / 'image.tar.sha256').write_text(good_issuer_sum.replace('  image.tar', '  ./image.tar'))
    (archive / 'transport-files.sha256').write_text(sums(archive, names))
    result = run(verify, state, hashlib.sha256((archive / 'transport-files.sha256').read_bytes()).hexdigest())
    require(result.returncode != 0 and not (state / 'calls').exists(), 'c: a checksum line the remote rejects is judged before any stop', result)
    (archive / 'image.tar.sha256').write_text(good_issuer_sum)
    (archive / 'transport-files.sha256').write_text(good_transport)


def manifest(revision, identity):
    return json.dumps({'schema_version': 1, 'source_revision': revision, 'go': {'goarch': 'arm64'}, 'architecture': 'arm64', 'image_id': identity})


def image_file(state, ref):
    return state / 'images' / ref.replace('/', '_').replace(':', '_')


collector_image, issuer_image = slot_table['c']['collector_image'], slot_table['c']['issuer_image']
collector_prod, issuer_prod = f'{collector_image}:prod', f'{issuer_image}:prod'
collector_rollback = f'{collector_image}:rollback-{change}'
issuer_rollback = f'{issuer_image}:rollback-{change}'
held_start = 'systemctl --show-transaction start '
mutations = ('podman tag ', 'podman load ', 'podman image rm ', 'systemctl restart ', held_start, 'systemctl stop ', 'mask link ', 'guard arm ', 'guard seal ', 'guard undo-unmask ', 'systemctl unmask ', 'systemctl daemon-reload', 'pair activate ')
central_cases = {
    'success': (0, False), 'absent_success': (0, False),
    'substitution_failure': (1, True), 'deploy_logs_collector': (5, True), 'deploy_error_log': (1, True),
    'deploy_grep_failure': (2, True), 'signal_TERM': (143, True), 'signal_HUP': (129, True),
    'client_lost_pending': (137, False), 'manager_probe_failure': (1, False),
    'restore_failure': (1, False), 'activation_failure': (7, False),
    'candidate_check_failure': (7, False), 'absent_candidate_check_failure': (7, False),
    'missing_receipt': (1, False), 'collector_stopped': (1, False), 'inventory_failure': (1, False),
    'rollback': (0, True), 'check_success': (0, False), 'check_error_log': (1, False),
    'transition_success': (0, False), 'transition_error_log': (1, False),
    'check_wrong_profile': (1, False), 'database_query_failure': (1, False),
    'check_wrong_instance': (1, False), 'check_waiting_handoff': (1, False),
    'rollback_masked_endpoint': (1, False), 'rollback_missing_endpoint': (1, False),
    'transition_wrong_endpoint': (1, False), 'rollback_cross': (1, False),
    'capacity_exhausted': (1, False),
    'rollback_capacity_exhausted': (1, False),
    'unadmitted_deploy': (1, False), 'foreign_card_pair': (1, False), 'rollback_stale': (1, False),
}
slots = {slot: slot_preamble(slot) for slot in ('c', 'b')}
runs = [('c', case) for case in central_cases] + [('b', case) for case in (
    'success', 'rollback', 'check_success', 'check_wrong_profile', 'database_query_failure', 'transition_success',
    'check_wrong_instance', 'check_waiting_handoff', 'unadmitted_deploy', 'foreign_card_pair', 'rollback_stale',
    'transition_error_log', 'transition_wrong_endpoint', 'rollback_cross', 'rollback_masked_endpoint', 'rollback_missing_endpoint')]
recovery_cases = (
    'receipt_signal', 'receipt_signal_changed', 'unmask_reload', 'unmask_reload_old_cid',
    'client_unknown', 'foreign_named_cid', 'late_notify_client', 'capture_inv_changed', 'capture_cid_changed', 'capture_restarted',
    'error_log_replaced', 'peer_probe_failure', 'error_log_guard_failure', 'error_log_evidence_failure',
    'signal_INT', 'signal_HUP', 'signal_group_TERM',
)
maintenance_cases = (
    'success', 'reboot', 'mask_failure', 'stop_failure', 'activation_failure',
    'collector_143', 'guard_failure', 'guard_check_failure', 'foreign_guard', 'stop_control_pid', 'legacy_receipt',
    'collected_143', 'died_duplicate', 'died_other_cid', 'died_stale', 'stop_terminal_invocation', 'stop_leftover_id',
    'signal_TERM', 'error_log_seal_failure', 'rollback_issuer_failure', 'arm_collector_profile',
    'other_exit', 'other_invocation', 'leftover_container', 'inventory_unavailable',
    'load_collector_replaced', 'load_collector_restarted', 'load_issuer_invocation', 'load_issuer_image',
    'load_issuer_restarted', 'load_issuer_unhealthy', 'load_issuer_socket', 'stop_other_invocation', 'stop_missing_job',
    'stop_stale_job',
    'stop_peer_restarted', 'mask_race', 'mask_replaced',
    'missing_stop_receipt', 'database_query_failure', 'rollback_image', 'rollback_profile', 'rollback_unhealthy',
    'candidate_check_failure', 'substitution_failure', 'error_log', 'unmask_failure',
    'source_changed', 'identity_changed', 'inputs_changed', 'foreign_hold', 'lock_busy',
    'operator_mask', 'incomplete_hold', 'candidate_unit_changed', 'candidate_profile_changed', 'wrong_endpoint',
    'installed_unit_changed', 'helper_changed', 'migration_changed', 'source_manifest_changed',
    'candidate_image_overlay_changed', 'candidate_endpoint_overlay_changed',
    'rollback_cross', 'rollback_coordinated', 'capacity_exhausted',
    'previous_candidate', 'origin_receipt_changed', 'copied_hold_receipt', 'lost_seal', 'seal_changed',
    'rollback_replaced', *recovery_cases,
)
runs += [(slot, 'maintenance_' + case) for slot in slots for case in maintenance_cases
         if (slot == 'c' or not case.startswith('capacity_')) and (slot == 'b' or case != 'installed_unit_changed')]
runs += [('b', 'staged_' + scenario) for scenario in (
    'success', 'already_loaded', 'error_log', 'missing_start_record', 'archive_drift', 'partial_transport', 'origin_stale', 'unsafe_stage',
    'changed_seal', 'absent_hold', 'foreign_hold', 'missing_seal', 'other_owner', 'other_invocation', 'other_cid', 'old_start_cid',
    'candidate_check_failure', 'database_query_failure', 'profile_drift', 'wrong_window', 'wrong_boot', 'wrong_tool',
    'expire_after_stop', 'expire_after_unmask', 'expire_after_issuer', 'expired_late_notify_client', 'running_pooled_origin')]
for slot, case in runs:
    expected_status, restored = central_cases.get(case, (0, False))
    service, port = slot_table[slot]['service'], int(slot_table[slot]['port'])
    slot_vars = slots[slot] + f'slot={slot} service="$slot_service" port="$slot_port" host_label="$slot_host"\n'
    collector_unit, issuer_unit = slot_table[slot]['collector_unit'], slot_table[slot]['issuer_unit']
    b_unit_source = f"{slot_table['b']['unit_dir']}/{container('b')}.container"
    label = f'{slot} {case}'
    with tempfile.TemporaryDirectory(prefix='central-cutover-test-') as raw:
        state = Path(raw)
        current, staging, backup = state / 'current', state / 'staging', state / f'card-po-{slot}-{change}'
        staged_pair = case.startswith('staged_')
        if staged_pair:
            backup = state / f'staged-po-b-{change}'
            (state / 'staged-pair').touch()
        for directory in (current, staging, state / 'images', state / 'units', state / f'po-current-{slot}'):
            directory.mkdir()
        host, db_port = ('holo-postgres', 5432) if slot == 'c' else ('100.100.1.8', 5433)
        if case == 'maintenance_direct_origin':
            (state / 'direct-origin').touch()
        installed_definition = f'[Container]\nEnvironment=POSTGRES_HOST={host}\nEnvironment=POSTGRES_PORT={db_port}\n'
        if slot == 'c':
            installed_definition += 'Environment=POSTGRES_POOL_MAX_CONNS=${COLLECTOR_POSTGRES_POOL_MAX_CONNS}\n[Service]\nEnvironment=COLLECTOR_POSTGRES_POOL_MAX_CONNS=6\nEnvironmentFile=/etc/stack-secrets/hololive-bot/compose.env\n'
        else:
            installed_definition = (root / b_unit_source).read_text().replace(
                'Environment=POSTGRES_PORT=5433', f'Environment=POSTGRES_PORT={db_port}')
        previous_host, previous_port = (('holo-pgbouncer', 6432) if slot == 'c' else ('100.100.1.8', 6433)) if case == 'rollback_cross' else (host, db_port)
        previous_definition = installed_definition.replace(f'POSTGRES_HOST={host}', f'POSTGRES_HOST={previous_host}').replace(f'POSTGRES_PORT={db_port}', f'POSTGRES_PORT={previous_port}') + '# previous revision\n'
        (state / f'units/{container(slot)}.container').write_text(installed_definition)
        (state / 'compose.env').write_text('APP_ENV=production\n')
        for name in ('hololive-api', 'hololive-alarm-worker'):
            shutil.copyfile(root / f'deploy/quadlet/hololive-bot/{name}.container', state / f'units/{name}.container')
        source_files = (root / 'scripts/deploy/po-central-files.txt').read_text().splitlines()
        for name in source_files:
            target = staging / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(root / name, target)
        (staging / 'source-files.sha256').write_text(''.join(
            f'{hashlib.sha256((staging / name).read_bytes()).hexdigest()}  {name}\n' for name in source_files))
        if case in ('capacity_exhausted', 'rollback_capacity_exhausted'):
            (state / 'compose.env').write_text('COLLECTOR_POSTGRES_POOL_MAX_CONNS=8\n')
        is_check, is_rollback = case.startswith('check_'), case.startswith('rollback')
        issuer_absent = case in ('absent_success', 'absent_candidate_check_failure')
        initial = 'new' if is_check or is_rollback else 'old'
        image_file(state, collector_prod).write_text(initial)
        image_file(state, old_id).write_text('old')
        if not issuer_absent:
            image_file(state, issuer_prod).write_text(initial)
        captured_cid = '3' if staged_pair else '1'
        (state / 'collector.generation').write_text('{"container_id":"' + captured_cid.zfill(64) + '","pid":42}\n')
        (state / 'issuer.started').write_text('1')
        (state / 'collector.started').write_text('1')
        (state / 'loaded.profile').write_text('retained profile\n')
        for kind in ('collector', 'issuer'):
            absent = kind == 'issuer' and issuer_absent
            (state / f'{kind}.invocation').write_text('1'.zfill(32) + '\n')
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
        active = state / f'po-{slot}.card-active'
        prior_backup = backup.with_name(f'card-po-{slot}-20261002T000000Z')
        prior_backup.mkdir()
        info = prior_backup.stat()
        origin_pair = f'{prior_backup} {info.st_dev}:{info.st_ino} {old_revision} {old_id} {old_id}\n'
        if staged_pair:
            active.write_text(origin_pair)
            (prior_backup / 'card-candidate').write_text(origin_pair)
            generation = ''.join(f'{kind} {"1".zfill(32)} {str(cid).zfill(64)} {"a" * 64} 1 43 42\n'
                                 for kind, cid in (('collector', 3), ('issuer', 4)))
            digest = hashlib.sha256((origin_pair + generation).encode()).hexdigest()
            (state / 'approved-origin').write_text(f'{"1" * 32} {old_revision} {old_id} {old_id} {digest} {int(time.time()) + 3600} {"1".zfill(40)}\n')
        if staged_pair or case.startswith('maintenance_') and not case.startswith('maintenance_rollback_'):
            if case == 'maintenance_previous_candidate':
                (staging / 'collector-image-id').write_text(old_id)
            names = ('source-files.sha256', 'rootfs-manifest.json', 'collector-manifest.json', 'image-id',
                     'collector-image-id', 'image.tar.sha256', 'collector-image.tar.sha256')
            transport = staging / 'transport-files.sha256'
            transport.write_text(''.join(f'{hashlib.sha256((staging / n).read_bytes()).hexdigest()}  {n}\n' for n in names))
            (state / 'trusted-seal').write_text(hashlib.sha256(transport.read_bytes()).hexdigest())
        elif not (is_check or is_rollback or case == 'unadmitted_deploy'):
            active.write_text(origin_pair.replace(old_id, new_id) if case == 'foreign_card_pair' else origin_pair)
        live_po = state / f'po-current-{slot}'
        if case != 'missing_receipt' and not issuer_absent:
            (live_po / 'rootfs-manifest.json').write_text(manifest(new_revision if initial == 'new' else old_revision, new_id if initial == 'new' else old_id))
            (live_po / 'image-id').write_text(new_id if initial == 'new' else old_id)
        if is_check or is_rollback:
            image_file(state, collector_rollback).write_text('old')
            image_file(state, issuer_rollback).write_text('old')
            backup.mkdir()
            info = backup.stat()
            candidate_pair = f'{backup} {info.st_dev}:{info.st_ino} {new_revision} {new_id} {new_id}\n'
            names = ('image-id', 'collector-image-id', 'rootfs-manifest.json', 'collector-manifest.json',
                     'image.tar.sha256', 'collector-image.tar.sha256', 'source-files.sha256')
            (backup / 'candidate-inputs.sha256').write_text(''.join(
                f'{hashlib.sha256((staging / n).read_bytes()).hexdigest()}  {n}\n' for n in names))
            (backup / 'card-origin').write_text(origin_pair)
            (backup / 'card-candidate').write_text(candidate_pair)
            active.write_text(candidate_pair.replace(str(backup), str(prior_backup)) if case == 'rollback_stale' else candidate_pair)
            (backup / 'snapshot-complete').touch()
            previous_pair = current / 'artifacts/worker-profile/releases' / backup.name / service / 'previous'
            previous_pair.mkdir(parents=True)
            (previous_pair / 'profile.json').write_text('retained profile\n')
            if case != 'rollback_missing_endpoint':
                captured_host, captured_port = host, db_port
                if case in ('rollback_cross', 'rollback_masked_endpoint'):
                    captured_host, captured_port = ('holo-pgbouncer', 6432) if slot == 'c' else ('100.100.1.8', 6433)
                (backup / 'collector.endpoint').write_text(f'{captured_host}|{captured_port}\n')
            if is_check:
                candidate = previous_pair.with_name('candidate')
                candidate.mkdir()
                (candidate / 'profile.json').write_text('candidate profile\n')
                if case != 'check_wrong_profile':
                    (state / 'loaded.profile').write_text('candidate profile\n')
            (previous_pair / 'unit.container').write_text(previous_definition)
            (backup / 'issuer.state').write_text('present')
            (backup / 'collector.revision').write_text(old_revision)
            (backup / 'collector-prechange-manifest.json').write_text(manifest(old_revision, old_id))
            (backup / 'collector-prechange.image-id').write_text(old_id)
            (backup / 'po-prechange-manifest.json').write_text(manifest(old_revision, old_id))
            (backup / 'po-prechange.image-id').write_text(old_id)
            (backup / 'change-started-at').write_text('2026-10-03T00:00:00Z')
        manager = slot_vars + central_setup + central_config + '\nsystem_unit_dir="$fixture_root/system-units"\n'
        body = manager + central_functions + '\n' + central_source_gate + '\n' + central_main
        (state / 'system-units').mkdir()
        if staged_pair:
            subprocess.run(['chmod', '-R', 'go-w', str(staging)], check=True)
            scenario = case.removeprefix('staged_')
            kernel_boot = state / 'boot-id'
            kernel_boot.write_text('c9b38886-755a-444a-9cb8-74cfb379012f\n')
            kernel_boot.chmod(0o444)
            body = body.replace('/proc/sys/kernel/random/boot_id', '"$fixture_root/boot-id"')
            hold = state / 'po-b.hold'
            masks = [state / 'system-units' / u for u in (issuer_unit, collector_unit)]
            expected_active = origin_pair
            def invoke(op, failure=''):
                return run(body, state, 'maintenance_' + op + ('_' + failure if failure else ''))
            def calls_now():
                return (state / 'calls').read_text().splitlines() if (state / 'calls').exists() else []
            generic = run(body, state, 'generic_deploy')
            require(generic.returncode != 0 and not any(c.startswith(mutations) for c in calls_now()),
                    'b: generic automatic restoration path cannot consume sealed staging', generic)
            if scenario == 'already_loaded':
                for image in (collector_prod, issuer_prod):
                    image_file(state, image.replace(':prod', ':candidate')).write_text('new')
            if scenario == 'partial_transport':
                transport.write_text(transport.read_text().splitlines(keepends=True)[0])
                (state / 'trusted-seal').write_text(hashlib.sha256(transport.read_bytes()).hexdigest())
            if scenario == 'archive_drift':
                (staging / 'image.tar').write_text('substituted archive with same manifest')
            elif scenario == 'origin_stale':
                (prior_backup / 'card-candidate').write_text(origin_pair.replace(old_revision, new_revision))
            elif scenario == 'other_owner':
                other = backup.with_name('card-po-b-20261001T000000Z')
                other.mkdir()
                st = other.stat()
                expected_active = f'{other} {st.st_dev}:{st.st_ino} {old_revision} {old_id} {old_id}\n'
                (other / 'card-candidate').write_text(expected_active)
                active.write_text(expected_active)
            elif scenario == 'other_invocation':
                (state / 'collector.invocation').write_text('7'.zfill(32) + '\n')
            elif scenario == 'other_cid':
                (state / 'collector.cid').write_text('7')
            elif scenario == 'unsafe_stage':
                (staging / 'foreign').symlink_to(prior_backup)
            elif scenario == 'running_pooled_origin':
                (state / 'direct-origin').touch()
            preflight_failure = scenario in ('candidate_check_failure', 'database_query_failure')
            result = invoke('quiesce', scenario if preflight_failure or scenario == 'expire_after_stop' else '')
            if scenario == 'expire_after_stop':
                calls = calls_now()
                require(result.returncode != 0 and '[OUTCOME] unconfirmed:' in result.stderr and
                        [c for c in calls if c.startswith('systemctl stop ')] == ['systemctl stop ' + issuer_unit] and
                        (hold / f'{issuer_unit}.stop-cid').is_file() and not (hold / 'quiesced').exists() and
                        not any(c.startswith('guard seal ') for c in calls) and active.read_text() == origin_pair,
                        'b: settle the submitted issuer stop; no second stop or seal after expiry', result)
                continue
            if preflight_failure:
                calls = calls_now()
                expected_check = 'pair check-database' if scenario == 'database_query_failure' else 'pair check'
                reached = any(c.startswith(expected_check + ' ') and c.endswith('/candidate') for c in calls)
                require(reached and result.returncode != 0 and not hold.exists() and active.read_text() == origin_pair and
                        all((state / f'{k}.running').read_text() == 'true' and
                            (state / f'{k}.invocation').read_text().strip() == '1'.zfill(32) and
                            (state / f'{k}.runtime').read_text() == 'old' for k in ('collector', 'issuer')) and
                        not any(c.startswith(('guard arm ', 'systemctl stop ', held_start, 'pair activate ')) for c in calls),
                        f'{label}: incompatible profile or pooled preflight cannot stop the healthy W2 pair', result)
                continue
            if scenario in ('archive_drift', 'partial_transport', 'origin_stale', 'other_owner', 'other_invocation', 'other_cid', 'unsafe_stage', 'running_pooled_origin'):
                require(result.returncode != 0 and not hold.exists() and not backup.exists() and
                        all((state / f'{k}.running').read_text() == 'true' for k in ('collector', 'issuer')) and
                        not any(c.startswith(mutations) for c in calls_now()) and active.read_text() == expected_active,
                        f'{label}: staged bytes and admitted W2 owner proven before effects', result)
                continue
            require(result.returncode == 0 and (hold / 'quiesced').read_text() == 'guarded-stop-v1\n' and
                    (backup / 'card-origin').read_text() == origin_pair and
                    (backup / 'transport.sha256').read_text().strip() == (state / 'trusted-seal').read_text(),
                    f'{label}: transport proof and prior admitted publisher retained independently', result)
            if scenario == 'already_loaded':
                require(not any(c.startswith('podman load ') for c in calls_now()), 'b: matching candidate images never reloaded', result)
            if scenario == 'absent_hold':
                hold.rename(state / 'unowned-hold')
            elif scenario == 'foreign_hold':
                (hold / 'backup').write_text(str(prior_backup))
            elif scenario == 'missing_seal':
                (backup / 'transport.sha256').unlink()
            elif scenario == 'changed_seal':
                (state / 'trusted-seal').write_text('e' * 64)
            elif scenario == 'wrong_window':
                path = state / 'approved-origin'
                path.write_text('9' * 32 + path.read_text()[32:])
            elif scenario == 'wrong_boot':
                next_boot = state / 'next-boot-id'
                next_boot.write_text('28ceceb9-e349-4d7c-b6a7-1f77ee31b4f0\n')
                next_boot.chmod(0o444)
                next_boot.replace(kernel_boot)
            elif scenario == 'wrong_tool':
                path = state / 'approved-origin'
                path.write_text(path.read_text().replace('1'.zfill(40) + '\n', 'f' * 40 + '\n'))
            elif scenario == 'profile_drift':
                profile = current / 'artifacts/worker-profile/releases' / backup.name / service / 'candidate/profile.json'
                profile.write_text('substituted valid profile')
                (state / 'source.profile').write_text(profile.read_text())
            before = len(calls_now())
            failures = ('error_log', 'missing_start_record', 'old_start_cid', 'expire_after_issuer', 'expire_after_unmask', 'expired_late_notify_client')
            result = invoke('resume', scenario if scenario in failures else '')
            after = calls_now()[before:]
            if scenario == 'expired_late_notify_client':
                submitted = (backup / f'resume-{collector_unit}.start-request').read_text().split()
                observed = (backup / f'resume-{collector_unit}.start').read_text().split()
                start = held_start + collector_unit
                require(result.returncode == 9 and '[OUTCOME] unconfirmed:' in result.stderr and
                        re.fullmatch(r'@[0-9]+\.[0-9]{6}', submitted[0]) and submitted[1:] == ['18', '9'] and
                        observed[:2] == submitted[:2] and observed[2:5] == ['2'.zfill(32), '5'.zfill(64), 'b' * 64] and
                        (state / 'notify.246s').is_file() and not (state / 'notify.pending').exists() and
                        (state / 'collector.running').read_text() == 'true' and not (hold / 'quiesced').exists() and
                        after.count(start) == 1 and not any(c.startswith(mutations) for c in after[after.index(start) + 1:]) and
                        active.read_text() == origin_pair, 'b: expired client failure retains the submitted and settled job without another writer action', result)
                print(f'[PASS] {label}')
                continue
            if scenario == 'expire_after_unmask':
                require(result.returncode != 0 and '[OUTCOME] unconfirmed:' in result.stderr and masks[1].is_symlink() and
                        [c for c in after if c.startswith('systemctl unmask ')] == ['systemctl unmask ' + issuer_unit] and
                        not any(c.startswith((held_start, 'systemctl stop ', 'guard arm ')) for c in after),
                        'b: an admitted unmask does not authorize a start or rehold after expiry', result)
                continue
            if scenario == 'expire_after_issuer':
                require(result.returncode != 0 and '[OUTCOME] unconfirmed:' in result.stderr and
                        [c for c in after if c.startswith(held_start)] == [held_start + issuer_unit] and
                        (backup / f'resume-{issuer_unit}.start').is_file() and masks[1].is_symlink() and
                        f'systemctl unmask {collector_unit}' not in after and not any(c.startswith('systemctl stop ') for c in after),
                        'b: settle issuer start; collector remains masked and no expired rehold is submitted', result)
                continue
            if scenario in ('absent_hold', 'foreign_hold', 'missing_seal', 'changed_seal', 'wrong_window', 'wrong_boot', 'wrong_tool', 'profile_drift'):
                require(result.returncode != 0 and not any(c.startswith(mutations) for c in after) and
                        all(m.is_symlink() for m in masks) and active.read_text() == origin_pair,
                        f'{label}: never release masks without seal and owned current hold', result)
                if scenario == 'wrong_tool':
                    refused = invoke('rollback')
                    require(refused.returncode != 0 and not any(c.startswith(mutations) for c in calls_now()[before:]),
                            'b: rollback cannot substitute the staged tool receipt', refused)
                continue
            require([c for c in after if c.startswith((held_start, 'systemctl restart '))] ==
                    [held_start + u for u in (issuer_unit, collector_unit)] and not any(
                        c.startswith('pair activate ') and c.endswith('/previous') for c in after),
                    f'{label}: only guarded original start requests, no automatic restoration', result)
            if scenario in ('success', 'already_loaded'):
                require(result.returncode == 0 and not hold.exists() and active.read_text() == (backup / 'card-candidate').read_text() and
                        all((state / f'{k}.runtime').read_text() == 'new' for k in ('issuer', 'collector')),
                        'b: activates exactly the staged pair', result)
                for unit, job in ((issuer_unit, 17), (collector_unit, 18)):
                    fields = (backup / f'resume-{unit}.start').read_text().split()
                    require(fields[1:3] == [str(job), '2'.zfill(32)] and fields[4] == 'b' * 64,
                            f'b: retains original {unit} job, invocation and generation', result)
                checked = invoke('check')
                require(checked.returncode == 0, 'b: completion checks both original launched generations', checked)
                (state / 'collector.cid').write_text('9')
                before = len(calls_now())
                for op in ('check', 'resume', 'rollback'):
                    refused = invoke(op)
                    require(refused.returncode != 0 and not any(c.startswith(mutations) for c in calls_now()[before:]),
                            f'b: {op} cannot adopt a replaced generation or reuse a released hold', refused)
            elif scenario in ('missing_start_record', 'old_start_cid'):
                require(result.returncode != 0 and '[OUTCOME] unconfirmed:' in result.stderr and not (hold / 'quiesced').exists() and
                        not (backup / f'resume-{collector_unit}.start').exists() and (state / 'collector.running').read_text() == 'true' and
                        f'systemctl stop {collector_unit}' not in after and active.read_text() == origin_pair,
                        'b: running collector without original manager record is never stopped or certified', result)
                before = len(calls_now())
                refused = invoke('rollback')
                require(refused.returncode != 0 and not any(c.startswith(mutations) for c in calls_now()[before:]),
                        'b: no rollback from unconfirmed collector start', refused)
            else:
                require(result.returncode != 0 and '[OUTCOME] held:' in result.stderr and all(m.is_symlink() for m in masks) and
                        (hold / 'quiesced').read_text() == 'guarded-stop-v1\n' and active.read_text() == origin_pair,
                        'b: log failure stops only the admitted start generations; no publisher restored', result)
                before = len(calls_now())
                refused = invoke('resume')
                require(refused.returncode != 0 and not any(c.startswith(mutations) for c in calls_now()[before:]),
                        'b: repeated post-card activation refused', refused)
                restored_pair = invoke('rollback')
                require(restored_pair.returncode == 0 and not hold.exists() and active.read_text() == origin_pair and
                        all((state / f'{k}.runtime').read_text() == 'old' for k in ('issuer', 'collector')),
                        'b: separately authorized confirmed hold may restore admitted W2, not pre-card publisher', restored_pair)
            print(f'[PASS] {label}')
            continue
        if case.startswith('maintenance_'):
            scenario = case.removeprefix('maintenance_')
            hold = state / f'po-{slot}.hold'
            masks = [state / 'system-units' / name for name in (issuer_unit, collector_unit)]
            guards = [mask.with_suffix('.service.guard') for mask in masks]
            release = current / 'artifacts/worker-profile/releases' / backup.name / service
            (state / 'source.profile').write_text('reviewed candidate profile\n')
            if slot == 'c':
                (current / '.source-revision').write_text(old_revision)

            def invoke(mode, failure=''):
                return run(body, state, 'maintenance_' + mode + ('_' + failure if failure else ''))

            def calls_now():
                return (state / 'calls').read_text().splitlines() if (state / 'calls').exists() else []

            def assert_new_manager_blocks():
                for unit in (issuer_unit, collector_unit):
                    attempted = run(manager + f'\nsystemctl start {unit}\n', state, 'maintenance_boot')
                    require(attempted.returncode == 8, f'{slot} {scenario}: persistent admission denied after reboot', attempted)

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
            if scenario == 'foreign_guard':
                guards[1].write_text('operator guard\n')
            if slot == 'c' and scenario == 'success':
                (state / 'pending-suffix').touch()
            fail_quiesce = scenario.startswith('load_') or scenario in (
                'mask_failure', 'stop_failure', 'stop_other_invocation', 'stop_missing_job', 'stop_stale_job',
                'stop_peer_restarted', 'mask_race', 'guard_failure', 'guard_check_failure', 'foreign_guard', 'arm_collector_profile',
                'stop_control_pid', 'collector_143', 'collected_143', 'died_duplicate', 'died_other_cid', 'died_stale',
                'stop_terminal_invocation', 'stop_leftover_id')
            result = invoke('quiesce', scenario if fail_quiesce else '')
            if scenario == 'previous_candidate':
                require(result.returncode != 0 and not hold.exists() and
                        not any(c.startswith(('podman load ', 'systemctl stop ', held_start)) for c in calls_now()),
                        f'{slot}: wrong original route or retained candidate cannot open the migration window', result)
                continue
            if scenario.startswith('load_'):
                require(result.returncode != 0 and not hold.exists() and
                        not any(c.startswith(('mask link ', 'systemctl stop ', 'systemctl restart ')) for c in calls_now()),
                        f'{slot} {scenario}: changed live pair rejected before hold', result)
                continue
            if scenario in ('guard_failure', 'guard_check_failure', 'foreign_guard', 'arm_collector_profile'):
                require(result.returncode != 0 and '[OUTCOME] unconfirmed:' in result.stderr and
                        not (hold / 'quiesced').exists() and not any(c.startswith(('systemctl stop ', 'guard seal ', 'systemctl restart ')) for c in calls_now()),
                        f'{slot} {scenario}: no writer stop without both effective reviewed guards', result)
                if scenario == 'foreign_guard':
                    require(guards[1].read_text() == 'operator guard\n' and not any(m.is_symlink() for m in masks),
                            f'{slot}: foreign guard not replaced or adopted', result)
                continue
            if scenario in ('collector_143', 'collected_143'):
                evidence = (backup / 'guarded-stop.log').read_text()
                outcome = evidence.split(f'unit={collector_unit} stop=true invocation={"1".zfill(32)}', 1)
                require(len(outcome) == 2 and 'exit_status=143' in outcome[1].splitlines()[0] and
                        f'unit={collector_unit} unconfirmed' in outcome[1] and
                        ('ActiveState=inactive' if scenario == 'collected_143' else 'ExecMainStatus=143') in outcome[1] and
                        masks[1].is_symlink() and not (state / 'collector.exit').exists(),
                        f'{slot}: matched manager stop and abnormal collector result survive manager collection', result)
            if scenario in ('stop_other_invocation', 'stop_missing_job', 'stop_stale_job', 'stop_peer_restarted', 'mask_race',
                            'collector_143', 'collected_143', 'died_duplicate', 'died_stale', 'died_other_cid', 'stop_terminal_invocation'):
                if scenario in ('stop_peer_restarted', 'mask_race'):
                    require(f'systemctl stop {collector_unit}' not in calls_now(),
                            f'{slot}: changed peer not accepted as captured after issuer stop', result)
                require(result.returncode != 0 and '[OUTCOME] unconfirmed:' in result.stderr and
                        not (hold / 'quiesced').exists(), f'{slot}: no receipt for wrong or absent stop job', result)
                before = len(calls_now())
                for mode in ('resume', 'rollback'):
                    refused = invoke(mode)
                    require(refused.returncode != 0 and not any(c.startswith(mutations) for c in calls_now()[before:]),
                            f'{slot}: incomplete stop cannot release the pair', refused)
                continue
            if scenario in ('mask_failure', 'stop_failure', 'stop_control_pid', 'stop_leftover_id'):
                require(result.returncode != 0 and hold.exists() and (backup / 'snapshot-complete').exists(),
                        f'{slot} {scenario}: partial failure retains recovery', result)
                require(not any(c.startswith('systemctl restart ') for c in calls_now()),
                        f'{slot} {scenario}: no old writer restart', result)
                require((state / 'issuer.running').read_text() == 'false' and masks[0].is_symlink(),
                        f'{slot} {scenario}: issuer stopped first', result)
                require((state / 'collector.running').read_text() == ('true' if scenario == 'stop_failure' else 'false'),
                        f'{slot} {scenario}: stop outcome preserved', result)
                require(not masks[1].is_symlink() and guards[1].is_file() and not (hold / 'quiesced').exists() and
                        '[OUTCOME] unconfirmed:' in result.stderr, f'{slot} {scenario}: persistent guard retained, no receipt', result)
                assert_new_manager_blocks()
                before = len(calls_now())
                rejected = invoke('resume')
                require(rejected.returncode != 0 and not any(c.startswith(mutations) for c in calls_now()[before:]),
                        f'{slot} {scenario}: incomplete hold cannot resume', rejected)
                continue
            require(result.returncode == 0, f'{slot} {scenario}: pooled original pair quiesced', result)
            assert_held(result)
            if scenario == 'success':
                before = len(calls_now())
                refused = invoke('rollback')
                require(refused.returncode != 0 and not any(c.startswith(mutations) for c in calls_now()[before:]),
                        f'{slot}: card transition never issues rollback authority, even on the same pool', refused)
            require((hold / 'quiesced').read_text() == 'guarded-stop-v1\n' and not any(g.exists() for g in guards),
                    f'{slot}: both guards sealed before versioned receipt', result)
            require((release / 'previous/unit.container').read_text() == installed_definition and not (release / 'candidate').exists(),
                    f'{slot} {scenario}: old unit preserved without premature candidate', result)
            require(image_file(state, collector_prod).read_text() == 'old' and (state / 'dropin').read_text() == 'initial',
                    f'{slot} {scenario}: quiesce does not publish candidate', result)
            stops = [c for c in calls_now() if c.startswith('systemctl stop ')]
            require(stops == [f'systemctl stop {u}' for u in (issuer_unit, collector_unit)],
                    f'{slot} {scenario}: issuer-first stop', result)
            calls = calls_now()
            require(max(calls.index(f'guard arm {u}') for u in (issuer_unit, collector_unit)) < calls.index(stops[0]) and
                    calls.index(stops[-1]) < min(calls.index(f'guard seal {u}') for u in (issuer_unit, collector_unit)),
                    f'{slot}: both guards before stop and both stop outcomes before mask reload', result)
            (state / 'db.stopped').touch()
            before = len(calls_now())
            checked = invoke('check')
            competed = invoke('quiesce')
            require(checked.returncode != 0 and competed.returncode != 0 and
                    not any(c.startswith(mutations) for c in calls_now()[before:]),
                    f'{slot} {scenario}: held state is not completed or replaceable', checked)
            if scenario == 'reboot':
                (state / 'issuer.exit').unlink()
                assert_new_manager_blocks()
                assert_held(result)
            candidate_definition = installed_definition
            unit_path = state / f'units/{container(slot)}.container'
            unit_path.write_text(candidate_definition)
            if slot == 'c':
                (current / '.source-revision').write_text(new_revision)
            else:
                shutil.copyfile(staging / b_unit_source, unit_path)
                require(unit_path.read_text() == candidate_definition and not (current / '.source-revision').exists(),
                        'b: actual staged candidate installed without source marker', result)
            (state / 'db.stopped').unlink()
            if scenario == 'other_exit':
                (state / 'issuer.exit').write_text('137\n')
            elif scenario == 'other_invocation':
                (state / 'issuer.invocation').write_text('2'.zfill(32))
            elif scenario == 'missing_stop_receipt':
                (hold / 'quiesced').unlink()
            elif scenario == 'legacy_receipt':
                (hold / 'quiesced').write_text('')
                before = len(calls_now())
                refused = invoke('rollback')
                require(refused.returncode != 0 and not any(c.startswith(mutations) for c in calls_now()[before:]),
                        f'{slot}: old static receipt cannot release masks on rollback', refused)
            elif scenario == 'leftover_container':
                (state / 'collector.leftover').touch()
            elif scenario == 'inventory_unavailable':
                (state / 'inventory-unavailable').touch()
            if scenario == 'capacity_exhausted':
                (state / 'compose.env').write_text('COLLECTOR_POSTGRES_POOL_MAX_CONNS=8\n')
            if scenario == 'wrong_endpoint' or scenario.startswith('rollback_'):
                unit_path.write_text(installed_definition.replace('holo-postgres', 'holo-pgbouncer').replace('POSTGRES_PORT=5432', 'POSTGRES_PORT=6432').replace('POSTGRES_PORT=5433', 'POSTGRES_PORT=6433'))
            elif scenario == 'origin_receipt_changed':
                (backup / 'collector.endpoint').write_text('holo-pgbouncer|6432\n' if slot == 'c' else '100.100.1.8|6433\n')
            elif scenario == 'copied_hold_receipt':
                (hold / 'card-candidate').unlink()
                shutil.copyfile(backup / 'card-candidate', hold / 'card-candidate')
            elif scenario == 'lost_seal':
                (backup / 'transport.sha256').unlink()
            elif scenario == 'seal_changed':
                (staging / 'transport-files.sha256').write_text('other transport\n')
            if scenario == 'source_changed':
                if slot == 'c':
                    (current / '.source-revision').write_text('c' * 40)
                else:
                    staged_unit = staging / b_unit_source
                    staged_unit.write_text(staged_unit.read_text() + '# unreviewed source\n')
            elif scenario == 'installed_unit_changed':
                unit_path.write_text(candidate_definition + 'Environment=EXTRA=changed\n')
            elif scenario == 'helper_changed':
                helper = staging / 'scripts/deploy/lib/compose-env.sh'
                helper.write_text(helper.read_text() + f'\ntouch "{state}/unreviewed-helper-ran"\n')
            elif scenario == 'migration_changed':
                (staging / 'hololive/hololive-api/scripts/migrations/001_schema_epoch4_baseline.sql').write_text('-- changed\n')
            elif scenario == 'source_manifest_changed':
                (staging / 'source-files.sha256').write_text(
                    f'{hashlib.sha256(unit_path.read_bytes()).hexdigest()}  {b_unit_source}\n')
            elif scenario == 'identity_changed':
                image_file(state, f'{collector_image}:candidate').write_text('old')
            elif scenario == 'inputs_changed':
                (staging / 'collector-image-id').write_text(old_id)
            elif scenario == 'foreign_hold':
                (hold / 'backup').write_text(str(backup) + '-foreign\n')
            elif scenario == 'mask_replaced':
                masks[0].unlink()
                masks[0].symlink_to('/dev/null')
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
                failed_restore = scenario in ('rollback_image', 'rollback_profile', 'rollback_unhealthy', 'rollback_issuer_failure', 'rollback_replaced')
                before = len(calls_now())
                result = invoke('rollback', scenario if failed_restore else '')
                if failed_restore:
                    require(result.returncode != 0 and 'previous collector/issuer restored' not in result.stdout,
                            f'{slot}: ready does not hide the failed restored runtime check', result)
                    if scenario == 'rollback_replaced':
                        stopped = [c for c in calls_now()[before:] if c.startswith('systemctl stop ')]
                        require(stopped == [f'systemctl stop {issuer_unit}'] and guards[1].is_file() and
                                (state / 'collector.running').read_text() == 'true' and
                                not (hold / 'quiesced').exists() and '[OUTCOME] unconfirmed:' in result.stderr,
                                f'{slot}: held rollback never stops replacement or certifies it as the original', result)
                        assert_new_manager_blocks()
                        continue
                    assert_held(result)
                    reopened = [issuer_unit] if scenario == 'rollback_issuer_failure' else [issuer_unit, collector_unit]
                    require([c for c in calls_now()[before:] if c.startswith('guard arm ')] == [f'guard arm {u}' for u in reopened] and
                            (hold / 'quiesced').is_file() and (hold / 'quiesced').read_text() == 'guarded-stop-v1\n',
                            f'{slot}: failed manual rollback re-holds only opened units', result)
                    continue
                require(result.returncode == 0 and not hold.exists() and all(not m.is_symlink() for m in masks),
                        f'{slot}: coordinated rollback releases owned masks', result)
                require(all((state / f'{k}.runtime').read_text() == 'old' for k in ('issuer', 'collector')),
                        f'{slot}: rollback restores original pair', result)
                require(unit_path.read_text() == installed_definition and
                        (release / 'previous/unit.container').read_text() == installed_definition,
                        f'{slot}: coordinated rollback retains the exact previous unit', result)
            else:
                if slot == 'c' and scenario == 'success':
                    require('lease-proof applied' in calls_now() and 'lease-proof target' not in calls_now(),
                            'c: a pending target does not prevent quiescing the applied head', result)
                    before = len(calls_now())
                    blocked = invoke('resume')
                    actions = calls_now()[before:]
                    require(blocked.returncode != 0 and 'lease-proof target' in actions and
                            not any(c.startswith((held_start, 'systemctl restart ', 'systemctl unmask ', 'pair activate ')) for c in actions),
                            'c: pending target cannot activate candidate or release either writer', blocked)
                    assert_held(blocked)
                    (state / 'pending-suffix').unlink()
                failure = scenario if scenario in ('activation_failure', 'candidate_check_failure', 'database_query_failure',
                                                   'substitution_failure', 'error_log', 'unmask_failure', 'signal_TERM', 'error_log_seal_failure', *recovery_cases) else ''
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
                    require([c for c in subsequent if c.startswith((held_start, 'systemctl restart '))] ==
                            [held_start + u for u in (issuer_unit, collector_unit)],
                            f'{slot}: issuer-first resume', result)
                    require(all((state / f'{k}.runtime').read_text() == 'new' for k in ('issuer', 'collector')),
                            f'{slot}: candidate pair running', result)
                    checked = invoke('check')
                    require(checked.returncode == 0, f'{slot}: completion check after resume', checked)
                    before = len(calls_now())
                    refused = invoke('rollback')
                    require(refused.returncode != 0 and not any(c.startswith(mutations) for c in calls_now()[before:]),
                            f'{slot}: accepted card transition cannot resurrect the previous publisher', refused)
                else:
                    require(hold.exists() and not any(c.startswith('pair activate ') and c.endswith('/previous') for c in subsequent),
                            f'{slot} {scenario}: no automatic direct restoration', result)
                    require((release / 'previous/unit.container').read_text() == installed_definition and
                            not (state / 'unreviewed-helper-ran').exists(),
                            f'{slot}: recovery unit retained and unverified helper never executed', result)
                    if scenario in recovery_cases or scenario == 'signal_TERM':
                        stops = [c for c in subsequent if c.startswith('systemctl stop ')]
                        starts = [c for c in subsequent if c.startswith((held_start, 'systemctl restart '))]
                        receipt = hold / 'quiesced'
                        if scenario.startswith(('receipt_signal', 'unmask_reload')):
                            intact = scenario in ('receipt_signal', 'unmask_reload')
                            require(not stops and not starts and
                                    [c for c in subsequent if c.startswith('guard undo-unmask ')] ==
                                    ([f'guard undo-unmask {issuer_unit}'] if scenario == 'unmask_reload' else []) and
                                    receipt.exists() == intact, f'{label}: only verified pre-start masks can be restored', result)
                            if scenario.startswith('receipt_signal'):
                                require(result.returncode == 143 and not any(c.startswith('systemctl unmask ') for c in subsequent),
                                        f'{label}: cancellation during receipt invalidation stays before unmask', result)
                            if intact:
                                assert_held(result)
                                require(receipt.read_text() == 'guarded-stop-v1\n', f'{label}: prior managed hold retained', result)
                        elif scenario.startswith('signal_'):
                            require(result.returncode == {'TERM': 143, 'INT': 130, 'HUP': 129}[scenario.rsplit('_', 1)[1]] and
                                    starts == [held_start + issuer_unit] and stops == [f'systemctl stop {issuer_unit}'] and
                                    receipt.read_text() == 'guarded-stop-v1\n', f'{label}: cancel stops only the one launched job', result)
                            assert_held(result)
                        else:
                            both_started = scenario.startswith('error_log_') or scenario == 'late_notify_client'
                            issuer_stopped = both_started or scenario in ('peer_probe_failure', 'client_unknown')
                            expected_stops = ([issuer_unit] if issuer_stopped else []) + (
                                [collector_unit] if scenario in ('error_log_evidence_failure', 'late_notify_client') else [])
                            require(starts == [held_start + u for u in ([issuer_unit, collector_unit] if both_started else [issuer_unit])] and
                                    stops == [f'systemctl stop {u}' for u in expected_stops] and
                                    (state / 'issuer.running').read_text() == ('false' if issuer_stopped else 'true') and
                                    (state / 'collector.running').read_text() == ('true' if both_started and collector_unit not in expected_stops else 'false') and
                                    not receipt.exists() and '[OUTCOME] unconfirmed:' in result.stderr,
                                    f'{label}: only independently proven first generations stopped; no receipt', result)
                            if scenario == 'late_notify_client':
                                require((state / 'notify.246s').is_file() and not (state / 'notify.pending').exists(),
                                        f'{label}: tracked collector job completed after 245s within its own limit', result)
                        assert_new_manager_blocks()
                        continue
                    if scenario == 'substitution_failure':
                        require('[OUTCOME] unconfirmed:' in result.stderr and not (hold / 'quiesced').exists() and
                                not masks[0].is_symlink() and guards[0].is_file() and masks[1].is_symlink() and
                                all((state / f'{kind}.running').read_text() == 'false' for kind in ('issuer', 'collector')) and
                                not any(c.startswith('systemctl stop ') for c in subsequent),
                                f'{slot}: failed start with exit 143 is guarded, never adopted as a managed stop', result)
                    if scenario in ('substitution_failure', 'unmask_failure', 'error_log', 'error_log_seal_failure'):
                        reopened = [issuer_unit, collector_unit] if scenario.startswith('error_log') else [issuer_unit]
                        require([c for c in subsequent if c.startswith('guard arm ')] == [f'guard arm {u}' for u in reopened],
                                f'{slot}: only reopened units acquire fresh admission guards', result)
                        if scenario == 'error_log_seal_failure':
                            require(not (hold / 'quiesced').exists() and guards[1].is_file() and not masks[1].is_symlink(),
                                    f'{slot}: incomplete protective seal does not reissue a receipt', result)
                        elif scenario != 'substitution_failure':
                            require((hold / 'quiesced').read_text() == 'guarded-stop-v1\n',
                                    f'{slot}: protective stop outcomes confirmed', result)
                        assert_new_manager_blocks()
                    if scenario == 'database_query_failure':
                        require(not any(c.startswith(mutations) for c in subsequent),
                                f'{slot}: failed pooled SQL before activation retains the hold', result)
                    if scenario not in ('foreign_hold', 'incomplete_hold', 'error_log_seal_failure', 'substitution_failure'):
                        assert_held(result)
                    if scenario in ('candidate_image_overlay_changed', 'candidate_endpoint_overlay_changed'):
                        require(not any(c.startswith(mutations) for c in subsequent) and
                                overlay.read_text() == changed_overlay,
                                f'{slot}: modified overlay refused without publishing or repairing it', result)
                    if scenario.startswith('capacity_'):
                        require(not any(c.startswith(mutations) for c in subsequent) and (release / 'previous/profile.json').exists(),
                                f'{slot}: capacity refusal preserves hold and recovery before effects', result)
                    if scenario not in ('substitution_failure', 'error_log', 'unmask_failure', 'error_log_seal_failure'):
                        require(not any(c.startswith(('systemctl restart ', 'systemctl unmask ')) for c in subsequent),
                                f'{slot} {scenario}: validation fails before unmask or activation', result)
            print(f'[PASS] {slot} maintenance {scenario}')
            continue
        manager_stop = threading.Event()
        manager = None
        if case == 'client_lost_pending':
            server = socket.socket(socket.AF_UNIX)
            server.bind(str(state / 'manager.sock'))
            server.listen(1)
            server.settimeout(10)
            def manage():
                try:
                    with server.accept()[0] as client:
                        unit = client.recv(256).decode()
                        (state / 'manager-job').write_text(unit)
                        client.sendall(b'queued')
                    manager_stop.wait(10)
                finally:
                    server.close()
            manager = threading.Thread(target=manage)
            manager.start()
            (state / 'manager-client.py').write_text(
                "import os,signal,socket,sys\n"
                "with socket.socket(socket.AF_UNIX) as client:\n"
                " client.connect(sys.argv[1]); client.sendall(sys.argv[2].encode()); assert client.recv(32)==b'queued'\n"
                "os.kill(os.getpid(),signal.SIGKILL)\n")
        try:
            result = run(body, state, case)
            if manager is not None:
                require(manager.is_alive() and (state / 'manager-job').is_file(), 'client exit does not settle manager job', result)
        finally:
            manager_stop.set()
            if manager is not None:
                manager.join(2)

        require(result.returncode == expected_status, f'{label}: original status', result)
        calls = (state / 'calls').read_text().splitlines() if (state / 'calls').exists() else []
        changed = [call for call in calls if call.startswith(mutations)]
        restarts = [call for call in calls if call.startswith('systemctl restart ')]
        endpoint_blocked = case in ('transition_error_log', 'transition_wrong_endpoint')
        if endpoint_blocked:
            require((state / 'collector.running').read_text() == 'false' and (state / 'issuer.running').read_text() == 'false',
                    f'{label}: both services stopped', result)
            require(all((state / 'system-units' / u).is_symlink() for u in (issuer_unit, collector_unit)) and
                    (state / f'po-{slot}.hold/quiesced').read_text() == 'guarded-stop-v1\n',
                    f'{label}: prohibited restore keeps writers durably held', result)
            require((backup / 'snapshot-complete').exists() and (backup / 'collector.endpoint').exists(),
                    f'{label}: recovery records retained', result)
            require(not any(call.startswith('pair activate ') and call.endswith('/previous') for call in calls),
                    f'{label}: previous pair never activated', result)
        if case in ('rollback_cross', 'rollback_masked_endpoint', 'rollback_missing_endpoint'):
            require(not changed, f'{label}: manual cross-endpoint rollback has no runtime mutations', result)
        if case.startswith('capacity_') or case == 'rollback_capacity_exhausted':
            require(not changed, f'{label}: capacity rejection has no runtime mutations', result)
            if is_rollback:
                require((previous_pair / 'profile.json').exists() and
                        (state / f'units/{container(slot)}.container').read_text() == installed_definition,
                        f'{label}: rollback recovery and installed unit retained', result)
            else:
                require(not backup.exists(), f'{label}: capacity rejected before snapshot', result)
        expected_warning = endpoint_blocked or case in ('restore_failure', 'activation_failure', 'client_lost_pending', 'manager_probe_failure')
        require(('automatic restoration failed' in result.stderr) == expected_warning, f'{label}: restoration warning', result)
        if case in ('client_lost_pending', 'manager_probe_failure'):
            require('outcome_unknown' in result.stderr, f'{label}: pending/unknown outcome is explicit', result)
            require((backup / 'cutover-outcome').read_text().strip() == 'outcome_unknown', f'{label}: outcome receipt retained', result)
            require(not any(call.startswith('pair activate ') and call.endswith('/previous') for call in calls), f'{label}: no previous pair activation', result)
            require(len(restarts) == 1, f'{label}: no restoration restart while manager outcome is unknown', result)
        elif case in ('missing_receipt', 'collector_stopped', 'inventory_failure', 'unadmitted_deploy', 'foreign_card_pair'):
            require(not backup.exists() and not changed, f'{label}: no partial backup or runtime change', result)
        elif case == 'rollback_stale':
            require(not changed, f'{label}: copied release receipt is not the active owner', result)
        elif is_check:
            require(not changed, f'{label}: check has no mutations', result)
            require(('completion verified' in result.stdout) == (expected_status == 0),
                    f'{label}: completion output requires the candidate loaded profile', result)
        elif case in ('candidate_check_failure', 'absent_candidate_check_failure', 'activation_failure', 'database_query_failure'):
            require(not restarts, f'{label}: no unit restart', result)
            require(image_file(state, collector_prod).read_text() == 'old', f'{label}: collector tag baseline', result)
            require((image_file(state, issuer_prod).exists()) != issuer_absent, f'{label}: issuer tag baseline', result)
            require((state / 'dropin').read_text() == 'initial', f'{label}: drop-in unchanged', result)
            require(image_file(state, f'{collector_image}:candidate').read_text() == 'new', f'{label}: candidate retained for inspection', result)
        if case in ('success', 'absent_success', 'transition_success'):
            require(restarts == [f'systemctl restart {unit}' for unit in (issuer_unit, collector_unit)], f'{label}: issuer-first restart order', result)
            require(('lease-proof target' in calls) == (slot == 'c'), f'{label}: lease proof only on central', result)
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
            release_dir = current / 'artifacts/worker-profile/releases' / backup.name / service
            require((state / f'units/{container(slot)}.container').read_text() == previous_definition, f'{label}: previous base unit restored', result)
            require((release_dir / f'rollback-replaced-{container(slot)}.container').read_text() == installed_definition, f'{label}: replaced base unit retained', result)
            require(calls.index('systemctl daemon-reload') > -1, f'{label}: units reloaded', result)
        expected_runtime = 'absent' if endpoint_blocked else 'new' if is_check or is_rollback and not restored or case in (
            'success', 'absent_success', 'transition_success') else 'old'
        require((state / 'collector.runtime').read_text() == expected_runtime, f'{label}: collector runtime', result)
        expected_issuer = 'new' if case in ('restore_failure', 'manager_probe_failure') else 'absent' if case == 'absent_candidate_check_failure' else expected_runtime
        require((state / 'issuer.runtime').read_text() == expected_issuer, f'{label}: issuer runtime', result)
        print(f'[PASS] {label}: exit={result.returncode}, collector={(state / "collector.runtime").read_text()}, issuer={(state / "issuer.runtime").read_text()}')
PY
