#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="$(mktemp -d /tmp/valkey-selfheal-state-test.XXXXXX)"
trap 'rm -rf "${TMP_DIR}"' EXIT

fail() { echo "[FAIL] $*" >&2; exit 1; }
pass() { echo "[PASS] $*"; }

canonical_monitoring_state() {
  printf '%s\n' '{"version":2,"restart_count":0,"ping_failures":0,"epoch":0,"mutations":0,"next_eligible_at":0,"status":"monitoring","lock_token":""}'
}

setup_fixture() {
  local label="$1" fixture
  fixture="${TMP_DIR}/${label}/repo"
  mkdir -p "${fixture}/scripts/ops/lib" "${fixture}/scripts/deploy" "${TMP_DIR}/${label}/bin"
  cp "${ROOT_DIR}/scripts/ops/valkey-selfheal.sh" "${fixture}/scripts/ops/valkey-selfheal.sh"
  cp "${ROOT_DIR}/scripts/ops/lib/valkey-selfheal-state.sh" "${fixture}/scripts/ops/lib/valkey-selfheal-state.sh"
  cp "${ROOT_DIR}/scripts/ops/lib/valkey-selfheal-journal.sh" "${fixture}/scripts/ops/lib/valkey-selfheal-journal.sh"
  chmod +x "${fixture}/scripts/ops/valkey-selfheal.sh"
  cat >"${fixture}/scripts/deploy/compose.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'compose %s\n' "$*" >>"${FAKE_DOCKER_LOG:?}"
EOF
  chmod +x "${fixture}/scripts/deploy/compose.sh"
  touch "${fixture}/docker-compose.prod.yml"
  printf 'CACHE_PASSWORD=test\n' >"${fixture}/compose.env"
  canonical_monitoring_state >"${fixture}/state"
  cp "${fixture}/state" "${fixture}/state.guard"
  cp "${fixture}/state" "${fixture}/state.receipt"
  : >"${fixture}/state.lock"
  setup_fake_commands "${TMP_DIR}/${label}/bin"
  printf '%s\n' "${fixture}"
}

setup_fake_commands() {
  local fakebin="$1"
  cat >"${fakebin}/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${FAKE_DOCKER_LOG:?}"
case "${1:-}" in
  inspect) printf '0\n' ;;
  exec)
    if [ "${FAKE_REPLACE_LOCK:-0}" = 1 ] && [ ! -e "${FAKE_LOCK_REPLACE_DONE:?}" ]; then
      mv "${FAKE_STATE_LOCK:?}" "${FAKE_STATE_LOCK}.held"
      : >"${FAKE_STATE_LOCK}"
      : >"${FAKE_LOCK_REPLACE_DONE}"
    fi
    if [ "${FAKE_REPLACE_STATE:-0}" = 1 ] && [ ! -e "${FAKE_REPLACE_DONE:?}" ]; then
      cp "${FAKE_REPLACEMENT_STATE:?}" "${FAKE_STATE_FILE:?}"
      : >"${FAKE_REPLACE_DONE}"
    fi
    if [ -n "${FAKE_PING_PAYLOAD_FILE:-}" ]; then cat "${FAKE_PING_PAYLOAD_FILE}"; exit "${FAKE_PING_STATUS:-0}"; fi
    if [ "${FAKE_PING_OK:-0}" = 1 ] || [ -e "${FAKE_MUTATION_MARKER:-/nonexistent}" ]; then printf 'PONG\n'; exit 0; fi
    exit 1
    ;;
  restart)
    if [ -n "${FAKE_ACTION_ENTER_FILE:-}" ]; then
      if [ "${FAKE_ACTION_PAUSE_PHASE:-before_mutation}" = after_mutation ]; then
        : >"${FAKE_MUTATION_MARKER:?}"
      fi
      : >"${FAKE_ACTION_ENTER_FILE}"
      while [ ! -e "${FAKE_ACTION_RELEASE_FILE:?}" ]; do sleep 0.01; done
    fi
    if [ "${FAKE_ACTION_PAUSE_PHASE:-before_mutation}" != after_mutation ]; then
      : >"${FAKE_MUTATION_MARKER:?}"
    fi
    [ -z "${FAKE_ACTION_DONE_FILE:-}" ] || : >"${FAKE_ACTION_DONE_FILE}"
    ;;
  ps) exit 1 ;;
esac
EOF
  cat >"${fakebin}/sync" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
