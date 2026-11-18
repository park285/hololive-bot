#!/usr/bin/env bash
# quadlet-redeploy-service.sh·quadlet-rollback-service.sh 흐름 테스트. 실제 helper를 비root 테스트 접두어
# (HOLOLIVE_QUADLET_TEST_ROOT) 아래에서 실행하고 podman·systemctl·hostname과 worker-profile-pair.py 호출(python3)만
# 가짜로 바꾼다. pair 도구의 검증은 worker-profile-pair_test.py가 소유한다. 단계 순서, 활성화 전 중단,
# 명시적 복원과 복원 실패, preflight 무변경, 두 소비자의 hold·부분 게시·재개를 확인한다.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
HELPER="${ROOT_DIR}/scripts/deploy/quadlet-redeploy-service.sh"
ROLLBACK="${ROOT_DIR}/scripts/deploy/quadlet-rollback-service.sh"
REAL_PYTHON3="$(command -v python3)"
QUADLET_BIN=/usr/libexec/podman/quadlet
REVISION=0123456789abcdef0123456789abcdef01234567
API_HEX="$(printf 'a%.0s' {1..64})"
WORKER_HEX="$(printf 'b%.0s' {1..64})"
POOLER_HEX="$(printf 'c%.0s' {1..64})"
. "${ROOT_DIR}/scripts/deploy/lib/postgres-capacity.sh"

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

pass() {
  echo "[PASS] $*"
}

[[ "$(id -u)" -ne 0 ]] || fail "run as a non-root user; the helpers refuse the test prefix as root"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
fakebin="${tmp}/fakebin"
mkdir -p "${fakebin}"

cat >"${fakebin}/hostname" <<'EOF'
#!/usr/bin/env bash
echo hololive-seoul
EOF

# image 파일은 tag와 sha256 identity 두 이름으로 같은 속성(id·platform·title·revision)을 담는다.
cat >"${fakebin}/podman" <<'EOF'
#!/usr/bin/env bash
state="${FAKE_STATE:?}"
if [[ "$1 $2" == 'exec -i' ]]; then
  [[ "$3" == holo-postgres ]] || exit 2
  role="${*: -1}"
  printf 'writer-sql %s\n' "$role" >>"$state/calls"
  [[ "${FAKE_CASE}" != "sql_failure_$role" ]] || exit 1
  printf 't\n'
  exit 0
fi
printf 'podman %s\n' "$*" >>"${state}/calls"
image_file() { printf '%s/images/%s' "${state}" "$(printf '%s' "$1" | tr '/:' '__')"; }
case "$1 ${2:-}" in
  'info --format') printf 'arm64\n' ;;
  'inspect -f')
    [[ "${*: -1}" =~ ^([0-9a-f]{64}|hololive-api|hololive-alarm-worker)$ ]] || exit 2
    if [[ "$3" == '{{.Id}}' ]]; then cat "$state/$4/cid"; exit; fi
    cat "${state}/running-endpoint"
    ;;
  'container exists')
    [[ "${FAKE_COORDINATED:-false}" == true ]] || exit 2
    [[ "${FAKE_CASE}" != inventory_failure ]] || exit 125
    [[ "$(<"$state/$3/active")" == true || "${FAKE_CASE}" == residual_container ]]
    ;;
  'container inspect')
    [[ "$3" == --format && "$4" == '{{.State.Running}} {{.Image}}' && "$5" == holo-pgbouncer ]] || exit 2
    cat "$state/pooler-identity" ;;
  'exec holo-pgbouncer')
    [[ "$*" == 'exec holo-pgbouncer sha256sum /etc/pgbouncer/pgbouncer.ini' ]] || exit 2
    [[ ! -e "${state}/pooler-query-failed" ]] || exit 125
    digest="$(sha256sum "${state}/live-pgbouncer.ini")" || exit
    printf '%s  /etc/pgbouncer/pgbouncer.ini\n' "${digest%% *}"
    ;;
  'image exists') [[ -f "$(image_file "$3")" ]] ;;
  'image inspect')
    file="$(image_file "${*: -1}")"
    [[ -f "${file}" ]] || exit 125
    case "$4" in
      '{{.Id}}') key=id ;;
      '{{.Os}}/{{.Architecture}}') key=platform ;;
      *image.title*) key=title ;;
      *image.revision*) key=revision ;;
      *) exit 2 ;;
    esac
    sed -n "s/^${key}=//p" "${file}"
    ;;
  *) exit 2 ;;
esac
EOF

cat >"${fakebin}/journalctl" <<'EOF'
#!/usr/bin/env bash
set -eu
[[ "$1 $2 $3 $4 $5 $6" == '--boot --quiet --no-pager -o json --output-fields=INVOCATION_ID,MESSAGE_ID,JOB_ID,JOB_RESULT' &&
  "$7" =~ ^--since=@[0-9]+\.[0-9]{6}$ && "$8" == _PID=1 && "$9" == UNIT=*.service &&
  "${10}" == INVOCATION_ID=* && "${11}" == JOB_TYPE=stop && "${12}" == MESSAGE_ID=de5b426a63be47a7b6ac3eaac82e2f6f &&
  "${13}" == MESSAGE_ID=9d1aaa27d60140bd96365438aad20286 ]] || exit 2
unit="${9#UNIT=}"
id="${10#INVOCATION_ID=}"
printf 'journal stop %s\n' "$unit" >>"$FAKE_STATE/calls"
[[ "$FAKE_CASE" != journal_failure ]] || exit 1
read -r actual at <"$FAKE_STATE/${unit%.service}/stopping" || exit 1
[[ "$FAKE_CASE" != missing_job && "$id" == "$actual" && ! "$at" < "${7#--since=@}" ]] || exit 0
printf '{"INVOCATION_ID":"%s","MESSAGE_ID":"%s","JOB_ID":"12"}\n' "$actual" "${12#MESSAGE_ID=}"
job=12
[[ "$FAKE_CASE" != other_done_job ]] || job=13
printf '{"INVOCATION_ID":"%s","MESSAGE_ID":"%s","JOB_ID":"%s","JOB_RESULT":"done"}\n' "$actual" "${13#MESSAGE_ID=}" "$job"
EOF

# restart는 설치된 worker-profile drop-in이 고정한 pair로 container를 다시 만든다. 명시적 restart는 NRestarts를 0으로 둔다.
cat >"${fakebin}/systemctl" <<'EOF'
#!/usr/bin/env bash
state="${FAKE_STATE:?}"
# 읽기 전용 manager 조회는 아래의 배포 변경 이벤트와 분리한다.
case "$*" in
  show-environment)
    [[ ! -f "${state}/manager-environment" ]] || cat "${state}/manager-environment"
    exit 0 ;;
  'show --property=UnitPath --value')
    printf '%s\n' "${HOLOLIVE_QUADLET_TEST_ROOT}/etc/systemd/system" "${HOLOLIVE_QUADLET_TEST_ROOT}/custom-systemd"
    exit 0 ;;
  show\ *.service\ --property=NeedDaemonReload\ --value)
    if [[ -e "${state}/manager-reload-required" ]]; then echo yes; else echo no; fi
    exit 0 ;;
