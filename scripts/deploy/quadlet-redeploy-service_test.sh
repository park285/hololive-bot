#!/usr/bin/env bash
# quadlet-redeploy-service.sh·quadlet-rollback-service.sh 흐름 테스트. 실제 helper를 비root 테스트 접두어
# (HOLOLIVE_QUADLET_TEST_ROOT) 아래에서 실행하고 podman·systemctl·hostname과 worker-profile-pair.py 호출(python3)만
# 가짜로 바꾼다. pair 도구의 검증은 worker-profile-pair_test.py가 소유한다. 단계 순서, 활성화 전 중단,
# 명시적 복원과 복원 실패, preflight 무변경, 두 소비자의 hold·부분 게시·재개를 확인한다.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
HELPER="${ROOT_DIR}/scripts/deploy/quadlet-redeploy-service.sh"
ROLLBACK="${ROOT_DIR}/scripts/deploy/quadlet-rollback-service.sh"
QUADLET_BIN=/usr/libexec/podman/quadlet
REVISION=0123456789abcdef0123456789abcdef01234567
API_HEX="$(printf 'a%.0s' {1..64})"
WORKER_HEX="$(printf 'b%.0s' {1..64})"

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
printf 'podman %s\n' "$*" >>"${state}/calls"
image_file() { printf '%s/images/%s' "${state}" "$(printf '%s' "$1" | tr '/:' '__')"; }
case "$1 ${2:-}" in
  'info --format') printf 'arm64\n' ;;
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

# restart는 설치된 worker-profile drop-in이 고정한 pair로 container를 다시 만든다. 명시적 restart는 NRestarts를 0으로 둔다.
cat >"${fakebin}/systemctl" <<'EOF'
#!/usr/bin/env bash
state="${FAKE_STATE:?}"
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
  printf 'true\n' >"${current}/active"
  [[ "${FAKE_CASE}" != auto_restart || "${pair}" != candidate ]] || printf '1\n' >"${current}/nrestarts"
}
case "$1" in
  daemon-reload) ;;
  is-active) [[ "$(cat "$(state_dir "${*: -1}")/active")" == true ]] ;;
  show)
    current="$(state_dir "$2")"
    case "$4" in
      NRestarts) cat "${current}/nrestarts" ;;
      LoadState) cat "${current}/loadstate" ;;
      UnitFileState) cat "${current}/mask_scope" ;;
      ActiveState) if [[ "$(<"${current}/active")" == true ]]; then echo active; else echo inactive; fi ;;
      *) exit 2 ;;
    esac
    ;;
  mask)
    scope=masked
    shift
    if [[ "$1" == --runtime ]]; then scope=masked-runtime; shift; fi
    [[ "$1" == --now ]] || exit 2
    shift
    for unit in "$@"; do
      current="$(state_dir "${unit}")"
      printf 'masked\n' >"${current}/loadstate"
      printf '%s\n' "${scope}" >"${current}/mask_scope"
      printf 'false\n' >"${current}/active"
    done
    ;;
  unmask)
    shift
    if [[ "${1:-}" == --runtime ]]; then shift; fi
    for unit in "$@"; do
      current="$(state_dir "${unit}")"
      printf 'loaded\n' >"${current}/loadstate"
      printf 'generated\n' >"${current}/mask_scope"
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