count=0
[ ! -r "${FAKE_SYNC_COUNT:-}" ] || count="$(cat "${FAKE_SYNC_COUNT}")"
count=$((count + 1))
[ -z "${FAKE_SYNC_COUNT:-}" ] || printf '%s\n' "${count}" >"${FAKE_SYNC_COUNT}"
if [ "${FAKE_SYNC_FAIL_AT:-0}" -eq "${count}" ]; then
  [ -z "${FAKE_SYNC_FAILED_MARKER:-}" ] || : >"${FAKE_SYNC_FAILED_MARKER}"
  exit 71
fi
if [ "${FAKE_SYNC_SKIP_REAL:-0}" != 1 ]; then
  /usr/bin/sync "$@" || exit "$?"
fi
if [ -n "${FAKE_RESERVATION_ENTER_FILE:-}" ] && [ ! -e "${FAKE_RESERVATION_ENTER_FILE}" ] &&
   [ "${2:-}" = "$(dirname -- "${FAKE_STATE_FILE:?}")" ] &&
   grep -Fq '"status":"recovering"' "${FAKE_STATE_FILE}" &&
   cmp -s "${FAKE_STATE_FILE}" "${FAKE_STATE_FILE}.guard" && cmp -s "${FAKE_STATE_FILE}" "${FAKE_STATE_FILE}.receipt"; then
  : >"${FAKE_RESERVATION_ENTER_FILE}"
  while [ ! -e "${FAKE_RESERVATION_RELEASE_FILE:?}" ]; do sleep 0.01; done
fi
if [ -n "${FAKE_RECEIPT_ENTER_FILE:-}" ] && [ ! -e "${FAKE_RECEIPT_ENTER_FILE}" ] &&
   [ "$(readlink -f -- "${2:-/nonexistent}" 2>/dev/null || true)" = "${FAKE_STATE_FILE:?}.receipt.tmp" ] &&
   grep -Fq "\"status\":\"${FAKE_RECEIPT_STATUS:?}\"" "${FAKE_STATE_FILE}.receipt.tmp"; then
  : >"${FAKE_RECEIPT_ENTER_FILE}"
  while [ ! -e "${FAKE_RECEIPT_RELEASE_FILE:?}" ]; do sleep 0.01; done
fi
EOF
  cat >"${fakebin}/mv" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
count=0
[ ! -r "${FAKE_MV_COUNT:-}" ] || count="$(cat "${FAKE_MV_COUNT}")"
count=$((count + 1))
[ -z "${FAKE_MV_COUNT:-}" ] || printf '%s\n' "${count}" >"${FAKE_MV_COUNT}"
if [ "${FAKE_MV_FAIL_AT:-0}" -eq "${count}" ]; then exit 72; fi
exec /usr/bin/mv "$@"
EOF
  cat >"${fakebin}/stat" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [ "${FAKE_SWAP_DURING_POSTCHECK:-0}" = 1 ] && [ -e "${FAKE_ACTION_DONE_FILE:?}" ] &&
   [ ! -e "${FAKE_POSTCHECK_SWAP_DONE:?}" ] && [[ " $* " == *" /proc/self/fd/"* ]]; then
  /usr/bin/stat "$@"
  /usr/bin/mv "${FAKE_STATE_LOCK:?}" "${FAKE_STATE_LOCK}.held"
  : >"${FAKE_STATE_LOCK}"
  : >"${FAKE_POSTCHECK_SWAP_DONE}"
  exit 0
fi
exec /usr/bin/stat "$@"
EOF
  chmod +x "${fakebin}/docker" "${fakebin}/sync" "${fakebin}/mv" "${fakebin}/stat"
}

run_probe() {
  local label="$1" fixture="$2" now="$3" out err fakebin
  out="${TMP_DIR}/${label}/out"
  err="${TMP_DIR}/${label}/err"
  fakebin="${TMP_DIR}/${label}/bin"
  : >"${TMP_DIR}/${label}/docker.log"
  PATH="${fakebin}:${PATH}" \
    FAKE_DOCKER_LOG="${TMP_DIR}/${label}/docker.log" \
    FAKE_MUTATION_MARKER="${TMP_DIR}/${label}/mutation" \
    FAKE_STATE_FILE="${fixture}/state" \
    FAKE_STATE_LOCK="${fixture}/state.lock" \
    FAKE_LOCK_REPLACE_DONE="${TMP_DIR}/${label}/lock-replace.done" \
    FAKE_REPLACE_DONE="${TMP_DIR}/${label}/replace.done" \
    FAKE_ACTION_DONE_FILE="${TMP_DIR}/${label}/action.done" \
    FAKE_POSTCHECK_SWAP_DONE="${TMP_DIR}/${label}/postcheck-swap.done" \
    SELFHEAL_STATE="${fixture}/state" SELFHEAL_JOURNAL="${fixture}/journal.jsonl" \
    SELFHEAL_NOW="${now}" RECOVERY_COOLDOWN_SEC=60 RESTART_SETTLE_SEC=0 \
    CRASH_RESTART_DELTA=999 PING_FAIL_THRESHOLD=1 REPO_DIR="${fixture}" \
    COMPOSE_FILE="${fixture}/docker-compose.prod.yml" COMPOSE_ENV_FILE="${fixture}/compose.env" \
    "${fixture}/scripts/ops/valkey-selfheal.sh" --apply >"${out}" 2>"${err}"
}