esac
printf 'systemctl %s\n' "$*" >>"${state}/calls"
state_dir() {
  if [[ "${FAKE_COORDINATED:-false}" == true ]]; then
    printf '%s/%s' "${state}" "${1%.service}"
  else
    printf '%s' "${state}"
  fi
}
restart_unit() {
  local unit="$1" current dropin pair worker
  current="$(state_dir "${unit}")"
  if [[ "${FAKE_COORDINATED:-false}" == true ]]; then
    [[ "$(<"${current}/loadstate")" != masked ]] || return 1
    [[ ! -e "$current/guard" ]] || return 0
    if [[ "${unit}" == hololive-api.service ]]; then
      worker="$(state_dir hololive-alarm-worker.service)"
      if [[ "$(<"${worker}/active")" != true && "$(<"${worker}/loadstate")" != masked ]]; then
        restart_unit hololive-alarm-worker.service || return
      fi
    fi
  fi
  dropin="${FAKE_QUADLET_DIR}/${unit%.service}.container.d/50-worker-profile.conf"
  pair=none
  [[ ! -f "${dropin}" ]] || pair="$(sed -n 's/^pair=//p' "${dropin}")"
  printf '%s\n' "${pair}" >"${current}/runtime"
  printf '0\n' >"${current}/nrestarts"
  if [[ "${FAKE_SERVER_EPOCH:-old}" == new && "${pair}" != candidate ]]; then
    printf '%s\n' "${unit}" >>"${state}/unsafe-start"
  fi
  if [[ "${FAKE_CASE}" == restore_failure || ( "${FAKE_CASE}" == health_failure && "${pair}" == candidate ) ]]; then
    printf 'false\n' >"${current}/active"
    return 1
  fi
  if [[ "${FAKE_COORDINATED:-false}" == true ]]; then
    printf '%032d\n' 4 >"$current/invocation"
    printf '%064d\n' 4 >"$current/cid"
  fi
  printf 'true\n' >"${current}/active"
  [[ "${FAKE_CASE}" != auto_restart || "${pair}" != candidate ]] || printf '1\n' >"${current}/nrestarts"
}
case "$1" in
  daemon-reload)
    if [[ "${FAKE_COORDINATED:-false}" == true ]]; then
      for service in hololive-api hololive-alarm-worker; do
        if [[ -L "$HOLOLIVE_QUADLET_TEST_ROOT/etc/systemd/system/$service.service" ]]; then
          printf 'masked\n' >"$state/$service/loadstate"
          printf 'masked\n' >"$state/$service/mask_scope"
          # mask를 읽은 관리자가 원래의 비정상 종료 결과를 잃어버리는 상황을 재현한다.
          rm -f "$state/$service/exit" "$state/$service/failed"
        fi
      done
    fi
    ;;
  is-active) [[ "$(cat "$(state_dir "${*: -1}")/active")" == true ]] ;;
  show)
    current="$(state_dir "$2")"
    if [[ "$3" == --all ]]; then
      [[ "$4 $5" == '-p LoadState,ActiveState,InvocationID,Result,ExecMainCode,ExecMainStatus,MainPID,ControlPID' ]] || exit 2
      load="$(<"$current/loadstate")"
      active=inactive main_pid=0 result=success code=0 status=0
      if [[ "$(<"$current/active")" == true ]]; then active=active; main_pid=42; fi
      if [[ "$FAKE_CASE" == residual_pid ]]; then main_pid=99; fi
      if [[ -f "$current/failed" ]]; then active=failed; result=exit-code; fi
      if [[ -f "$current/exit" ]]; then read -r code status <"$current/exit"; fi
      printf 'LoadState=%s\nActiveState=%s\nInvocationID=%s\nResult=%s\nExecMainCode=%s\nExecMainStatus=%s\nMainPID=%s\nControlPID=0\n' \
        "$load" "$active" "$(<"$current/invocation")" "$result" "$code" "$status" "$main_pid"
      exit 0
    fi
    case "$4" in
      NRestarts) cat "${current}/nrestarts" ;;
      LoadState) cat "${current}/loadstate" ;;
      UnitFileState) cat "${current}/mask_scope" ;;
      ActiveState) if [[ "$(<"${current}/active")" == true ]]; then echo active; else echo inactive; fi ;;
      InvocationID) cat "${current}/invocation" ;;
      MainPID) if [[ "$(<"${current}/active")" == true || "${FAKE_CASE}" == residual_pid ]]; then echo 42; else echo 0; fi ;;
      ControlPID) echo 0 ;;
      Result) if [[ -f "$current/failed" ]]; then echo exit-code; else echo success; fi ;;
      *) exit 2 ;;
    esac
    ;;
  stop)
    shift
    for unit in "$@"; do
      current="$(state_dir "$unit")"
      if [[ ! -f "$current/guard" || "$(<"$current/loadstate")" != loaded ]]; then
        printf '%s\n' "$unit" >>"$state/unguarded-stop"
      fi
      printf '%s %s\n' "$(<"$current/invocation")" "$EPOCHREALTIME" >"$current/stopping"
      status=0
      if [[ "$FAKE_CASE" == gc_exit_143 || "$FAKE_CASE" == failed_stop || "$FAKE_CASE" == failed_rehold ]]; then status=143; fi
      printf '%s %s %s\n' "$(<"$current/cid")" "$EPOCHREALTIME" "$status" >"$current/died"
      printf '1 0\n' >"$current/exit"
      if [[ "$FAKE_CASE" == failed_stop || ("$FAKE_CASE" == failed_rehold && "$unit" == hololive-alarm-worker.service) ]]; then
        : >"$current/failed"
        printf '1 143\n' >"$current/exit"
      elif [[ "$FAKE_CASE" == abnormal_success_result ]]; then
        printf '1 143\n' >"$current/exit"
      else
        # 참조하지 않는 정상 종료 unit을 수거하면 서비스 필드는 기본값으로 돌아간다.
        : >"$current/invocation"
        rm -f "$current/exit"
      fi
      printf 'false\n' >"$current/active"
      if [[ "${FAKE_CASE}" == other_stopped_invocation ]]; then printf '%032d\n' 3 >"$current/invocation"; fi
      if [[ "$FAKE_CASE" == peer_changed ]]; then printf '%032d\n' 9 >"$state/hololive-api/invocation"; fi
    done
    [[ "$FAKE_CASE" != stop_command_failed ]]
    ;;
  unmask)
    shift
    if [[ "${1:-}" == --runtime ]]; then shift; fi
    for unit in "$@"; do
      current="$(state_dir "${unit}")"
      rm -- "$HOLOLIVE_QUADLET_TEST_ROOT/etc/systemd/system/$unit" || exit 1
      printf 'loaded\n' >"${current}/loadstate"
      printf 'generated\n' >"${current}/mask_scope"
      if [[ "$FAKE_CASE" == term_after_unmask ]]; then kill -TERM "$PPID"; fi
    done
    ;;
  start)
    [[ "$2" == hololive-db-migrate.service ]] || exit 2
    if [[ "${FAKE_COORDINATED:-false}" == true ]]; then
      for unit in hololive-api hololive-alarm-worker; do
        if [[ "$(<"${state}/${unit}/active")" == true ]]; then
          printf '%s\n' "${unit}" >>"${state}/unsafe-migration"
        fi
      done
    fi
    sed -n 's/^Image=//p' "${FAKE_QUADLET_DIR}/hololive-db-migrate.container.d/50-migrator-image.conf" >"${state}/migrated-with"
    if [[ "${FAKE_CASE}" == fence_during_migration ]]; then
      mkdir -p "${FAKE_FENCE_DIR}"
      : >"${FAKE_FENCE_DIR}/fence.intent"
    fi
    if [[ "$FAKE_CASE" == lost_hold_during_migration ]]; then printf 'true\n' >"$state/hololive-api/active"; fi
    [[ "${FAKE_CASE}" != migration_failure ]]
    ;;
  simulate-reboot)
    for unit in hololive-alarm-worker.service hololive-api.service; do
      current="$(state_dir "${unit}")"
      printf 'false\n' >"${current}/active"
      if [[ "$(<"${current}/mask_scope")" != masked ]]; then
        printf 'loaded\n' >"${current}/loadstate"
        restart_unit "${unit}" || exit
      fi
    done
    ;;
  restart) restart_unit "$2" ;;
  *) exit 2 ;;
esac
EOF

# pair와 정지 가드의 고정 CLI만 대체하고 나머지 파이썬 실행은 그대로 둔다.
cat >"${fakebin}/python3" <<'EOF'
#!/usr/bin/env bash
state="${FAKE_STATE:?}"
if [[ "${1:-} ${2:-}" == '-I -B' && ( "${3:-}" == "${FAKE_PAIR_TOOL%/*}/systemd-stop-guard.py" ||
  "${3:-}" == "${FAKE_STAGE_PAIR_TOOL%/*}/systemd-stop-guard.py" ) ]]; then
  [[ -f "$3" && "$#" == 7 ]] || exit 2
  action="$4" unit="$5" anchor="$6" identity="$7"
  if [[ "$action" == exit-status ]]; then
    current="$state/${unit%.service}"
    printf 'exit-status %s\n' "$unit" >>"$state/calls"
    [[ "$6" =~ ^[0-9a-f]{64}$ && "$7" =~ ^@[0-9]+\.[0-9]{6}$ && "$FAKE_CASE" != missing_exit ]] || exit 1
    read -r cid at code <"$current/died" && [[ "$cid" == "$6" && ! "$at" < "${7#@}" ]] || exit 1
    printf '%s\n' "$code"
    exit 0
  fi
  current="$state/${unit%.service}" mask="${anchor%/*}/$unit"
  printf 'guard %s %s\n' "$action" "$unit" >>"$state/calls"
  [[ "$anchor" == "$HOLOLIVE_QUADLET_TEST_ROOT/etc/systemd/system/."*.mask && "$(readlink "$anchor")" == /dev/null &&
    "$(stat -c '%d:%i' "$anchor")" == "$identity" && ! -L "$mask" && ! -e "$mask" && "$(<"$current/loadstate")" == loaded ]] || exit 1
  case "$action" in
    arm)
      (set -C; printf '%s\n' "$identity" >"$current/guard") || exit 1
      systemctl daemon-reload || exit 1
      if [[ "$FAKE_CASE" == invocation_changed ]]; then printf '%032d\n' 2 >"$current/invocation"; fi
      if [[ "$FAKE_CASE" == lost_guard && "$unit" == hololive-api.service ]]; then rm "$state/hololive-alarm-worker/guard"; fi
      ;;
    check|seal)
      [[ -f "$current/guard" && "$(<"$current/guard")" == "$identity" ]] || exit 1
      if [[ "$action" == seal ]]; then
        [[ "$(<"$current/active")" == false && "$FAKE_CASE" != residual_pid ]] || exit 1
        ln -PT -- "$anchor" "$mask" && systemctl daemon-reload && rm "$current/guard" || exit 1
      fi
      ;;
    *) exit 2 ;;
  esac
  exit 0
fi
[[ "${1:-}" == "${FAKE_PAIR_TOOL}" || "${1:-}" == "${FAKE_STAGE_PAIR_TOOL:-unused}" ]] || exec "${FAKE_REAL_PYTHON3:?}" "$@"
shift
action="$1" service="$2" dir="$3"
printf 'pair %s %s %s\n' "${action}" "${service}" "${dir##*/}" >>"${state}/calls"
unit_dropin="${FAKE_QUADLET_DIR}/${service}.container.d/50-worker-profile.conf"
current="${state}"
if [[ "${FAKE_COORDINATED:-false}" == true ]]; then current="${state}/${service}"; fi
case "${action}" in
  capture|prepare)
    [[ "$4" == "${service}" && "$5" == "${FAKE_DEPLOY_ROOT}" ]] || exit 3
    [[ "$(cat "${current}/active")" == true ]] || exit 3
    mkdir "${dir}"
    cid="$(printf '%064d' 1)"
    if [[ "${FAKE_COORDINATED:-false}" == true ]]; then cid="$(<"$current/cid")"; fi
    printf '{"container_id":"%s"}\n' "$cid" >"${dir}/pair.json"
    printf 'profile\n' >"${dir}/profile.json"
    # 실제 도구는 이전 pair가 기록한 기본 unit(그 image가 돌던 정의)을 이어받는다.
    if [[ -f "${state}/previous-unit" ]]; then
      cp "${state}/previous-unit" "${dir}/unit.container"
    else
      cp "${FAKE_QUADLET_DIR}/${service}.container" "${dir}/unit.container"
    fi
    if [[ "${action}" == prepare ]]; then
      printf 'pair=previous\n' >"${dir}/50-worker-profile.conf"
      mkdir -p "${unit_dropin%/*}"
      cp "${dir}/50-worker-profile.conf" "${unit_dropin}"
    fi
    ;;
  rollback)
    [[ -f "${dir}/profile.json" ]] || exit 3
    [[ "$4" == "${dir%/*}/rollback.conf" ]] || exit 3
    printf 'pair=previous\napp_version=captured\n' >"$4"
    ;;
  candidate)
    expected="${FAKE_CANDIDATE_ID}"
    if [[ "${FAKE_COORDINATED:-false}" == true ]]; then
      expected="${FAKE_WORKER_ID}"
      [[ "${service}" != hololive-api ]] || expected="${FAKE_API_ID}"
    fi
    [[ "$4" == "${expected}" ]] || exit 3
    grep -q '^\[Container\]' || exit 3
    mkdir "${dir}"
    printf 'profile\n' >"${dir}/profile.json"
    printf 'pair=candidate\n' >"${dir}/50-worker-profile.conf"
    ;;
  check)
    [[ -f "${dir}/profile.json" ]] || exit 3
    [[ "${FAKE_CASE}" != candidate_check_failure || "${dir##*/}" != candidate ]]
    ;;
  verify|verify-source) [[ -f "${dir}/profile.json" ]] ;;
  verify-dropin)
    [[ -f "$4" && "$(<"$4")" == $'pair=previous\napp_version=captured' ]] ;;
  verify-captured)
    [[ "$4" == "${service}" && "$(<"${current}/runtime")" == baseline && "$(<"${current}/active")" == true &&
      "${FAKE_CASE}" != generation_changed ]] ;;
  verify-retained)
    [[ -f "${dir}/profile.json" && -f "$4/profile.json" && "$5" == "${FAKE_DEPLOY_ROOT}" ]] || exit 3
    if grep -qx 'pair=previous' "${unit_dropin}"; then
      cmp -s "$dir/50-worker-profile.conf" "$unit_dropin" || exit 3
    fi
    retained="$(sed -n 's/^pair=//p' "${unit_dropin}")"
    [[ "${retained}" == baseline || "${retained}" == previous || "${retained}" == candidate ]]
    ;;
  activate)
    [[ "$4" == "${FAKE_DEPLOY_ROOT}" ]] || exit 3
    [[ "${FAKE_CASE}" != api_activation_failure || "${service}" != hololive-api ]] || exit 1
    mkdir -p "${unit_dropin%/*}"
    if [[ "${dir##*/}" == previous ]]; then
      [[ "$#" == 5 && "$(<"$5")" == $'pair=previous\napp_version=captured' ]] || exit 3
      if [[ "$5" != "${dir}/50-worker-profile.conf" ]]; then cp "$5" "${dir}/50-worker-profile.conf"; fi
    fi
    cp "${dir}/50-worker-profile.conf" "${unit_dropin}"
    ;;
  verify-live)
    [[ "${FAKE_CASE}" != "verify_failure_${service}" ]] || exit 1
    [[ "${FAKE_CASE}" != failed_rehold || "$service" != hololive-alarm-worker ]] || exit 1
    expected="${dir##*/}"
    if [[ "${FAKE_COORDINATED:-false}" == true && "${expected}" == previous ]]; then expected=baseline; fi
    [[ "$4" == "${service}" && "$(cat "${current}/active")" == true &&
      "$(cat "${current}/runtime")" == "${expected}" ]] || exit 1
    if [[ "$FAKE_CASE" == worker_cid_changed && "$service" == hololive-alarm-worker ]]; then printf '%064d\n' 9 >"$current/cid"; fi
    ;;
  *) exit 2 ;;