# worker-profile-pair.py 호출만 받는다. pair 디렉터리와 drop-in 게시를 파일로 흉내 낸다.
cat >"${fakebin}/python3" <<'EOF'
#!/usr/bin/env bash
state="${FAKE_STATE:?}"
[[ "${1:-}" == "${FAKE_PAIR_TOOL}" ]] || { echo "unexpected python3 call: $*" >&2; exit 2; }
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
    printf 'profile\n' >"${dir}/profile.json"
    # 실제 도구는 이전 pair가 기록한 기본 unit(그 image가 돌던 정의)을 이어받는다.
    [[ ! -f "${state}/previous-unit" ]] || cp "${state}/previous-unit" "${dir}/unit.container"
    if [[ "${action}" == prepare ]]; then
      printf 'pair=previous\n' >"${dir}/50-worker-profile.conf"
      mkdir -p "${unit_dropin%/*}"
      cp "${dir}/50-worker-profile.conf" "${unit_dropin}"
    fi
    ;;
  rollback)
    [[ -f "${dir}/profile.json" ]] || exit 3
    printf 'pair=%s\n' "${dir##*/}" >"${dir}/50-worker-profile.conf"
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
  verify-retained)
    [[ -f "${dir}/profile.json" && -f "$4/profile.json" && "$5" == "${FAKE_DEPLOY_ROOT}" ]] || exit 3
    retained="$(sed -n 's/^pair=//p' "${unit_dropin}")"
    [[ "${retained}" == baseline || "${retained}" == previous || "${retained}" == candidate ]]
    ;;
  activate)
    [[ "$4" == "${FAKE_DEPLOY_ROOT}" ]] || exit 3
    [[ "${FAKE_CASE}" != api_activation_failure || "${service}" != hololive-api ]] || exit 1
    mkdir -p "${unit_dropin%/*}"
    cp "${dir}/50-worker-profile.conf" "${unit_dropin}"
    ;;
  verify-live)
    expected="${dir##*/}"
    if [[ "${FAKE_COORDINATED:-false}" == true && "${expected}" == previous ]]; then expected=baseline; fi
    [[ "$4" == "${service}" && "$(cat "${current}/active")" == true &&
      "$(cat "${current}/runtime")" == "${expected}" ]]
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
    "${root}/state/images"
  printf '%s\n' "${REVISION}" >"${root}/opt/hololive-bot/compose/current/.source-revision"
  cp "${ROOT_DIR}/deploy/quadlet/hololive-bot/"*.container "${quadlet_dir}/"
  printf 'HOLOLIVE_VALKEY_HA_ENABLED=1\n' >"${root}/etc/stack-secrets/hololive-bot/compose.env"
  if [[ "${with_dropin}" == true ]]; then
    mkdir -p "${quadlet_dir}/${service}.container.d"
    printf 'pair=baseline\n' >"${quadlet_dir}/${service}.container.d/50-worker-profile.conf"
  fi
  add_image "${root}" localhost/hololive-api:candidate "${API_HEX}" hololive-api "${api_revision}"
  add_image "${root}" localhost/hololive-alarm-worker:candidate "${WORKER_HEX}" hololive-alarm-worker "${REVISION}"
  printf 'true\n' >"${root}/state/active"
  printf '3\n' >"${root}/state/nrestarts"
  printf 'baseline\n' >"${root}/state/runtime"
  : >"${root}/state/calls"
}

run_tool() {
  local root="$1" case_name="$2" candidate_hex="$3"
  shift 3
  env PATH="${fakebin}:${PATH}" HOLOLIVE_QUADLET_TEST_ROOT="${root}" FAKE_STATE="${root}/state" FAKE_CASE="${case_name}" \
    FAKE_PAIR_TOOL="${ROOT_DIR}/scripts/deploy/lib/worker-profile-pair.py" \
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
  'pair activate hololive-api candidate' 'systemctl daemon-reload' 'systemctl restart hololive-api.service' \
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

# 5-2. 다른 helper가 실행 중이면 아무것도 바꾸지 않고 멈춘다(migrator drop-in·daemon-reload·migration 경합 방지).
root="${tmp}/concurrent"
make_case "${root}" hololive-api true
mkdir -p "${root}/run/lock"
exec 8>"${root}/run/lock/hololive-quadlet-redeploy.lock"
flock 8
if run_tool "${root}" success "${API_HEX}" "${HELPER}" hololive-api localhost/hololive-api:candidate; then
  fail "concurrent: helper ran while another held the lock"
fi
exec 8>&-
grep -q 'another quadlet-redeploy-service.sh is running' "${root}/out" || { cat "${root}/out" >&2; fail "concurrent: lock message missing"; }
[[ ! -s "${root}/state/calls" ]] || { cat "${root}/state/calls" >&2; fail "concurrent: helper made calls while locked out"; }
pass "a second helper refuses to run while another holds the redeploy lock"

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

# 9. rollback --check는 이전 pair 검증만 하고 아무것도 바꾸지 않는다.
root="${tmp}/success"
: >"${root}/state/calls"
run_tool "${root}" rollback_check "${API_HEX}" "${ROLLBACK}" "${success_release}" --check ||
  { cat "${root}/out" >&2; fail "rollback check failed"; }
assert_order "${root}" "rollback check" 'pair verify hololive-api previous' 'pair check hololive-api previous'
assert_no_call "${root}" "rollback check" '^(pair activate |systemctl )'
pass "rollback --check verifies the retained pair without changes"

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
quiesce_case "${root}"
api_release="$(coordinated_release "${root}" hololive-api)"
worker_release="$(coordinated_release "${root}" hololive-alarm-worker)"
FAKE_SERVER_EPOCH=new
run_tool "${root}" success "${API_HEX}" "${HELPER}" --resume "${api_release}" "${worker_release}" ||
  { cat "${root}/out" >&2; fail "coordinated resume failed"; }
assert_candidate_pair_running "${root}"
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

echo "all quadlet redeploy checks passed"