assert_no_mutation() {
  local label="$1"
  if grep -Eq '^(restart |compose )' "${TMP_DIR}/${label}/docker.log"; then fail "${label}: unexpected recovery mutation"; fi
}

invalid_state_fails_closed() {
  local label fixture invalid
  for label in missing corrupt oversize unknown-field wrong-type out-of-range legacy-recovering pending-missing-token; do
    fixture="$(setup_fixture "invalid-${label}")"
    case "${label}" in
      missing) rm -f "${fixture}/state" ;;
      corrupt) printf '{broken}\n' >"${fixture}/state"; cp "${fixture}/state" "${fixture}/state.guard" ;;
      oversize) printf '%600s\n' x >"${fixture}/state"; cp "${fixture}/state" "${fixture}/state.guard" ;;
      unknown-field) invalid='{"version":2,"restart_count":0,"ping_failures":0,"epoch":0,"mutations":0,"next_eligible_at":0,"status":"monitoring","lock_token":"","extra":0}'; printf '%s\n' "${invalid}" >"${fixture}/state"; cp "${fixture}/state" "${fixture}/state.guard" ;;
      wrong-type) invalid='{"version":2,"restart_count":"0","ping_failures":0,"epoch":0,"mutations":0,"next_eligible_at":0,"status":"monitoring","lock_token":""}'; printf '%s\n' "${invalid}" >"${fixture}/state"; cp "${fixture}/state" "${fixture}/state.guard" ;;
      out-of-range) invalid='{"version":2,"restart_count":0,"ping_failures":0,"epoch":1,"mutations":3,"next_eligible_at":0,"status":"manual_intervention_required","lock_token":""}'; printf '%s\n' "${invalid}" >"${fixture}/state"; cp "${fixture}/state" "${fixture}/state.guard" ;;
      legacy-recovering) invalid='{"version":1,"restart_count":0,"ping_failures":1,"epoch":100,"mutations":1,"next_eligible_at":160,"status":"recovering"}'; printf '%s\n' "${invalid}" >"${fixture}/state"; cp "${fixture}/state" "${fixture}/state.guard"; cp "${fixture}/state" "${fixture}/state.receipt" ;;
      pending-missing-token) invalid='{"version":2,"restart_count":0,"ping_failures":1,"epoch":100,"mutations":1,"next_eligible_at":160,"status":"recovering","lock_token":""}'; printf '%s\n' "${invalid}" >"${fixture}/state"; cp "${fixture}/state" "${fixture}/state.guard"; printf 'pending\n' >"${fixture}/state.receipt" ;;
    esac
    run_probe "invalid-${label}" "${fixture}" 100 || true
    assert_no_mutation "invalid-${label}"
    grep -Fq '"event":"state_invalid"' "${TMP_DIR}/invalid-${label}/err" || fail "${label}: invalid state was not observable"
  done
  pass "invalid and tokenless legacy/pending recovery state fail closed"
}

state_replacement_fails_closed() {
  local fixture label=replace-state
  fixture="$(setup_fixture "${label}")"
  printf '%s\n' '{"version":2,"restart_count":9,"ping_failures":0,"epoch":0,"mutations":0,"next_eligible_at":0,"status":"monitoring","lock_token":""}' >"${TMP_DIR}/${label}/replacement"
  FAKE_REPLACE_STATE=1 FAKE_REPLACEMENT_STATE="${TMP_DIR}/${label}/replacement" run_probe "${label}" "${fixture}" 100 || true
  assert_no_mutation "${label}"
  rm -f "${TMP_DIR}/${label}/replace.done"
  run_probe "${label}" "${fixture}" 200 || true
  assert_no_mutation "${label}"
  pass "state replacement cannot reopen the recovery mutation budget"
}