esac
EOF
chmod +x "${fakebin}"/*

add_image() {
  local root="$1" ref="$2" hex="$3" title="$4" revision="$5" name
  for name in "${ref}" "sha256:${hex}"; do
    printf 'id=%s\nplatform=linux/arm64\ntitle=%s\nrevision=%s\n' "${hex}" "${title}" "${revision}" \
      >"${root}/state/images/$(printf '%s' "${name}" | tr '/:' '__')"
  done
}

# case root: 배포 트리 revision 기록, 설치된 unit, compose.env, 적재된 image, 실행 중 unit 상태를 만든다.
make_case() {
  local root="$1" service="$2" with_dropin="$3" api_revision="${4:-${REVISION}}" quadlet_dir
  quadlet_dir="${root}/etc/containers/systemd/hololive-bot"
  mkdir -p "${root}/opt/hololive-bot/compose/current" "${quadlet_dir}" "${root}/etc/stack-secrets/hololive-bot" \
    "${root}/etc/systemd/system" \
    "${root}/state/images"
  printf '%s\n' "${REVISION}" >"${root}/opt/hololive-bot/compose/current/.source-revision"
  cp "${ROOT_DIR}/deploy/quadlet/hololive-bot/"*.container "${quadlet_dir}/"
  printf 'HOLOLIVE_VALKEY_HA_ENABLED=1\n' >"${root}/etc/stack-secrets/hololive-bot/compose.env"
  postgres_capacity_write_pgbouncer_config "${ROOT_DIR}/scripts/ci/postgres-capacity-policy.tsv" \
    "${root}/etc/stack-secrets/hololive-bot/compose.env" "${root}/state/live-pgbouncer.ini" >/dev/null
  if [[ "${with_dropin}" == true ]]; then
    mkdir -p "${quadlet_dir}/${service}.container.d"
    printf 'pair=baseline\n' >"${quadlet_dir}/${service}.container.d/50-worker-profile.conf"
  fi
  add_image "${root}" localhost/hololive-api:candidate "${API_HEX}" hololive-api "${api_revision}"
  add_image "${root}" localhost/hololive-alarm-worker:candidate "${WORKER_HEX}" hololive-alarm-worker "${REVISION}"
  printf 'true\n' >"${root}/state/active"
  printf 'POSTGRES_HOST=holo-pgbouncer\nPOSTGRES_PORT=6432\n' >"$root/state/running-endpoint"
  printf '3\n' >"${root}/state/nrestarts"
  printf 'baseline\n' >"${root}/state/runtime"
  : >"${root}/state/calls"
}

run_tool() {
  local root="$1" case_name="$2" candidate_hex="$3"
  shift 3
  env PATH="${fakebin}:${PATH}" HOLOLIVE_QUADLET_TEST_ROOT="${root}" FAKE_STATE="${root}/state" FAKE_CASE="${case_name}" \
    FAKE_PAIR_TOOL="${ROOT_DIR}/scripts/deploy/lib/worker-profile-pair.py" \
    FAKE_STAGE_PAIR_TOOL="${root}/opt/hololive-bot/compose/staged/${REVISION}/scripts/deploy/lib/worker-profile-pair.py" \
    FAKE_REAL_PYTHON3="${REAL_PYTHON3}" \
    FAKE_QUADLET_DIR="${root}/etc/containers/systemd/hololive-bot" \
    FAKE_FENCE_DIR="${root}/var/lib/hololive-postgres-fence" \
    FAKE_DEPLOY_ROOT="$(realpath -m "${root}/opt/hololive-bot/compose/current")" \
    FAKE_CANDIDATE_ID="sha256:${candidate_hex}" \
    FAKE_COORDINATED="${FAKE_COORDINATED:-false}" FAKE_SERVER_EPOCH="${FAKE_SERVER_EPOCH:-old}" \
    FAKE_API_ID="sha256:${API_HEX}" FAKE_WORKER_ID="sha256:${WORKER_HEX}" \
    bash "$@" >"${root}/out" 2>&1
}

installed_pair() {
  sed -n 's/^pair=//p' "$1/etc/containers/systemd/hololive-bot/$2.container.d/50-worker-profile.conf"
}

# 기대한 호출이 이 순서로 나타나는지 본다(사이의 다른 호출은 허용).
assert_order() {
  local root="$1" label="$2" line=0 expected found
  shift 2
  for expected in "$@"; do
    # 일치가 없을 때 grep의 1이 pipefail로 테스트를 조용히 끝내지 않게 한다. 판정은 아래 빈 값 검사가 한다.
    found="$(grep -nxF -- "${expected}" "${root}/state/calls" | awk -F: -v after="${line}" '$1 > after { print $1; exit }' || true)"
    [[ -n "${found}" ]] || { cat "${root}/state/calls" "${root}/out" >&2; fail "${label}: missing or out of order: ${expected}"; }
    line="${found}"
  done
}

assert_no_call() {
  local root="$1" label="$2" pattern="$3"
  ! grep -qE -- "${pattern}" "${root}/state/calls" || { cat "${root}/state/calls" >&2; fail "${label}: unexpected call matching ${pattern}"; }
}

release_of() {
  find "$1/opt/hololive-bot/compose/current/artifacts/worker-profile/releases" -mindepth 1 -maxdepth 1 -type d
}

# 1. 정상 교체: 이전 pair 보존 → candidate 검사 → candidate migrator로 migration → 활성화·restart → verify-live.
root="${tmp}/success"
make_case "${root}" hololive-api true
run_tool "${root}" success "${API_HEX}" "${HELPER}" hololive-api localhost/hololive-api:candidate ||
  { cat "${root}/out" >&2; fail "success: helper failed"; }
assert_order "${root}" success \
  'pair capture hololive-api previous' 'pair rollback hololive-api previous' 'pair check hololive-api previous' \
  'pair candidate hololive-api candidate' 'pair check hololive-api candidate' 'pair verify-source hololive-api candidate' \
  'systemctl daemon-reload' 'systemctl start hololive-db-migrate.service' \
  'writer-sql hololive_runtime' 'pair activate hololive-api candidate' 'systemctl daemon-reload' 'systemctl restart hololive-api.service' \
  'pair verify-live hololive-api candidate'
assert_no_call "${root}" success '^pair (prepare|activate hololive-api previous)'
[[ "$(installed_pair "${root}" hololive-api)" == candidate && "$(cat "${root}/state/runtime")" == candidate ]] ||
  fail "success: candidate pair is not active"
[[ "$(cat "${root}/state/migrated-with")" == "sha256:${API_HEX}" ]] || fail "success: migration did not use the candidate image"
grep -q '^\[OUTCOME\] deployed service=hololive-api' "${root}/out" || fail "success: outcome line missing"
success_release="$(release_of "${root}")"
[[ "$(cat "${success_release}/nrestarts-before")" == 3 ]] || fail "success: NRestarts baseline was not recorded"
if [[ -x "${QUADLET_BIN}" ]]; then
  # helper가 쓴 migrator drop-in이 실제 Quadlet 생성에서 compose.env 기반 ${HOLOLIVE_API_IMAGE}를 대신하는지 본다.
  units="${tmp}/success-units"
  mkdir -p "${units}"
  cp "${ROOT_DIR}/deploy/quadlet/hololive-bot/"* "${units}/"
  cp -R "${root}/etc/containers/systemd/hololive-bot/hololive-db-migrate.container.d" "${units}/"
  exec_start="$(QUADLET_UNIT_DIRS="${units}" "${QUADLET_BIN}" -dryrun 2>/dev/null |
    grep -E '^ExecStart=.* --name hololive-db-migrate ')" || fail "success: hololive-db-migrate did not generate"
  [[ "${exec_start##* }" == "sha256:${API_HEX}" ]] || fail "success: generated migrator image is ${exec_start##* }"
fi
pass "deploy retains the previous pair, migrates with the candidate image before activation and verifies the live pair"

# 2. 첫 도입(drop-in 없음)·alarm-worker: prepare로 실행 중 image를 먼저 고정하고 같은 revision의 API image로 migration한다.
root="${tmp}/first-adoption"
make_case "${root}" hololive-alarm-worker false
run_tool "${root}" first_adoption "${WORKER_HEX}" "${HELPER}" hololive-alarm-worker \
  localhost/hololive-alarm-worker:candidate localhost/hololive-api:candidate ||
  { cat "${root}/out" >&2; fail "first adoption: helper failed"; }
assert_order "${root}" "first adoption" \
  'pair prepare hololive-alarm-worker previous' 'systemctl daemon-reload' 'pair candidate hololive-alarm-worker candidate' \
  'systemctl start hololive-db-migrate.service' 'pair activate hololive-alarm-worker candidate' \
  'systemctl restart hololive-alarm-worker.service' 'pair verify-live hololive-alarm-worker candidate'
assert_no_call "${root}" "first adoption" '^pair capture '
[[ "$(cat "${root}/state/migrated-with")" == "sha256:${API_HEX}" ]] || fail "first adoption: migration did not use the API image"
[[ "$(installed_pair "${root}" hololive-alarm-worker)" == candidate ]] || fail "first adoption: candidate pair is not active"
pass "first adoption pins the running image with prepare and migrates with the same-revision API image"

# 3. migration 실패, 또는 migration과 겹친 fence(condition으로 건너뛰어도 start는 0): 활성화 전에 멈추고
# 실행 중 pair·unit을 바꾸지 않는다.
for case_name in migration_failure fence_during_migration; do
  root="${tmp}/${case_name}"
  make_case "${root}" hololive-api true
  if run_tool "${root}" "${case_name}" "${API_HEX}" "${HELPER}" hololive-api localhost/hololive-api:candidate; then
    fail "${case_name}: helper succeeded"
  fi
  assert_no_call "${root}" "${case_name}" '^(pair activate |systemctl restart )'
  [[ "$(installed_pair "${root}" hololive-api)" == baseline && "$(cat "${root}/state/runtime")" == baseline ]] ||
    fail "${case_name}: running pair changed"
  grep -q '^\[OUTCOME\] not_activated service=hololive-api stage=migration' "${root}/out" ||
    { cat "${root}/out" >&2; fail "${case_name}: outcome line missing"; }
  pass "${case_name}: the change stops before activation and leaves the running pair"
done

# 4·5. 활성화 뒤 실패(restart health gate 실패, verify 중 자동 재시작)은 자동으로
# 되돌리지 않는다. 이번 배포가 migration을 적용했을 수 있으므로 이전 image 복원은 스키마·admission 호환성
# 승인 뒤 운영자가 quadlet-rollback-service.sh로 한다. 승인 뒤 --apply는 이전 pair를 되살려 다시 확인한다.
for case_name in health_failure auto_restart; do
  root="${tmp}/${case_name}"
  make_case "${root}" hololive-api true
  if run_tool "${root}" "${case_name}" "${API_HEX}" "${HELPER}" hololive-api localhost/hololive-api:candidate; then
    fail "${case_name}: helper succeeded"
  fi
  assert_order "${root}" "${case_name}" 'pair activate hololive-api candidate' 'systemctl restart hololive-api.service'
  assert_no_call "${root}" "${case_name}" '^pair activate hololive-api previous'
  [[ "$(installed_pair "${root}" hololive-api)" == candidate ]] || fail "${case_name}: helper replaced the candidate pair by itself"
  grep -q '^\[OUTCOME\] failed_after_activation service=hololive-api .*compatibility is approved' "${root}/out" ||
    { cat "${root}/out" >&2; fail "${case_name}: outcome line missing"; }
  release="$(release_of "${root}")"
  : >"${root}/state/calls"
  run_tool "${root}" "${case_name}" "${API_HEX}" "${ROLLBACK}" "${release}" --apply ||
    { cat "${root}/out" >&2; fail "${case_name}: approved rollback failed"; }
  assert_order "${root}" "${case_name} rollback" \
    'pair verify hololive-api previous' 'pair check hololive-api previous' 'pair activate hololive-api previous' \
    'systemctl daemon-reload' 'systemctl restart hololive-api.service' 'pair verify-live hololive-api previous'
  [[ "$(installed_pair "${root}" hololive-api)" == previous && "$(cat "${root}/state/runtime")" == previous ]] ||
    fail "${case_name}: approved rollback did not restore the previous pair"
  pass "${case_name}: a failure after activation is reported without automatic rollback; the approved rollback restores the previous pair"
done

# 5-1. 이번 revision이 기본 unit 정의도 바꿨다면(sync가 먼저 설치), 롤백은 이전 image를 그 image가 돌던 unit으로 띄운다.
root="${tmp}/unit-changed"
make_case "${root}" hololive-api true
unit_file="${root}/etc/containers/systemd/hololive-bot/hololive-api.container"
cp "${unit_file}" "${root}/state/new-unit"
{ printf '# previous revision definition\n'; cat "${unit_file}"; } >"${root}/state/previous-unit"
if run_tool "${root}" health_failure "${API_HEX}" "${HELPER}" hololive-api localhost/hololive-api:candidate; then
  fail "unit changed: helper succeeded"
fi
cmp -s "${unit_file}" "${root}/state/new-unit" || fail "unit changed: helper changed the base unit without an approved rollback"
run_tool "${root}" health_failure "${API_HEX}" "${ROLLBACK}" "$(release_of "${root}")" --apply ||
  { cat "${root}/out" >&2; fail "unit changed: approved rollback failed"; }
cmp -s "${unit_file}" "${root}/state/previous-unit" ||
  fail "unit changed: rollback kept the new base unit definition with the previous image"
cmp -s "$(release_of "${root}")/rollback-replaced-hololive-api.container" "${root}/state/new-unit" ||
  fail "unit changed: the replaced base unit was not retained in the release"
pass "a rollback restores the base unit definition the previous image ran with"

for endpoint_case in changed override restored missing invalid unresolved; do
  root="${tmp}/rollback-endpoint-${endpoint_case}"
  make_case "${root}" hololive-api true
  unit_file="${root}/etc/containers/systemd/hololive-bot/hololive-api.container"
  sed -e 's/^Environment=HOLOLIVE_CENTRAL_POSTGRES_HOST=.*/Environment=HOLOLIVE_CENTRAL_POSTGRES_HOST=holo-postgres/' \
      -e 's/^Environment=HOLOLIVE_CENTRAL_POSTGRES_PORT=.*/Environment=HOLOLIVE_CENTRAL_POSTGRES_PORT=5432/' \
      "${unit_file}" > "${root}/state/previous-unit"
  release="$root/opt/hololive-bot/compose/current/artifacts/worker-profile/releases/prior"
  mkdir -p "$release/previous"
  printf 'hololive-api\n' >"$release/service"
  printf 'profile\n' >"$release/previous/profile.json"
  printf 'pair=previous\napp_version=captured\n' >"$release/previous/50-worker-profile.conf"
  cp "$root/state/previous-unit" "$release/previous/unit.container"
  case "${endpoint_case}" in
    override) printf 'HOLOLIVE_CENTRAL_POSTGRES_HOST=holo-pgbouncer\nHOLOLIVE_CENTRAL_POSTGRES_PORT=6432\n' >> "${root}/etc/stack-secrets/hololive-bot/compose.env" ;;
    restored)
      printf 'HOLOLIVE_CENTRAL_POSTGRES_HOST=holo-postgres\nHOLOLIVE_CENTRAL_POSTGRES_PORT=5432\n' >> "${root}/etc/stack-secrets/hololive-bot/compose.env"
      rm "${root}/state/live-pgbouncer.ini" ;;
    missing) printf 'HOLOLIVE_CENTRAL_POSTGRES_HOST=\n' >> "${root}/etc/stack-secrets/hololive-bot/compose.env" ;;
    invalid) printf 'HOLOLIVE_CENTRAL_POSTGRES_PORT=65536\n' >> "${root}/etc/stack-secrets/hololive-bot/compose.env" ;;
    unresolved) printf 'HOLOLIVE_CENTRAL_POSTGRES_HOST=${UNKNOWN_ENDPOINT}\n' >> "${root}/etc/stack-secrets/hololive-bot/compose.env" ;;
  esac
  for rollback_mode in --check --apply; do
    : > "${root}/state/calls"
    if [[ "${endpoint_case}" == override || "${endpoint_case}" == restored ]]; then
      run_tool "${root}" health_failure "${API_HEX}" "${ROLLBACK}" "$(release_of "${root}")" "${rollback_mode}" ||
        { cat "${root}/out" >&2; fail "same effective endpoint rollback rejected: ${endpoint_case}"; }
    else
      if run_tool "${root}" health_failure "${API_HEX}" "${ROLLBACK}" "$(release_of "${root}")" "${rollback_mode}"; then
        fail "unsafe endpoint rollback accepted: ${endpoint_case}"
      fi
      assert_no_call "${root}" "endpoint rollback rejected" '^(pair activate |systemctl )'
      [[ -f "$(release_of "${root}")/previous/unit.container" ]] || fail "rollback recovery unit lost"
    fi
  done