reservation_failures_remain_closed() {
  local kind point label fixture
  for kind in sync mv; do
    case "${kind}" in sync) points='5 6 7 8 9 10 11 12 13' ;; mv) points='3 4 5 6' ;; esac
    for point in ${points}; do
      label="reservation-${kind}-${point}"
      fixture="$(setup_fixture "${label}")"
      case "${kind}" in
        sync) FAKE_SYNC_COUNT="${TMP_DIR}/${label}/sync.count" FAKE_SYNC_FAIL_AT="${point}" run_probe "${label}" "${fixture}" 100 || true ;;
        mv) FAKE_MV_COUNT="${TMP_DIR}/${label}/mv.count" FAKE_MV_FAIL_AT="${point}" run_probe "${label}" "${fixture}" 100 || true ;;
      esac
      assert_no_mutation "${label}"
      run_probe "${label}" "${fixture}" 200 || true
      assert_no_mutation "${label}"
    done
  done
  pass "state fsync, rename, and directory-fsync failures remain fail closed"
}

finite_state_temp_slots_fail_closed() {
  local slot label fixture target before iteration attempts
  for slot in state.tmp state.guard.tmp state.receipt.tmp; do
    label="finite-${slot//./-}"
    fixture="$(setup_fixture "${label}")"
    target="${TMP_DIR}/${label}/foreign"
    printf 'foreign-owned\n' >"${target}"
    before="$(sha256sum "${target}")"
    ln -s "${target}" "${fixture}/${slot}"
    attempts=1
    [ "${slot}" != state.tmp ] || attempts=100
    for iteration in $(seq 1 "${attempts}"); do run_probe "${label}" "${fixture}" "$((100 + iteration))" || true; done
    assert_no_mutation "${label}"
    [ "$(sha256sum "${target}")" = "${before}" ] || fail "${slot}: foreign target changed"
    [ "$(find "${fixture}" -maxdepth 1 -name '*.tmp' -o -name '*.tmp.*' | wc -l)" -le 3 ] || fail "${slot}: temp artifact cap exceeded"
  done
  pass "100 repeated failures preserve foreign temp targets and finite state slots"
}

fixed_slots_bound_write_and_sync_failures() {
  local kind label fixture iteration_dir target before iteration count status sync_count
  for kind in write sync; do
    label="slot-${kind}-failure"
    fixture="$(setup_fixture "${label}")"
    for iteration in $(seq 1 101); do
      iteration_dir="${fixture}/failure-${iteration}"
      mkdir "${iteration_dir}"
      status=0
      if [ "${kind}" = write ]; then
        PATH="${TMP_DIR}/${label}/bin:${PATH}" bash -c '
          set -uo pipefail
          trap "" XFSZ
          ulimit -f 0
          MODE=--apply
          STATE_FILE="$1/state"
          . "$2"
          identity=""
          state_owned_slot_write "$1/write.tmp" value identity
        ' slot-write "${iteration_dir}" "${ROOT_DIR}/scripts/ops/lib/valkey-selfheal-state.sh" >/dev/null 2>&1 || status="$?"
      else
        sync_count="${iteration_dir}/sync.count"
        PATH="${TMP_DIR}/${label}/bin:${PATH}" FAKE_SYNC_FAIL_AT=1 FAKE_SYNC_COUNT="${sync_count}" \
          FAKE_SYNC_FAILED_MARKER="${iteration_dir}/sync.failed" bash -c '
          set -uo pipefail
          MODE=--apply
          STATE_FILE="$1/state"
          . "$2"
          identity=""
          state_owned_slot_write "$1/sync.tmp" value identity
        ' slot-sync "${iteration_dir}" "${ROOT_DIR}/scripts/ops/lib/valkey-selfheal-state.sh" >/dev/null 2>&1 || status="$?"
        [ -r "${sync_count}" ] && [ "$(cat "${sync_count}")" -eq 1 ] || fail "sync iteration ${iteration}: injected sync count=$(cat "${sync_count}" 2>/dev/null || printf missing), status=${status}"
        [ -e "${iteration_dir}/sync.failed" ] || fail "sync iteration ${iteration}: injected error marker missing"
      fi
      [ "${status}" -ne 0 ] || fail "${kind} iteration ${iteration}: injected failure returned success"
      count="$(find "${iteration_dir}" -maxdepth 1 \( -name 'write.tmp' -o -name 'sync.tmp' \) | wc -l)"
      [ "${count}" -le 1 ] || fail "${kind} iteration ${iteration}: fixed temp slot cap exceeded"
    done

    target="${TMP_DIR}/${label}/foreign"
    printf 'foreign-owned\n' >"${target}"
    before="$(sha256sum "${target}")"
    ln -s "${target}" "${fixture}/${kind}.tmp"
    status=0
    PATH="${TMP_DIR}/${label}/bin:${PATH}" bash -c '
      set -uo pipefail
      MODE=--apply
      STATE_FILE="$1/state"
      . "$2"
      identity=""
      state_owned_slot_write "$1/$3.tmp" value identity
    ' slot-foreign "${fixture}" "${ROOT_DIR}/scripts/ops/lib/valkey-selfheal-state.sh" "${kind}" >/dev/null 2>&1 || status="$?"
    [ "${status}" -ne 0 ] || fail "${kind}: foreign slot was accepted"
    [ "$(sha256sum "${target}")" = "${before}" ] || fail "${kind}: foreign symlink target changed"
    [ ! -e "${TMP_DIR}/${label}/mutation" ] || fail "${kind}: helper failure performed a mutation"
  done
  pass "first and 100 repeated write/sync failures stay within fixed slots without mutation"
}

full_persist_sync_failures_are_non_mutating() {
  local iteration label fixture sync_count
  for iteration in $(seq 1 101); do
    label="full-sync-${iteration}"
    fixture="$(setup_fixture "${label}")"
    sync_count="${TMP_DIR}/${label}/sync.count"
    FAKE_SYNC_COUNT="${sync_count}" FAKE_SYNC_FAIL_AT=5 FAKE_SYNC_FAILED_MARKER="${TMP_DIR}/${label}/sync.failed" \
      FAKE_SYNC_SKIP_REAL=1 run_probe "${label}" "${fixture}" 100 || true
    [ -e "${TMP_DIR}/${label}/sync.failed" ] || fail "full persist ${iteration}: injected sync failure was not reached"
    [ -r "${sync_count}" ] && [ "$(cat "${sync_count}")" -ge 5 ] || fail "full persist ${iteration}: sync count=$(cat "${sync_count}" 2>/dev/null || printf missing)"
    assert_no_mutation "${label}"
  done
  pass "first and 100 repeated full-persist sync failures perform zero Docker mutations"
}

postaction_reset_failure_keeps_reservation() {
  local label=postaction-reset fixture
  fixture="$(setup_fixture "${label}")"
  FAKE_SYNC_COUNT="${TMP_DIR}/${label}/sync.count" FAKE_SYNC_FAIL_AT=42 run_probe "${label}" "${fixture}" 100 || true
  [ -e "${TMP_DIR}/${label}/mutation" ] || fail "postaction fixture did not execute the reserved restart"
  rm -f "${TMP_DIR}/${label}/mutation"
  run_probe "${label}" "${fixture}" 101 || true
  assert_no_mutation "${label}"
  pass "post-side-effect reset failure cannot trigger an immediate second mutation"
}

concurrent_lock_and_healthy_reset() {
  local label=lock-busy fixture lock_fd healthy=healthy-repair
  fixture="$(setup_fixture "${label}")"
  exec {lock_fd}>"${fixture}/state.lock"
  flock -n "${lock_fd}"
  run_probe "${label}" "${fixture}" 100 || true
  assert_no_mutation "${label}"
  flock -u "${lock_fd}"

  fixture="$(setup_fixture "${healthy}")"
  rm -f "${fixture}/state" "${fixture}/state.guard"
  FAKE_PING_OK=1 run_probe "${healthy}" "${fixture}" 100
  cmp -s "${fixture}/state" "${fixture}/state.guard" && cmp -s "${fixture}/state" "${fixture}/state.receipt" || fail "healthy reset did not commit matching state receipts"
  jq -e '.version == 2 and .epoch == 0 and .mutations == 0 and .status == "monitoring" and .lock_token == "" and (keys == ["epoch","lock_token","mutations","next_eligible_at","ping_failures","restart_count","status","version"])' "${fixture}/state" >/dev/null || fail "healthy reset state schema mismatch"
  pass "concurrent lock is non-mutating and healthy PING repairs state"
}