done
pass "rollback compares effective endpoints, preserves recovery pairs and honors coordinated configuration restoration"

# 5-2. 다른 helper가 실행 중이면 아무것도 바꾸지 않고 멈춘다(migrator drop-in·daemon-reload·migration 경합 방지).
root="${tmp}/concurrent"
make_case "${root}" hololive-api true
mkdir -p "${root}/run/lock"
exec 8>"${root}/run/lock/hololive-quadlet-redeploy.lock"
flock 8
if run_tool "${root}" success "${API_HEX}" "${HELPER}" hololive-api localhost/hololive-api:candidate; then
  fail "concurrent: helper ran while another held the lock"
fi
grep -q 'another central Quadlet writer is running' "${root}/out" || { cat "${root}/out" >&2; fail "concurrent: lock message missing"; }
[[ ! -s "${root}/state/calls" ]] || { cat "${root}/state/calls" >&2; fail "concurrent: helper made calls while locked out"; }
pass "a second helper refuses to run while another holds the redeploy lock"
if run_tool "${root}" success "${API_HEX}" "${ROLLBACK}" "${root}/opt/hololive-bot/compose/current/artifacts/worker-profile/releases/missing" --apply; then
  fail "concurrent: rollback ran while another writer held the lock"
fi
grep -q 'another central Quadlet writer is running' "${root}/out" || fail "concurrent: rollback lock message missing"
[[ ! -s "${root}/state/calls" ]] || fail "concurrent: rollback made calls while locked out"
exec 8>&-

# 6. 승인된 되돌리기도 실패: rollback이 실패로 끝나고 release pair를 남긴다.
root="${tmp}/restore-failure"
make_case "${root}" hololive-api true
if run_tool "${root}" restore_failure "${API_HEX}" "${HELPER}" hololive-api localhost/hololive-api:candidate; then
  fail "restore failure: helper succeeded"
fi
release="$(release_of "${root}")"
if run_tool "${root}" restore_failure "${API_HEX}" "${ROLLBACK}" "${release}" --apply; then
  fail "restore failure: rollback reported success"
fi
[[ -f "${release}/previous/profile.json" && -f "${release}/candidate/profile.json" ]] ||
  fail "restore failure: release pairs were not retained"
pass "a failed restoration fails visibly and retains the release pairs"

# 7·8. preflight 거부(배포 트리와 다른 migrator revision, 연결 예산 초과): pair·unit·release 저장소를 건드리지 않는다.
root="${tmp}/revision-mismatch"
make_case "${root}" hololive-alarm-worker true fedcba9876543210fedcba9876543210fedcba98
if run_tool "${root}" revision_mismatch "${WORKER_HEX}" "${HELPER}" hololive-alarm-worker \
  localhost/hololive-alarm-worker:candidate localhost/hololive-api:candidate; then
  fail "revision mismatch: helper succeeded"