lock_path_safety() {
  local label=lock-symlink fixture target before
  fixture="$(setup_fixture "${label}")"
  target="${TMP_DIR}/${label}/lock-target"
  printf 'owned-target\n' >"${target}"
  before="$(sha256sum "${target}")"
  rm -f "${fixture}/state.lock"
  ln -s "${target}" "${fixture}/state.lock"
  run_probe "${label}" "${fixture}" 100 || true
  assert_no_mutation "${label}"
  [ "$(sha256sum "${target}")" = "${before}" ] || fail "lock symlink target was modified"

  label=lock-replaced
  fixture="$(setup_fixture "${label}")"
  FAKE_REPLACE_LOCK=1 run_probe "${label}" "${fixture}" 100 || true
  assert_no_mutation "${label}"
  run_probe "${label}" "${fixture}" 200 || true
  assert_no_mutation "${label}"
  [ -e "${fixture}/state.failed" ] || fail "lock replacement did not persist fail-closed state"
  pass "lock symlinks and inode replacement cannot create a second mutation owner"
}

lock_token_closes_replacement_timing_table() {
  local phase label owner_b fixture owner_pid iteration enter release state_hash mutation_count
  for phase in reservation preaction postaction postcheck; do
    label="race-${phase}-owner-a"
    owner_b="race-${phase}-owner-b"
    fixture="$(setup_fixture "${label}")"
    mkdir -p "${TMP_DIR}/${owner_b}/bin"
    setup_fake_commands "${TMP_DIR}/${owner_b}/bin"
    case "${phase}" in
      reservation)
        enter="${TMP_DIR}/${label}/reservation.enter"
        release="${TMP_DIR}/${label}/reservation.release"
        FAKE_RESERVATION_ENTER_FILE="${enter}" FAKE_RESERVATION_RELEASE_FILE="${release}" run_probe "${label}" "${fixture}" 100 &
        ;;
      preaction|postaction)
        enter="${TMP_DIR}/${label}/action.enter"
        release="${TMP_DIR}/${label}/action.release"
        FAKE_ACTION_ENTER_FILE="${enter}" FAKE_ACTION_RELEASE_FILE="${release}" \
          FAKE_ACTION_PAUSE_PHASE="$([ "${phase}" = postaction ] && printf after_mutation || printf before_mutation)" \
          run_probe "${label}" "${fixture}" 100 &
        ;;
      postcheck)
        enter=""
        release=""
        FAKE_SWAP_DURING_POSTCHECK=1 run_probe "${label}" "${fixture}" 100 &
        ;;
    esac
    owner_pid="$!"
    if [ -n "${enter}" ]; then
      for iteration in $(seq 1 500); do [ ! -e "${enter}" ] || break; sleep 0.01; done
      [ -e "${enter}" ] || { kill "${owner_pid}" 2>/dev/null || true; wait "${owner_pid}" 2>/dev/null || true; fail "${phase}: owner A did not reach timing boundary"; }
      jq -e '.version == 2 and .status == "recovering" and .mutations == 1 and (.lock_token | length > 0)' "${fixture}/state" >/dev/null || fail "${phase}: reservation truth missing"
      state_hash="$(sha256sum "${fixture}/state")"
      /usr/bin/mv "${fixture}/state.lock" "${fixture}/state.lock.held"
      : >"${fixture}/state.lock"
      run_probe "${owner_b}" "${fixture}" 200 || true
      assert_no_mutation "${owner_b}"
      [ "$(sha256sum "${fixture}/state")" = "${state_hash}" ] || fail "${phase}: owner B overwrote reservation"
      : >"${release}"
    fi
    wait "${owner_pid}" || true
    if [ "${phase}" = postcheck ]; then
      [ -e "${TMP_DIR}/${label}/postcheck-swap.done" ] || fail "postcheck: lock was not replaced at the postcheck boundary"
      state_hash="$(sha256sum "${fixture}/state")"
      run_probe "${owner_b}" "${fixture}" 200 || true
      assert_no_mutation "${owner_b}"
    fi
    [ -e "${fixture}/state.failed" ] || fail "${phase}: terminal failure marker missing"
    mutation_count="$({ grep -Eh '^(restart |compose )' "${TMP_DIR}/${label}/docker.log" "${TMP_DIR}/${owner_b}/docker.log" 2>/dev/null || true; } | wc -l)"
    [ "${mutation_count}" -le 1 ] || fail "${phase}: more than one Docker mutation"
    [ "$(sha256sum "${fixture}/state")" = "${state_hash}" ] || fail "${phase}: reserved state truth changed"
  done
  pass "lock replacement timing table preserves one-mutation and terminal-state truth"
}