fi
root_capacity="${tmp}/capacity"
make_case "${root_capacity}" hololive-api true
printf 'BOT_POSTGRES_POOL_MAX_CONNS=40\n' >>"${root_capacity}/etc/stack-secrets/hololive-bot/compose.env"
if run_tool "${root_capacity}" capacity "${API_HEX}" "${HELPER}" hololive-api localhost/hololive-api:candidate; then
  fail "capacity: helper succeeded"
fi
for root in "${root}" "${root_capacity}"; do
  assert_no_call "${root}" preflight '^(pair |systemctl (daemon-reload|start|restart))'
  [[ ! -e "${root}/opt/hololive-bot/compose/current/artifacts" ]] || fail "preflight: release storage was created"
  grep -q '^\[OUTCOME\] not_activated .* stage=preflight' "${root}/out" || fail "preflight: outcome line missing"
done
pass "preflight rejects a revision mismatch and an exhausted connection budget before any change"

for capacity_case in compose unit_default container_override missing query_failed; do
  root="${tmp}/live-capacity-${capacity_case}"
  make_case "${root}" hololive-api true
  unit_dir="${root}/etc/containers/systemd/hololive-bot"
  case "${capacity_case}" in
    compose) printf 'ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n' >>"${root}/etc/stack-secrets/hololive-bot/compose.env" ;;
    unit_default)
      printf '[Service]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n' >"${unit_dir}/hololive-api.container.d/60-pool.conf" ;;
    container_override)
      printf '[Container]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n' >"${unit_dir}/hololive-api.container.d/60-pool.conf" ;;
    missing) rm "${root}/state/live-pgbouncer.ini" ;;
    query_failed) touch "${root}/state/pooler-query-failed" ;;
  esac
  if run_tool "${root}" success "${API_HEX}" "${HELPER}" hololive-api localhost/hololive-api:candidate; then
    fail "${capacity_case}: live pooler mismatch accepted"
  fi
  assert_no_call "${root}" capacity '^(pair |systemctl (daemon-reload|start|restart|stop))'
  [[ "$(installed_pair "${root}" hololive-api)" == baseline && ! -e "${root}/opt/hololive-bot/compose/current/artifacts" ]] ||
    fail "${capacity_case}: capacity rejection changed deployment state"
done
pass "live capacity drift and failed inspection stop before deployment effects"