canonical_receipt_boundary_is_fail_closed() {
  local status label owner_b fixture owner_pid iteration enter release state_hash mutation_count
  for status in recovering monitoring; do
    label="receipt-${status}-owner-a"
    owner_b="receipt-${status}-owner-b"
    fixture="$(setup_fixture "${label}")"
    mkdir -p "${TMP_DIR}/${owner_b}/bin"
    setup_fake_commands "${TMP_DIR}/${owner_b}/bin"
    enter="${TMP_DIR}/${label}/receipt.enter"
    release="${TMP_DIR}/${label}/receipt.release"
    FAKE_RECEIPT_ENTER_FILE="${enter}" FAKE_RECEIPT_RELEASE_FILE="${release}" FAKE_RECEIPT_STATUS="${status}" run_probe "${label}" "${fixture}" 100 &
    owner_pid="$!"
    for iteration in $(seq 1 2000); do [ ! -e "${enter}" ] || break; sleep 0.01; done
    [ -e "${enter}" ] || { kill "${owner_pid}" 2>/dev/null || true; wait "${owner_pid}" 2>/dev/null || true; fail "receipt ${status}: canonical publish boundary not reached"; }
    state_hash="$(sha256sum "${fixture}/state")"
    /usr/bin/mv "${fixture}/state.lock" "${fixture}/state.lock.held"
    : >"${fixture}/state.lock"
    run_probe "${owner_b}" "${fixture}" 200 || true
    assert_no_mutation "${owner_b}"
    [ "$(sha256sum "${fixture}/state")" = "${state_hash}" ] || fail "receipt ${status}: owner B overwrote state"
    : >"${release}"
    wait "${owner_pid}" || true
    mutation_count="$({ grep -Eh '^(restart |compose )' "${TMP_DIR}/${label}/docker.log" "${TMP_DIR}/${owner_b}/docker.log" 2>/dev/null || true; } | wc -l)"
    [ "${mutation_count}" -le 1 ] || fail "receipt ${status}: more than one Docker mutation"
    if [ "${status}" = recovering ]; then
      [ -e "${fixture}/state.failed" ] || fail "receipt recovering: terminal failure marker missing"
      jq -e '.status == "recovering" and .mutations == 1 and (.lock_token | length > 0)' "${fixture}/state" >/dev/null || fail "receipt recovering: reserved state truth lost"
    else
      jq -e '.status == "monitoring" and .mutations == 0 and .lock_token == ""' "${fixture}/state" >/dev/null || fail "receipt monitoring: exact-PONG terminal truth lost"
      cmp -s "${fixture}/state" "${fixture}/state.guard" && cmp -s "${fixture}/state" "${fixture}/state.receipt" || fail "receipt monitoring: canonical files diverged"
    fi
  done
  pass "canonical receipt publish races preserve recovering or exact-PONG monitoring truth"
}

large_state_symlink_fails_closed() {
  local label=large-state-symlink fixture target before
  fixture="$(setup_fixture "${label}")"
  target="${TMP_DIR}/${label}/large-target"
  truncate -s 10M "${target}"
  before="$(sha256sum "${target}")"
  rm -f "${fixture}/state"
  ln -s "${target}" "${fixture}/state"
  run_probe "${label}" "${fixture}" 100 || true
  assert_no_mutation "${label}"
  [ "$(sha256sum "${target}")" = "${before}" ] || fail "state symlink target was read or modified"
  [ "$(stat -c '%s' "${target}")" -eq 10485760 ] || fail "large state target size changed"
  pass "state loading rejects a 10MiB symlink target without mutation"
}