# 정확한 unit drop-in 외의 유효 원천은 parser가 알던 예산으로 통과시키지 않는다.
for origin in \
  etc/containers/systemd/hololive-bot/container.d/60-pool.conf \
  etc/containers/systemd/hololive-bot/hololive-.container.d/60-pool.conf \
  etc/containers/systemd/hololive-bot/hololive-youtube-.container.d/60-pool.conf \
  etc/containers/systemd/hololive-bot/holo-.container.d/60-pool.conf \
  etc/containers/systemd/container.d/60-pool.conf \
  run/containers/systemd/hololive-api.container.d/60-pool.conf \
  run/containers/systemd/users/hololive-.container.d/60-pool.conf \
  usr/share/containers/systemd/nested/hololive-.container.d/60-pool.conf \
  run/containers/systemd/nested/hololive-api.container \
  etc/systemd/system/hololive-api.service.d/60-pool.conf \
  run/systemd/system/hololive-.service.d/60-pool.conf \
  usr/lib/systemd/system/service.d/60-pool.conf \
  run/systemd/generator/hololive-api.service.d/60-pool.conf \
  run/systemd/generator.early/hololive-api.service \
  custom-systemd/hololive-api.service.d/60-pool.conf \
  etc/systemd/system/hololive-api.service; do
  root="${tmp}/override-origin-${origin//\//_}"
  make_case "${root}" hololive-api true
  mkdir -p "$(dirname "${root}/${origin}")"
  section=Container
  case "${origin}" in
    */systemd/system/*|*/systemd/generator*/*|custom-systemd/*) section=Service ;;
  esac
  printf '[%s]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n' "${section}" >"${root}/${origin}"
  if run_tool "${root}" success "${API_HEX}" "${HELPER}" hololive-api localhost/hololive-api:candidate; then
    fail "${origin}: unsupported effective origin accepted"
  fi
  grep -q 'unsupported Quadlet/systemd override origin:' "${root}/out" ||
    { cat "${root}/out" >&2; fail "${origin}: origin diagnostic missing"; }
  assert_no_call "${root}" origin '^(pair |systemctl (daemon-reload|start|restart|stop|mask|unmask))'
  [[ "$(installed_pair "${root}" hololive-api)" == baseline &&
    ! -e "${root}/state/migrated-with" &&
    ! -e "${root}/opt/hololive-bot/compose/current/artifacts" ]] ||
    fail "${origin}: origin rejection changed deployment state"
done
for manager_case in manager-environment manager-reload-required; do
  root="${tmp}/override-${manager_case}"
  make_case "${root}" hololive-api true
  printf 'QUADLET_UNIT_DIRS=/unreviewed\n' >"${root}/state/${manager_case}"
  if run_tool "${root}" success "${API_HEX}" "${HELPER}" hololive-api localhost/hololive-api:candidate; then
    fail "${manager_case}: unknown manager state accepted"
  fi
  grep -q 'unsupported Quadlet/systemd override origin:' "${root}/out" ||
    { cat "${root}/out" >&2; fail "manager origin diagnostic missing"; }
  assert_no_call "${root}" origin '^(pair |systemctl (daemon-reload|start|restart|stop|mask|unmask))'
  [[ ! -e "${root}/state/migrated-with" && ! -e "${root}/opt/hololive-bot/compose/current/artifacts" ]] ||
    fail "${manager_case}: manager rejection changed deployment state"
done
pass "inherited, cross-root and generated-service origins fail before migration, stop or pair publication"

for directive in \
  'PodmanArgs=--env ADMIN_API_POSTGRES_POOL_MAX_CONNS=5' \
  'GlobalArgs=--module=/unreviewed.conf' \
  'EnvironmentHost=true'; do
  root="${tmp}/override-directive-${directive%%=*}"
  make_case "${root}" hololive-api true
  printf '[Container]\n%s\n' "${directive}" \
    >"${root}/etc/containers/systemd/hololive-bot/hololive-api.container.d/60-pool.conf"
  if run_tool "${root}" success "${API_HEX}" "${HELPER}" hololive-api localhost/hololive-api:candidate; then
    fail "${directive%%=*}: unknown execution environment accepted"
  fi
  grep -q 'unsupported Quadlet execution override:' "${root}/out" ||
    { cat "${root}/out" >&2; fail "execution override diagnostic missing"; }
  assert_no_call "${root}" origin '^(pair |systemctl (daemon-reload|start|restart|stop))'
done

for level in s0:c900,c901 s0:c902,c903; do
  root="${tmp}/neutral-security-${level}"
  make_case "${root}" hololive-api true
  security_dir="${root}/etc/containers/systemd/hololive-api.container.d"
  mkdir -p "${security_dir}"
  printf '[Container]\nSecurityLabelLevel=%s\n' "${level}" >"${security_dir}/70-security.conf"
  cp "${security_dir}/70-security.conf" "${root}/state/retained-security.conf"
  run_tool "${root}" success "${API_HEX}" "${HELPER}" hololive-api localhost/hololive-api:candidate ||
    { cat "${root}/out" >&2; fail "neutral cross-root MCS drop-in rejected"; }
  cmp -s "${security_dir}/70-security.conf" "${root}/state/retained-security.conf" ||
    fail "deployment rewrote the host MCS policy"
  [[ "$(installed_pair "${root}" hololive-api)" == candidate ]] || fail "neutral MCS deployment did not activate"
done
root="${tmp}/mixed-security-capacity"
make_case "${root}" hololive-api true
security_dir="${root}/etc/containers/systemd/hololive-api.container.d"
mkdir -p "${security_dir}"
printf '[Container]\nSecurityLabelLevel=s0:c900,c901\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n' \
  >"${security_dir}/70-security.conf"
if run_tool "${root}" success "${API_HEX}" "${HELPER}" hololive-api localhost/hololive-api:candidate; then
  fail "mixed security/capacity cross-root drop-in accepted"
fi
grep -q 'unsupported Quadlet/systemd override origin:' "${root}/out" ||
  { cat "${root}/out" >&2; fail "mixed security/capacity origin diagnostic missing"; }
assert_no_call "${root}" origin '^(pair |systemctl (daemon-reload|start|restart|stop))'
[[ ! -e "${root}/state/migrated-with" && ! -e "${root}/opt/hololive-bot/compose/current/artifacts" ]] ||
  fail "mixed security/capacity rejection changed deployment state"
pass "neutral cross-root MCS policies survive deployment unchanged; mixed capacity settings stop before mutations"

for source in base dropin; do
  for directive in 'Exec=/tmp/other.ini' 'Entrypoint=/bin/sh'; do
    root="${tmp}/pooler-command-${source}-${directive%%=*}"
    make_case "${root}" hololive-api true
    pooler_unit="${root}/etc/containers/systemd/hololive-bot/holo-pgbouncer.container"
    if [[ "${source}" == dropin ]]; then
      mkdir -p "${pooler_unit}.d"
      pooler_unit="${pooler_unit}.d/60-command.conf"
    fi
    printf '\n[Container]\n%s\n' "${directive}" >>"${pooler_unit}"
    if run_tool "${root}" success "${API_HEX}" "${HELPER}" hololive-api localhost/hololive-api:candidate; then
      fail "pooler ${source} ${directive%%=*} bypassed the fixed config command"
    fi
    grep -q 'unsupported Quadlet pooler command override:' "${root}/out" ||
      { cat "${root}/out" >&2; fail "pooler command diagnostic missing"; }
    assert_no_call "${root}" command '^(pair |systemctl (daemon-reload|start|restart|stop))'
    [[ ! -e "${root}/state/migrated-with" && ! -e "${root}/opt/hololive-bot/compose/current/artifacts" ]] ||
      fail "pooler command rejection changed deployment state"
  done
done
pass "pooler base/drop-in command changes stop before migration or stop despite a matching live config hash"

if [[ -x "${QUADLET_BIN}" ]]; then
  # 실제 생성기의 결과가 5임을 확인하여 텍스트 유사성 검사가 아니라 구체적 우회를 재현한다.
  units="${tmp}/inherited-capacity-units"
  mkdir -p "${units}/hololive-.container.d"
  cp "${ROOT_DIR}/deploy/quadlet/hololive-bot/"* "${units}/"
  printf '[Container]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n' \
    >"${units}/hololive-.container.d/60-pool.conf"
  exec_start="$(QUADLET_UNIT_DIRS="${units}" "${QUADLET_BIN}" -dryrun 2>/dev/null |
    grep -E '^ExecStart=.* --name hololive-api ')" || fail "inherited capacity unit did not generate"
  [[ "${exec_start}" == *'ADMIN_API_POSTGRES_POOL_MAX_CONNS=5'* ]] ||
    fail "actual Quadlet did not apply inherited capacity override"
  printf '[Container]\nSecurityLabelLevel=s0:c900,c901\n' >"${units}/hololive-.container.d/60-pool.conf"
  exec_start="$(QUADLET_UNIT_DIRS="${units}" "${QUADLET_BIN}" -dryrun 2>/dev/null |
    grep -E '^ExecStart=.* --name hololive-api ')" || fail "neutral inherited MCS unit did not generate"
  [[ "${exec_start}" == *'label=level:s0:c900,c901'* &&
    "${exec_start}" != *'ADMIN_API_POSTGRES_POOL_MAX_CONNS=5'* ]] ||
    fail "actual Quadlet did not retain only the neutral MCS setting"
fi

root="${tmp}/live-capacity-matching"
make_case "${root}" hololive-api true
printf 'ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n' >>"${root}/etc/stack-secrets/hololive-bot/compose.env"
postgres_capacity_write_pgbouncer_config "${ROOT_DIR}/scripts/ci/postgres-capacity-policy.tsv" \
  "${root}/etc/stack-secrets/hololive-bot/compose.env" "${root}/state/live-pgbouncer.ini" >/dev/null
run_tool "${root}" success "${API_HEX}" "${HELPER}" hololive-api localhost/hololive-api:candidate ||
  { cat "${root}/out" >&2; fail "matching increased live capacity rejected"; }
[[ "$(installed_pair "${root}" hololive-api)" == candidate ]] || fail "matching capacity did not activate"

for capacity_case in unit_default compose_override dropin_override missing query_failed; do
  root="${tmp}/rollback-capacity-${capacity_case}"
  make_case "${root}" hololive-api true
  unit_file="${root}/etc/containers/systemd/hololive-bot/hololive-api.container"
  { cat "${unit_file}"; printf '\n[Service]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n'; } >"${root}/state/previous-unit"
  run_tool "${root}" success "${API_HEX}" "${HELPER}" hololive-api localhost/hololive-api:candidate ||
    { cat "${root}/out" >&2; fail "rollback capacity fixture deployment failed"; }
  release="$(release_of "${root}")"
  case "${capacity_case}" in
    compose_override) printf 'ADMIN_API_POSTGRES_POOL_MAX_CONNS=4\n' >>"${root}/etc/stack-secrets/hololive-bot/compose.env" ;;
    dropin_override) printf '[Service]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=4\n' >"${unit_file}.d/60-pool.conf" ;;
    missing) rm "${root}/state/live-pgbouncer.ini" ;;
    query_failed) touch "${root}/state/pooler-query-failed" ;;
  esac
  for rollback_mode in --check --apply; do
    : >"${root}/state/calls"
    if [[ "${capacity_case}" == compose_override || "${capacity_case}" == dropin_override ]]; then
      run_tool "${root}" success "${API_HEX}" "${ROLLBACK}" "${release}" "${rollback_mode}" ||
        { cat "${root}/out" >&2; fail "effective rollback override rejected"; }
    else
      if run_tool "${root}" success "${API_HEX}" "${ROLLBACK}" "${release}" "${rollback_mode}"; then
        fail "${capacity_case}: unsafe rollback capacity accepted"
      fi
      assert_no_call "${root}" capacity '^(pair activate |systemctl )'
      cmp -s "${unit_file}" "${release}/previous/unit.container" && fail "unsafe rollback replaced base unit"
      [[ "$(installed_pair "${root}" hololive-api)" == candidate && -f "${release}/previous/profile.json" ]] ||
        fail "capacity rejection lost current pair or recovery"
    fi
  done
done
pass "rollback validates retained unit caps with compose and installed drop-in precedence"

# 9. rollback --check는 이전 pair 검증만 하고 아무것도 바꾸지 않는다.
root="${tmp}/success"
: >"${root}/state/calls"
run_tool "${root}" rollback_check "${API_HEX}" "${ROLLBACK}" "${success_release}" --check ||
  { cat "${root}/out" >&2; fail "rollback check failed"; }
assert_order "${root}" "rollback check" 'pair verify hololive-api previous' 'pair check hololive-api previous'
assert_no_call "${root}" "rollback check" '^(pair activate |systemctl )'
pass "rollback --check verifies the retained pair without changes"

root="${tmp}/legacy-rollback"
make_case "$root" hololive-api true
release="$root/opt/hololive-bot/compose/current/artifacts/worker-profile/releases/prior"
mkdir -p "$release/previous"
printf 'hololive-api\n' >"$release/service"
printf 'profile\n' >"$release/previous/profile.json"
printf 'pair=previous\napp_version=captured\n' >"$release/previous/50-worker-profile.conf"
for mode in --check --apply; do
  run_tool "$root" success "$API_HEX" "$ROLLBACK" "$release" "$mode" ||
    { cat "$root/out" >&2; fail "기존 복원 가능한 release 거부"; }
done
[[ ! -e "$release/rollback.conf" && ! -e "$release/previous.endpoint" ]] ||
  fail "기존 release에 전환 증거를 소급 생성함"
pass "기존 release는 보존한 overlay로만 명시적 복원이 가능하다"

# 두 서비스의 실제 상태를 분리하고 API의 Wants가 정지한 worker를 기동하는 효과도 모델링한다.
FAKE_COORDINATED=true
make_coordinated_case() {
  local root="$1" service
  make_case "${root}" hololive-api true
  for service in hololive-api hololive-alarm-worker; do
    mkdir -p "${root}/state/${service}" "${root}/etc/containers/systemd/hololive-bot/${service}.container.d"
    printf 'true\n' >"${root}/state/${service}/active"
    printf 'loaded\n' >"${root}/state/${service}/loadstate"
    printf 'generated\n' >"${root}/state/${service}/mask_scope"
    if [[ "$service" == hololive-api ]]; then printf '%032d\n' 7; else printf '%032d\n' 1; fi >"${root}/state/${service}/invocation"
    if [[ "$service" == hololive-api ]]; then printf '%064d\n' 7; else printf '%064d\n' 1; fi >"${root}/state/${service}/cid"
    printf '3\n' >"${root}/state/${service}/nrestarts"
    printf 'baseline\n' >"${root}/state/${service}/runtime"
    printf 'pair=baseline\n' >"${root}/etc/containers/systemd/hololive-bot/${service}.container.d/50-worker-profile.conf"
  done
}
coordinated_release() {
  local root="$1" service="$2" candidate
  for candidate in "${root}/opt/hololive-bot/compose/current/artifacts/worker-profile/releases/"*; do
    if [[ -f "${candidate}/service" && "$(<"${candidate}/service")" == "${service}" ]]; then
      printf '%s\n' "${candidate}"
      return
    fi
  done
  return 1
}
assert_held_pair() {
  local root="$1" service
  for service in hololive-api hololive-alarm-worker; do
    [[ "$(<"${root}/state/${service}/active")" == false &&
      "$(<"${root}/state/${service}/loadstate")" == masked ]] ||
      fail "coordinated failure released the stopped peer"
  done
}
quiesce_case() {
  local root="$1"
  FAKE_SERVER_EPOCH=old
  run_tool "${root}" success "${API_HEX}" "${HELPER}" --quiesce \
    localhost/hololive-api:candidate localhost/hololive-alarm-worker:candidate ||
    { cat "${root}/out" >&2; fail "coordinated quiescence failed"; }
  assert_held_pair "${root}"
  [[ ! -e "${root}/state/migrated-with" ]] || fail "quiescence ran a migration before the peer cutover"
  [[ ! -e "$root/state/unguarded-stop" ]] || fail "기동 차단이나 원래 정지 명령 없이 원본을 정지함"
}
assert_candidate_pair_running() {
  local root="$1" service
  for service in hololive-api hololive-alarm-worker; do
    [[ "$(<"${root}/state/${service}/active")" == true &&
      "$(<"${root}/state/${service}/loadstate")" == loaded &&
      "$(<"${root}/state/${service}/runtime")" == candidate ]] ||
      fail "coordinated resume did not start the complete candidate pair"
  done
  [[ ! -e "${root}/state/unsafe-start" && ! -e "${root}/state/unsafe-migration" ]] ||
    fail "a live old peer crossed the coordinated activation boundary"
  [[ "$(<"${root}/state/migrated-with")" == "sha256:${API_HEX}" ]] ||
    fail "coordinated resume did not use its API image for migration"
}

root="${tmp}/coordinated-success"
make_coordinated_case "${root}"
mv "${root}/state/live-pgbouncer.ini" "${root}/state/stopped-pgbouncer.ini"
quiesce_case "${root}"
assert_no_call "${root}" quiesce '^podman exec holo-pgbouncer '
assert_order "$root" quiesce \
  'guard arm hololive-alarm-worker.service' 'guard arm hololive-api.service' \
  'guard check hololive-alarm-worker.service' 'guard check hololive-api.service' \
  'pair verify-captured hololive-alarm-worker previous' 'guard check hololive-alarm-worker.service' \
  'systemctl stop hololive-alarm-worker.service' 'journal stop hololive-alarm-worker.service' \
  'exit-status hololive-alarm-worker.service' \
  'pair verify-captured hololive-api previous' 'guard check hololive-api.service' \
  'systemctl stop hololive-api.service' 'journal stop hololive-api.service' \
  'exit-status hololive-api.service' \
  'guard seal hololive-alarm-worker.service' 'guard seal hololive-api.service'
api_release="$(coordinated_release "${root}" hololive-api)"
worker_release="$(coordinated_release "${root}" hololive-alarm-worker)"
for capacity_case in missing drift query_failed; do
  : >"${root}/state/calls"
  case "${capacity_case}" in
    drift)
      cp "${root}/state/stopped-pgbouncer.ini" "${root}/state/live-pgbouncer.ini"
      printf 'ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n' >>"${root}/etc/stack-secrets/hololive-bot/compose.env" ;;
    query_failed) touch "${root}/state/pooler-query-failed" ;;
  esac
  if run_tool "${root}" success "${API_HEX}" "${HELPER}" --resume "${api_release}" "${worker_release}"; then
    fail "${capacity_case}: unsafe held resume accepted"
  fi
  assert_held_pair "${root}"
  assert_no_call "${root}" capacity '^(pair activate |systemctl (unmask|start|restart|daemon-reload))'
  [[ -f "${api_release}/previous/profile.json" && -f "${worker_release}/previous/profile.json" ]] ||
    fail "capacity rejection discarded retained recovery pairs"
done
rm "${root}/state/pooler-query-failed"
printf 'HOLOLIVE_VALKEY_HA_ENABLED=1\n' >"${root}/etc/stack-secrets/hololive-bot/compose.env"
api_release="$(coordinated_release "${root}" hololive-api)"
worker_release="$(coordinated_release "${root}" hololive-alarm-worker)"
FAKE_SERVER_EPOCH=new
run_tool "${root}" success "${API_HEX}" "${HELPER}" --resume "${api_release}" "${worker_release}" ||
  { cat "${root}/out" >&2; fail "coordinated resume failed"; }
assert_candidate_pair_running "${root}"
assert_order "$root" resume \
  'systemctl start hololive-db-migrate.service' 'writer-sql hololive_alarm_worker' 'writer-sql hololive_runtime' \
  'pair activate hololive-alarm-worker candidate' 'pair activate hololive-api candidate' \
  'systemctl unmask hololive-alarm-worker.service' 'pair verify-live hololive-alarm-worker candidate' \
  'systemctl unmask hololive-api.service' 'pair verify-live hololive-api candidate'
pass "coordinated resume never starts an old dependency before both candidate pairs are installed"

root="${tmp}/coordinated-reboot"
make_coordinated_case "${root}"
quiesce_case "${root}"
api_release="$(coordinated_release "${root}" hololive-api)"
worker_release="$(coordinated_release "${root}" hololive-alarm-worker)"
FAKE_SERVER_EPOCH=new
run_tool "${root}" success "${API_HEX}" "${fakebin}/systemctl" simulate-reboot
assert_held_pair "${root}"
[[ ! -e "${root}/state/unsafe-start" ]] || fail "reboot started a retained old writer against the new Iris"
run_tool "${root}" success "${API_HEX}" "${HELPER}" --resume "${api_release}" "${worker_release}" ||
  { cat "${root}/out" >&2; fail "resume after reboot failed"; }
assert_candidate_pair_running "${root}"
pass "host reboot preserves both old writers in hold until explicit candidate resume"

root="${tmp}/coordinated-partial"
make_coordinated_case "${root}"
quiesce_case "${root}"
api_release="$(coordinated_release "${root}" hololive-api)"
worker_release="$(coordinated_release "${root}" hololive-alarm-worker)"
FAKE_SERVER_EPOCH=new
if run_tool "${root}" api_activation_failure "${API_HEX}" "${HELPER}" --resume "${api_release}" "${worker_release}"; then
  fail "partial activation failure was accepted"
fi
assert_held_pair "${root}"
[[ "$(installed_pair "${root}" hololive-alarm-worker)" == candidate &&
  "$(installed_pair "${root}" hololive-api)" == baseline ]] ||
  fail "partial activation did not preserve the actual installed pair state"
run_tool "${root}" success "${API_HEX}" "${HELPER}" --resume "${api_release}" "${worker_release}" ||
  { cat "${root}/out" >&2; fail "explicit resume of the held partial activation failed"; }
assert_candidate_pair_running "${root}"
[[ "$(grep -cxF 'systemctl start hololive-db-migrate.service' "$root/state/calls")" == 1 ]] ||
  fail "부분 게시 재개가 migration을 반복함"
pass "partial publication leaves both peers held and supports an explicit verified resume"

root="${tmp}/coordinated-stale"
make_coordinated_case "${root}"
quiesce_case "${root}"
api_release="$(coordinated_release "${root}" hololive-api)"
worker_release="$(coordinated_release "${root}" hololive-alarm-worker)"
printf '%s\n' fedcba9876543210fedcba9876543210fedcba98 \
  >"${root}/opt/hololive-bot/compose/current/.source-revision"
FAKE_SERVER_EPOCH=new
if run_tool "${root}" success "${API_HEX}" "${HELPER}" --resume "${api_release}" "${worker_release}"; then
  fail "stale prepared source was accepted"
fi
assert_held_pair "${root}"
[[ ! -e "${root}/state/migrated-with" ]] || fail "stale preparation reached migration"
pass "a changed deployment source cannot consume the retained coordinated release"

root="${tmp}/coordinated-lost-hold"
make_coordinated_case "${root}"
quiesce_case "${root}"
api_release="$(coordinated_release "${root}" hololive-api)"
worker_release="$(coordinated_release "${root}" hololive-alarm-worker)"
printf 'true\n' >"${root}/state/hololive-api/active"
FAKE_SERVER_EPOCH=new
if run_tool "${root}" success "${API_HEX}" "${HELPER}" --resume "${api_release}" "${worker_release}"; then
  fail "a running peer was accepted as a completed coordinated hold"
fi
[[ "$(<"${root}/state/hololive-api/active")" == true &&
  "$(<"${root}/state/hololive-alarm-worker/active")" == false &&
  "$(<"${root}/state/hololive-alarm-worker/loadstate")" == masked &&
  ! -e "${root}/state/migrated-with" ]] ||
  fail "incomplete hold triggered migration or changed the other peer"
pass "an incomplete coordinated hold is rejected without migration or automatic peer recovery"

for reason in generation_changed invocation_changed other_stopped_invocation residual_pid residual_container inventory_failure \
    failed_stop stop_command_failed abnormal_success_result lost_guard missing_job other_done_job journal_failure \
    gc_exit_143 missing_exit peer_changed; do
  root="${tmp}/quiesce-${reason}"
  make_coordinated_case "$root"
  if run_tool "$root" "$reason" "$API_HEX" "$HELPER" --quiesce \
      localhost/hololive-api:candidate localhost/hololive-alarm-worker:candidate; then
    fail "$reason: 불완전한 정지를 성공으로 판정"
  fi
  [[ ! -e "$(coordinated_release "$root" hololive-api)/quiesced" ]] || fail "$reason: 정지 영수증을 발행함"
  if [[ "$reason" == lost_guard || "$reason" == invocation_changed ]]; then
    assert_no_call "$root" "$reason" '^systemctl stop '
  elif [[ "$reason" == peer_changed ]]; then
    assert_no_call "$root" "$reason" '^systemctl stop hololive-api.service$'
  elif [[ "$reason" == gc_exit_143 ]]; then
    worker_release="$(coordinated_release "$root" hololive-alarm-worker)"
    grep -qxF 'ExecMainCode=0' "$worker_release/stop/terminal" && grep -qxF '143' "$worker_release/stop/container-exit" ||
      fail "수거된 관리자의 초기값이 원래 컨테이너의 143을 숨김"
  elif [[ "$reason" == failed_stop || "$reason" == abnormal_success_result ]]; then
    worker_release="$(coordinated_release "$root" hololive-alarm-worker)"
    grep -qxF 'ExecMainStatus=143' "$worker_release/stop/terminal" || fail "비정상 원본 종료 증거를 잃음"
    [[ -f "$root/state/hololive-alarm-worker/guard" && -f "$root/state/hololive-api/guard" ]] ||
      fail "비정상 정지에서 기동 차단을 해제함"
  fi
  assert_no_call "$root" "$reason" '^(pair activate |systemctl (start|restart|unmask))'
  : >"$root/state/calls"
  if run_tool "$root" success "$API_HEX" "$HELPER" --resume \
      "$(coordinated_release "$root" hololive-api)" "$(coordinated_release "$root" hololive-alarm-worker)"; then
    fail "$reason: 불완전한 정지를 근거로 재개함"
  fi
  assert_no_call "$root" "$reason" '^(writer-sql |podman exec |pair activate |systemctl (start|stop|restart|unmask))'
done

for reason in foreign_mask mixed_release swapped_generation legacy_receipt direct tls; do
  root="${tmp}/reject-resume-${reason}"
  make_coordinated_case "$root"
  quiesce_case "$root"
  api_release="$(coordinated_release "$root" hololive-api)"
  worker_release="$(coordinated_release "$root" hololive-alarm-worker)"
  case "$reason" in
    foreign_mask)
      mask="$root/etc/systemd/system/hololive-api.service"
      rm "$mask"
      ln -s /dev/null "$mask" ;;
    mixed_release)
      worker_copy="${worker_release}-other"
      cp -a "$worker_release" "$worker_copy"
      # 유효한 inode와 같은 이미지·invocation이라도 다른 release 경로는 영수증에 없다.
      mask="$root/etc/systemd/system/hololive-alarm-worker.service"
      ln -PT -- "$mask" "$root/etc/systemd/system/.${worker_copy##*/}.mask"
      worker_release="$worker_copy" ;;
    swapped_generation) printf '{"container_id":"%064d"}\n' 2 >"$worker_release/previous/pair.json" ;;
    legacy_receipt) sed -i '/^guarded-stop-v1$/d' "$api_release/quiesced" ;;
    direct) printf 'HOLOLIVE_CENTRAL_POSTGRES_HOST=holo-postgres\nHOLOLIVE_CENTRAL_POSTGRES_PORT=5432\n' \
      >>"$root/etc/stack-secrets/hololive-bot/compose.env" ;;
    tls) printf 'POSTGRES_SSLMODE=require\n' >>"$root/etc/stack-secrets/hololive-bot/compose.env" ;;
  esac
  : >"$root/state/calls"
  if run_tool "$root" success "$API_HEX" "$HELPER" --resume "$api_release" "$worker_release"; then
    fail "$reason: 안전하지 않은 재개를 허용함"
  fi
  assert_held_pair "$root"
  assert_no_call "$root" "$reason" '^(writer-sql |pair activate |systemctl (daemon-reload|unmask|start|restart))'
done

for reason in sql_failure_hololive_runtime sql_failure_hololive_alarm_worker migration_failure; do
  root="${tmp}/held-${reason}"
  make_coordinated_case "$root"
  quiesce_case "$root"
  api_release="$(coordinated_release "$root" hololive-api)"
  worker_release="$(coordinated_release "$root" hololive-alarm-worker)"
  if run_tool "$root" "$reason" "$API_HEX" "$HELPER" --resume "$api_release" "$worker_release"; then
    fail "$reason: migration 또는 SQL 실패를 성공으로 처리함"
  fi
  assert_no_call "$root" "$reason" '^(pair activate |systemctl (unmask|restart))'
  assert_held_pair "$root"
  : >"$root/state/calls"
  if run_tool "$root" "$reason" "$API_HEX" "$HELPER" --resume "$api_release" "$worker_release"; then
    fail "$reason: 실패한 검사를 건너뜀"
  fi
  assert_no_call "$root" "$reason" '^(pair activate |systemctl (start|restart|unmask))'
done

for writer in hololive-api hololive-alarm-worker; do
  root="${tmp}/reject-start-${writer}"
  make_coordinated_case "$root"
  quiesce_case "$root"
  api_release="$(coordinated_release "$root" hololive-api)"
  worker_release="$(coordinated_release "$root" hololive-alarm-worker)"
  : >"$root/state/calls"
  if run_tool "$root" "verify_failure_$writer" "$API_HEX" "$HELPER" --resume "$api_release" "$worker_release"; then
    fail "$writer: 후보 수용 실패가 성공으로 끝남"
  fi
  if [[ "$writer" == hololive-alarm-worker ]]; then
    assert_no_call "$root" "$writer" '^systemctl (unmask|restart|stop) hololive-api.service$'
    assert_no_call "$root" "$writer" '^guard (arm|check|seal) hololive-api.service$'
  fi
  assert_held_pair "$root"
  [[ ! -e "$root/state/unguarded-stop" ]] || fail "$writer: 복구 시 기동 차단 없이 정지함"
  ! grep -q '^\[OUTCOME\] deployed' "$root/out" || fail "$writer: 실패한 쌍의 배포 성공을 기록함"
  run_tool "$root" success "$API_HEX" "${fakebin}/systemctl" simulate-reboot
  assert_held_pair "$root"