ping_requires_exact_pong() {
  local payload label fixture terminal before
  for payload in notpong prefix suffix extra-line extra-blank; do
    label="ping-${payload}"
    fixture="$(setup_fixture "${label}")"
    terminal="$(printf '{\"version\":2,\"restart_count\":9,\"ping_failures\":8,\"epoch\":100,\"mutations\":2,\"next_eligible_at\":0,\"status\":\"manual_intervention_required\",\"lock_token\":\"%s\"}' "$(stat -Lc '%f:%d:%i' "${fixture}/state.lock")")"
    printf '%s\n' "${terminal}" >"${fixture}/state"
    cp "${fixture}/state" "${fixture}/state.guard"
    cp "${fixture}/state" "${fixture}/state.receipt"
    before="$(sha256sum "${fixture}/state")"
    case "${payload}" in
      notpong) printf 'NOTPONG\n' >"${TMP_DIR}/${label}/payload" ;;
      prefix) printf 'XPONG\n' >"${TMP_DIR}/${label}/payload" ;;
      suffix) printf 'PONGX\n' >"${TMP_DIR}/${label}/payload" ;;
      extra-line) printf 'PONG\nEXTRA\n' >"${TMP_DIR}/${label}/payload" ;;
      extra-blank) printf 'PONG\n\n' >"${TMP_DIR}/${label}/payload" ;;
    esac
    FAKE_PING_PAYLOAD_FILE="${TMP_DIR}/${label}/payload" run_probe "${label}" "${fixture}" 200 || true
    assert_no_mutation "${label}"
    [ "$(sha256sum "${fixture}/state")" = "${before}" ] || fail "${payload}: non-exact PONG reset terminal state"
  done

  label=ping-crlf
  fixture="$(setup_fixture "${label}")"
  printf 'PONG\r\n' >"${TMP_DIR}/${label}/payload"
  FAKE_PING_PAYLOAD_FILE="${TMP_DIR}/${label}/payload" run_probe "${label}" "${fixture}" 200
  jq -e '.status == "monitoring" and .mutations == 0' "${fixture}/state" >/dev/null || fail "CRLF PONG was not normalized"
  pass "only one bounded exact PONG line is healthy"
}

steady_healthy_ticks_are_observation_only() {
  local label=steady-healthy fixture before_journal before_guard before_meta before_state before_state_meta iteration
  fixture="$(setup_fixture "${label}")"
  printf '%s\n' '{"event":"fixture"}' >"${fixture}/journal.jsonl"
  printf '%s %s\n' "$(stat -c '%s' "${fixture}/journal.jsonl")" "$(sha256sum "${fixture}/journal.jsonl" | awk '{print $1}')" >"${fixture}/journal.jsonl.guard"
  before_journal="$(sha256sum "${fixture}/journal.jsonl")"
  before_guard="$(sha256sum "${fixture}/journal.jsonl.guard")"
  before_meta="$(stat -c '%s:%Y:%Z' "${fixture}/journal.jsonl" "${fixture}/journal.jsonl.guard")"
  before_state="$(sha256sum "${fixture}/state" "${fixture}/state.guard" "${fixture}/state.receipt")"
  before_state_meta="$(stat -c '%s:%Y:%Z' "${fixture}/state" "${fixture}/state.guard" "${fixture}/state.receipt")"
  for iteration in $(seq 1 10); do
    FAKE_PING_OK=1 FAKE_SYNC_COUNT="${TMP_DIR}/${label}/sync.count" run_probe "${label}" "${fixture}" "$((300 + iteration))"
  done
  [ ! -e "${TMP_DIR}/${label}/sync.count" ] || [ "$(cat "${TMP_DIR}/${label}/sync.count")" -eq 0 ] || fail "steady healthy tick issued durable sync"
  [ "$(sha256sum "${fixture}/journal.jsonl")" = "${before_journal}" ] &&
    [ "$(sha256sum "${fixture}/journal.jsonl.guard")" = "${before_guard}" ] || fail "steady healthy tick changed journal content"
  [ "$(stat -c '%s:%Y:%Z' "${fixture}/journal.jsonl" "${fixture}/journal.jsonl.guard")" = "${before_meta}" ] || fail "steady healthy tick changed journal metadata"
  [ "$(sha256sum "${fixture}/state" "${fixture}/state.guard" "${fixture}/state.receipt")" = "${before_state}" ] || fail "steady healthy tick changed state content"
  [ "$(stat -c '%s:%Y:%Z' "${fixture}/state" "${fixture}/state.guard" "${fixture}/state.receipt")" = "${before_state_meta}" ] || fail "steady healthy tick changed state metadata"
  pass "steady healthy ticks use stderr observation without state or journal fsync"
}

invalid_state_fails_closed
state_replacement_fails_closed
reservation_failures_remain_closed
finite_state_temp_slots_fail_closed
fixed_slots_bound_write_and_sync_failures
full_persist_sync_failures_are_non_mutating
postaction_reset_failure_keeps_reservation
concurrent_lock_and_healthy_reset
lock_path_safety
lock_token_closes_replacement_timing_table
canonical_receipt_boundary_is_fail_closed
large_state_symlink_fails_closed
ping_requires_exact_pong
steady_healthy_ticks_are_observation_only
echo "ok: valkey self-heal durable state contract tests passed"