done

for reason in lost_hold_during_migration term_after_unmask worker_cid_changed; do
  root="$tmp/$reason"
  make_coordinated_case "$root"
  quiesce_case "$root"
  api_release="$(coordinated_release "$root" hololive-api)"
  worker_release="$(coordinated_release "$root" hololive-alarm-worker)"
  : >"$root/state/calls"
  if run_tool "$root" "$reason" "$API_HEX" "$HELPER" --resume "$api_release" "$worker_release"; then
    fail "$reason: 불완전한 상태에서 재개함"
  fi
  assert_no_call "$root" "$reason" '^systemctl (restart|unmask) hololive-api.service$'
  if [[ "$reason" == lost_hold_during_migration ]]; then
    assert_no_call "$root" "$reason" '^(writer-sql |pair activate |systemctl unmask )'
  elif [[ "$reason" == worker_cid_changed ]]; then
    [[ -d "$api_release/opening" && -f "$root/state/hololive-alarm-worker/guard" &&
      "$(<"$root/state/hololive-alarm-worker/active")" == true ]] || fail "다른 실행 세대를 인수하거나 기동 차단을 누락함"
    assert_no_call "$root" "$reason" '^systemctl stop '
  else
    [[ -d "$api_release/opening" && -f "$root/state/hololive-alarm-worker/guard" ]] ||
      fail "unmask 직후 신호로 실패했는데 영속 기동 차단이 없음"
    assert_no_call "$root" "$reason" '^systemctl (stop|restart) '
  fi
done

root="$tmp/rehold-abnormal"
make_coordinated_case "$root"
quiesce_case "$root"
api_release="$(coordinated_release "$root" hololive-api)"
worker_release="$(coordinated_release "$root" hololive-alarm-worker)"
: >"$root/state/calls"
if run_tool "$root" failed_rehold "$API_HEX" "$HELPER" --resume "$api_release" "$worker_release"; then
  fail "재차 정지한 worker의 비정상 종료를 성공으로 보고함"
fi
assert_no_call "$root" rehold '^((guard (arm|check|seal)|systemctl (stop|restart|unmask)) hololive-api.service)$'
grep -qxF 'ExecMainStatus=143' "$worker_release"/rehold.*/terminal || fail "재정지 당시 원본 증거 유실"
[[ -d "$api_release/opening" && -f "$root/state/hololive-alarm-worker/guard" ]] || fail "불완전한 재정지를 허용함"
: >"$root/state/calls"
if run_tool "$root" success "$API_HEX" "$HELPER" --resume "$api_release" "$worker_release"; then
  fail "재정지에 실패했는데 기존 영수증을 소비함"
fi
assert_no_call "$root" rehold '^(writer-sql |podman exec |pair activate |systemctl (start|restart|unmask))'

root="${tmp}/before-sync"
make_coordinated_case "$root"
current="$root/opt/hololive-bot/compose/current"
staged="$root/opt/hololive-bot/compose/staged/$REVISION"
mkdir -p "$staged/scripts/deploy/lib" "$staged/scripts/ci"
cp "$HELPER" "$staged/scripts/deploy/"
cp "$ROOT_DIR/scripts/deploy/lib/"{systemd-stop-guard.py,compose-env.sh,postgres-endpoint.sh,postgres-capacity.sh} "$staged/scripts/deploy/lib/"
cp "$ROOT_DIR/scripts/ci/postgres-capacity-policy.tsv" "$staged/scripts/ci/"
printf '%s\n' "$REVISION" >"$staged/.source-revision"
printf '%040d\n' 1 >"$current/.source-revision"
units="$root/etc/containers/systemd/hololive-bot"
rm "$units/hololive-api.container.d/50-worker-profile.conf"
printf 'POSTGRES_HOST=holo-postgres\nPOSTGRES_PORT=5432\n' >"$root/state/running-endpoint"
for service in hololive-api hololive-alarm-worker; do
  sed -i -e 's/HOLOLIVE_CENTRAL_POSTGRES_HOST=holo-pgbouncer/HOLOLIVE_CENTRAL_POSTGRES_HOST=holo-postgres/' \
    -e 's/HOLOLIVE_CENTRAL_POSTGRES_PORT=6432/HOLOLIVE_CENTRAL_POSTGRES_PORT=5432/' "$units/$service.container"
done
mv "$root/state/"{live,uninstalled}-pgbouncer.ini
add_image "$root" staged-pooler "$POOLER_HEX" iris-stack-pgbouncer "${REVISION}"
for attribute in 'platform=linux/amd64' 'revision=bad' 'title=other'; do
  image="$root/state/images/sha256_${POOLER_HEX}"
  sed -i "s|^${attribute%%=*}=.*|${attribute}|" "$image"
  if run_tool "$root" success "$API_HEX" "$staged/scripts/deploy/quadlet-redeploy-service.sh" --quiesce-before-sync \
      localhost/hololive-api:candidate localhost/hololive-alarm-worker:candidate "sha256:$POOLER_HEX"; then
    fail "staged pooler의 잘못된 ${attribute%%=*}를 허용함"
  fi
  assert_no_call "$root" pooler '^(pair |systemctl (stop|start|restart|unmask))'
  add_image "$root" staged-pooler "$POOLER_HEX" iris-stack-pgbouncer "${REVISION}"
done
run_tool "$root" success "$API_HEX" "$staged/scripts/deploy/quadlet-redeploy-service.sh" --quiesce-before-sync \
  localhost/hololive-api:candidate localhost/hololive-alarm-worker:candidate "sha256:$POOLER_HEX" ||
  { cat "$root/out" >&2; fail "sync 전 실세대 정지 실패"; }
api_release="$(coordinated_release "$root" hololive-api)"
worker_release="$(coordinated_release "$root" hololive-alarm-worker)"
for release in "$api_release" "$worker_release"; do
  [[ "$(<"$release/pooler.image")" == "sha256:$POOLER_HEX" && "$(<"$release/pooler.platform")" == linux/arm64 &&
    "$(<"$release/pooler.revision")" == "$REVISION" ]] || fail "두 release에 고정한 pooler가 일치하지 않음"
done
cmp -s "$api_release/previous/50-worker-profile.conf" "$units/hololive-api.container.d/50-worker-profile.conf" ||
  fail "최초 prepare한 API의 활성 overlay가 rollback 렌더링으로 바뀜"
[[ "$(<"$api_release/previous.endpoint")" == 'holo-postgres|5432' && ! -e "$api_release/candidate" ]] ||
  fail "sync 전에 새 unit에서만 만들 수 있는 후보 또는 잘못된 endpoint를 기록함"
assert_no_call "$root" before-sync '^(writer-sql |podman exec holo-pgbouncer |pair candidate |systemctl (unmask|restart|start))'
mkdir -p "$current/deploy/quadlet/hololive-bot"
for service in hololive-api hololive-alarm-worker; do
  cp "$ROOT_DIR/deploy/quadlet/hololive-bot/$service.container" "$current/deploy/quadlet/hololive-bot/"
  cp "$current/deploy/quadlet/hololive-bot/$service.container" "$units/"
done
cp "$staged/.source-revision" "$current/.source-revision"
mv "$root/state/"{uninstalled,live}-pgbouncer.ini
run_tool "$root" success "$API_HEX" "${fakebin}/systemctl" simulate-reboot
assert_held_pair "$root"
printf 'true %s\n' "$WORKER_HEX" >"$root/state/pooler-identity"
: >"$root/state/calls"
if run_tool "$root" success "$API_HEX" "$HELPER" --resume "$api_release" "$worker_release"; then
  fail "실행 pooler가 staged ID와 다른데 재개함"
fi
assert_no_call "$root" pooler '^(writer-sql |pair candidate |pair activate |systemctl (unmask|start|restart))'
assert_held_pair "$root"
printf 'true %s\n' "$POOLER_HEX" >"$root/state/pooler-identity"
run_tool "$root" success "$API_HEX" "$HELPER" --resume "$api_release" "$worker_release" ||
  { cat "$root/out" >&2; fail "검토한 revision sync 후 재개 실패"; }
assert_candidate_pair_running "$root"
printf 'HOLOLIVE_CENTRAL_POSTGRES_HOST=holo-pgbouncer\nHOLOLIVE_CENTRAL_POSTGRES_PORT=6432\n' \
  >>"$root/etc/stack-secrets/hololive-bot/compose.env"
for mode in --check --apply; do
  : >"$root/state/calls"
  if run_tool "$root" success "$API_HEX" "$ROLLBACK" "$api_release" "$mode"; then
    fail "새 compose가 이전 direct 실행 세대의 증거를 대체함"
  fi
  assert_no_call "$root" rollback '^(pair activate |systemctl (start|restart|daemon-reload))'
done
FAKE_COORDINATED=false
root="$tmp/single-endpoint-change"
make_case "$root" hololive-api false
printf 'POSTGRES_HOST=holo-postgres\nPOSTGRES_PORT=5432\n' >"$root/state/running-endpoint"
if run_tool "$root" success "$API_HEX" "$HELPER" hololive-api localhost/hololive-api:candidate; then
  fail "단일 deploy로 coordinated endpoint 전환을 우회함"
fi
grep -q 'single-writer endpoint change refused' "$root/out" || { cat "$root/out" >&2; fail "단일 실행 endpoint gate에 도달하지 못함"; }
assert_no_call "$root" single '^(pair |systemctl (start|restart|daemon-reload))'
[[ ! -e "$root/opt/hololive-bot/compose/current/artifacts" ]] || fail "단일 전환 거부가 release를 변경함"
pass "새 unit 설치 전 확보한 같은 영속 hold에서만 pooled 후보를 준비하고 직렬로 연다"

echo "all quadlet redeploy checks passed"
