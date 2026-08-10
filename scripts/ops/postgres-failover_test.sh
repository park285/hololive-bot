#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PRIMARY_FENCE="${ROOT_DIR}/scripts/ops/postgres-primary-fence.sh"
TMP_DIR="$(mktemp -d /tmp/postgres-failover-test.XXXXXX)"
trap 'rm -rf "${TMP_DIR}"' EXIT
EXEC_ROOT="${TMP_DIR}/exec"; mkdir -p "${EXEC_ROOT}"
(cd "${ROOT_DIR}" && cp --parents scripts/ops/postgres-failover.sh scripts/ops/lib/postgres-failover-lib.sh scripts/ops/lib/postgres-failover-transition-lib.sh "${EXEC_ROOT}")
chmod -R go-w "${EXEC_ROOT}"
CONTROLLER="${EXEC_ROOT}/scripts/ops/postgres-failover.sh"
if [[ "$(id -u)" == "0" ]]; then CONTROLLER_TEST_MODE=0; else CONTROLLER_TEST_MODE=1; fi
failures=0
pass() { printf '[PASS] %s\n' "$*"; }
fail() { printf '[FAIL] %s\n' "$*" >&2; failures=$((failures + 1)); }
setup_fake_docker() {
  local dir="$1"
  mkdir -p "${dir}"
  cat >"${dir}/docker" <<'FAKE_DOCKER'
#!/usr/bin/env bash
set -u
printf '%s\n' "$*" >>"${FAKE_DOCKER_LOG:?}"
args="$*"
if [[ "${args}" == *"pg_promote"* ]]; then
  : >"${FAKE_PROMOTED_FILE:?}"
  printf 't\n'
  exit 0
fi
if [[ "${args}" == exec* && "${args}" == *" grep -qx role=primary "* ]]; then
  grep -qx 'role=primary' "${FAKE_CONTAINER_SIGNAL_FILE:?}" 2>/dev/null
  exit $?
fi
if [[ "${args}" == exec* && "${args}" == *" /bin/sh -ec "* && "${args}" == *".hololive-promoted"* ]]; then
  printf 'role=primary\n' >"${FAKE_CONTAINER_SIGNAL_FILE:?}"
  exit 0
fi
if [[ "${args}" == exec* && "${args}" == *" -h ${FAKE_PRIMARY_HOST:-100.100.1.8} "* ]]; then
  count=0
  [[ -r "${FAKE_PRIMARY_COUNT_FILE:?}" ]] && count="$(cat "${FAKE_PRIMARY_COUNT_FILE}")"
  count=$((count + 1))
  printf '%s\n' "${count}" >"${FAKE_PRIMARY_COUNT_FILE}"
  IFS=',' read -r -a sequence <<<"${FAKE_PRIMARY_SEQUENCE:-up}"
  index=$((count - 1))
  if (( index >= ${#sequence[@]} )); then index=$((${#sequence[@]} - 1)); fi
  case "${sequence[$index]}" in
    up) printf '%s\n' "${FAKE_PRIMARY_STATUS:-f|0/20|off}"; exit 0 ;;
    readonly) printf 'f|0/20|on\n'; exit 0 ;;
    standby) printf 't|0/20|on\n'; exit 0 ;;
    down) exit 1 ;;
    *) exit 2 ;;
  esac
fi
if [[ "${args}" == exec* && "${args}" == *"SELECT pg_is_in_recovery()"* ]]; then
  if [[ -e "${FAKE_PROMOTED_FILE:?}" ]]; then
    printf 'f|0/20|0/20|f|off\n'
  else
    printf '%s\n' "${FAKE_LOCAL_STATUS:-t|0/20|0/20|f|on}"
  fi
  exit 0
fi
exit 2
FAKE_DOCKER
  chmod 0755 "${dir}/docker"
}
setup_case() {
  local label="$1"
  local root="${TMP_DIR}/${label}"
  mkdir -p "${root}/state" "${root}/bin" "${root}/hooks"
  chmod 0700 "${root}/state" "${root}/hooks"
  setup_fake_docker "${root}/bin"
  : >"${root}/docker.log"
  : >"${root}/hooks.log"
  : >"${root}/primary.count"
  rm -f "${root}/primary.count" "${root}/promoted" "${root}/container-signal"
  cat >"${root}/hooks/fence.sh" <<'FENCE'
#!/usr/bin/env bash
printf 'fence\n' >>"${FAKE_HOOK_LOG:?}"
printf '%s\n' "${FAKE_FENCE_OUTPUT:-FENCED|${POSTGRES_FAILOVER_PRIMARY_HOST}|${POSTGRES_FAILOVER_NEW_PRIMARY_HOST}:${POSTGRES_FAILOVER_NEW_PRIMARY_PORT}|${POSTGRES_FAILOVER_REQUEST_ID}}"
FENCE
  cat >"${root}/hooks/route.sh" <<'ROUTE'
#!/usr/bin/env bash
printf 'route\n' >>"${FAKE_HOOK_LOG:?}"
printf '%s\n' "${FAKE_ROUTE_OUTPUT:-ROUTED|${POSTGRES_FAILOVER_NEW_PRIMARY_HOST}:${POSTGRES_FAILOVER_NEW_PRIMARY_PORT}|${POSTGRES_FAILOVER_FENCE_TOKEN}}"
ROUTE
  chmod 0600 "${root}/hooks/fence.sh" "${root}/hooks/route.sh"
  printf '%s\n' "${root}"
}
seed_ready_state() {
  local root="$1"
  local failure_count="${2:-1}"
  local first_failure="${3:-100}"
  local last_healthy="${4:-90}"
  local primary_lsn="${5:-0/20}"
  local replay_lsn="${6:-0/20}"
  printf '1\t%s\t%s\t%s\t%s\t%s\tmonitoring\t-\n' \
    "${failure_count}" "${first_failure}" "${last_healthy}" "${primary_lsn}" "${replay_lsn}" >"${root}/state/state.tsv"
  chmod 0600 "${root}/state/state.tsv"
}
run_controller() {
  local root="$1" mode="$2" sequence="$3"
  shift 3
  env \
    PATH="${root}/bin:${PATH}" \
    FAKE_DOCKER_LOG="${root}/docker.log" \
    FAKE_HOOK_LOG="${root}/hooks.log" \
    FAKE_PROMOTED_FILE="${root}/promoted" \
    FAKE_PRIMARY_COUNT_FILE="${root}/primary.count" \
    FAKE_CONTAINER_SIGNAL_FILE="${root}/container-signal" \
    FAKE_PRIMARY_SEQUENCE="${sequence}" \
    POSTGRES_FAILOVER_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" \
    POSTGRES_FAILOVER_NOW=150 \
    POSTGRES_FAILOVER_STATE_DIR="${root}/state" \
    POSTGRES_FAILOVER_FAILURE_THRESHOLD=2 \
    POSTGRES_FAILOVER_MIN_OUTAGE_SEC=30 \
    POSTGRES_FAILOVER_MAX_LAST_HEALTHY_AGE_SEC=120 \
    POSTGRES_FAILOVER_MAX_KNOWN_LAG_BYTES=0 \
    POSTGRES_FAILOVER_PRIMARY_HOST=100.100.1.8 \
    POSTGRES_FAILOVER_NEW_PRIMARY_HOST=100.100.1.5 \
    POSTGRES_FAILOVER_FENCE_COMMAND="${root}/hooks/fence.sh" \
    POSTGRES_FAILOVER_ROUTE_COMMAND="${root}/hooks/route.sh" \
    "$@" \
    /usr/bin/env bash "${CONTROLLER}" "${mode}" >"${root}/out.log" 2>"${root}/err.log"
}
static_deployment_contracts_are_wired() {
  local standby_compose="${ROOT_DIR}/deploy/compose/docker-compose.standby.yml"
  local compose_unit="${ROOT_DIR}/scripts/systemd/hololive-compose.service"
  local pattern
  for pattern in 'HOLOLIVE_STANDBY_POSTGRES_BIND_IP' '.hololive-promoted' 'pg_is_in_recovery()'; do
    grep -Fq "${pattern}" "${standby_compose}" || { fail "standby compose missing contract: ${pattern}"; return; }
  done
  for pattern in 'ConditionPathExists=!/var/lib/hololive-postgres-fence/fence.intent' 'ConditionPathExists=!/var/lib/hololive-postgres-fence/fenced'; do
    grep -Fq "${pattern}" "${compose_unit}" || { fail "compose unit missing fence condition: ${pattern}"; return; }
  done
  pass "standby health, bind, and old-primary boot fencing are wired"
}
healthy_primary_records_fresh_observation() {
  local root; root="$(setup_case healthy)"
  if ! run_controller "${root}" --dry-run up; then
    cat "${root}/err.log" >&2
    fail "healthy primary probe failed"
    return
  fi
  if ! grep -Fq $'1\t0\t0\t150\t0/20\t0/20\tmonitoring\t-' "${root}/state/state.tsv"; then
    cat "${root}/state/state.tsv" >&2
    fail "healthy observation was not persisted"
    return
  fi
  pass "healthy primary records a fresh replay observation"
}
standby_ahead_of_primary_fails_closed() {
  local root; root="$(setup_case ahead)"
  if run_controller "${root}" --dry-run up 'FAKE_LOCAL_STATUS=t|0/30|0/30|f|on'; then
    fail "standby ahead of primary unexpectedly passed"; return
  fi
  grep -Fq 'reason=standby_replay_ahead_of_primary' "${root}/err.log" || { cat "${root}/err.log" >&2; fail "missing divergent LSN guard"; return; }
  pass "standby LSN ahead of primary fails closed"
}
dry_run_never_fences_or_promotes() {
  local root; root="$(setup_case dry-run)"
  seed_ready_state "${root}"
  if ! run_controller "${root}" --dry-run down; then
    cat "${root}/err.log" >&2
    fail "dry-run returned failure"
    return
  fi
  if grep -Fq 'pg_promote' "${root}/docker.log" || [[ -e "${root}/promoted" ]]; then
    cat "${root}/docker.log" >&2
    fail "dry-run promoted the standby"
    return
  fi
  if ! grep -Fq 'event=promotion_would_run' "${root}/err.log"; then
    cat "${root}/err.log" >&2
    fail "dry-run did not report promotion candidate"
    return
  fi
  pass "dry-run performs no fence or promotion mutation"
}
invalid_fence_ack_blocks_promotion() {
  local root; root="$(setup_case invalid-fence)"
  seed_ready_state "${root}"
  if FAKE_FENCE_OUTPUT='NOPE|100.100.1.8|100.100.1.5:5434|bad-token-1234' run_controller "${root}" --apply down; then
    fail "invalid fence acknowledgement unexpectedly succeeded"
    return
  fi
  if grep -Fq 'pg_promote' "${root}/docker.log"; then
    cat "${root}/docker.log" >&2
    fail "promotion ran after invalid fence acknowledgement"
    return
  fi
  grep -Fq 'event=fence_invalid_ack' "${root}/err.log" || { cat "${root}/err.log" >&2; fail "missing fence_invalid_ack event"; return; }
  pass "invalid fence acknowledgement blocks promotion"
}
primary_recovery_before_fence_cancels_failover() {
  local root; root="$(setup_case primary-recovered)"
  seed_ready_state "${root}"
  run_controller "${root}" --apply 'down,up' || { cat "${root}/err.log" >&2; fail "primary recovery cancellation failed"; return; }
  [[ ! -s "${root}/hooks.log" ]] || { cat "${root}/hooks.log" >&2; fail "recovered primary was fenced"; return; }
  grep -Fq 'reason=primary_recovered_before_fence' "${root}/err.log" || { cat "${root}/err.log" >&2; fail "missing pre-fence recovery event"; return; }
  pass "primary recovery immediately before fencing cancels failover"
}
writable_old_primary_after_fence_blocks_promotion() {
  local root; root="$(setup_case old-primary-writable)"
  seed_ready_state "${root}"
  if run_controller "${root}" --apply 'down,down,up'; then
    fail "writable old primary after fence unexpectedly succeeded"
    return
  fi
  if grep -Fq 'pg_promote' "${root}/docker.log"; then
    cat "${root}/docker.log" >&2
    fail "promotion ran while old primary still accepted writes"
    return
  fi
  grep -Fq 'reason=old_primary_still_writable_after_fence' "${root}/err.log" || { cat "${root}/err.log" >&2; fail "missing post-fence writable-primary guard"; return; }
  pass "post-fence read/write reprobe prevents split brain"
}
fresh_fenced_standby_is_promoted_and_routed() {
  local root; root="$(setup_case promote)"
  seed_ready_state "${root}"
  if ! run_controller "${root}" --apply 'down,down'; then
    cat "${root}/err.log" >&2
    fail "safe promotion path failed"
    return
  fi
  [[ -e "${root}/promoted" ]] || { fail "fake standby was not promoted"; return; }
  [[ -e "${root}/container-signal" ]] || { fail "container promotion signal was not written"; return; }
  grep -Fq 'role=primary' "${root}/state/promoted" || { cat "${root}/state/promoted" >&2; fail "promoted marker missing role"; return; }
  grep -Fq 'route_state=complete' "${root}/state/promoted" || { cat "${root}/state/promoted" >&2; fail "route hook completion not persisted"; return; }
  grep -Fq $'\tpromoted\t' "${root}/state/state.tsv" || { cat "${root}/state/state.tsv" >&2; fail "state did not transition to promoted"; return; }
  grep -Fq 'event=promotion_complete' "${root}/err.log" || { cat "${root}/err.log" >&2; fail "missing promotion_complete event"; return; }
  pass "fresh fenced standby is promoted and route hook completes"
}
route_failure_is_persisted_and_retried_without_repromotion() {
  local root; root="$(setup_case route-retry)"
  seed_ready_state "${root}"
  if FAKE_ROUTE_OUTPUT='INVALID|100.100.1.5:5434|wrong-token' run_controller "${root}" --apply 'down,down'; then
    fail "invalid route acknowledgement unexpectedly completed promotion"
    return
  fi
  grep -Fq 'route_state=pending' "${root}/state/promoted" || { cat "${root}/state/promoted" >&2; fail "route failure was not durably marked pending"; return; }
  grep -Fq $'\tpromoted_route_failed\t' "${root}/state/state.tsv" || { cat "${root}/state/state.tsv" >&2; fail "route failure state was not persisted"; return; }
  if ! run_controller "${root}" --apply down; then
    cat "${root}/err.log" >&2
    fail "route retry failed"
    return
  fi
  grep -Fq 'route_state=complete' "${root}/state/promoted" || { cat "${root}/state/promoted" >&2; fail "route retry did not complete"; return; }
  [[ "$(grep -Fc 'pg_promote' "${root}/docker.log")" == "1" ]] || { cat "${root}/docker.log" >&2; fail "route retry issued another promotion"; return; }
  pass "route failure retries without a second promotion"
}
stale_observation_blocks_promotion() {
  local root; root="$(setup_case stale)"
  seed_ready_state "${root}" 1 1 1
  if run_controller "${root}" --apply down; then
    fail "stale observation unexpectedly allowed promotion"
    return
  fi
  if grep -Fq 'pg_promote' "${root}/docker.log"; then
    fail "promotion ran with stale observation"
    return
  fi
  grep -Fq 'reason=stale_healthy_observation' "${root}/err.log" || { cat "${root}/err.log" >&2; fail "missing stale observation guard"; return; }
  pass "stale healthy observation blocks promotion"
}
setup_primary_fence_fake_tools() {
  local root="$1"
  mkdir -p "${root}/bin"
  cat >"${root}/bin/ip" <<'EOF_IP'
#!/usr/bin/env bash
printf '1: tailscale0    inet 100.100.1.8/32 scope global tailscale0\n'
EOF_IP
  cat >"${root}/bin/systemctl" <<'EOF_SYSTEMCTL'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${FENCE_TOOL_LOG:?}"
if [[ "${1:-}" == "cat" ]]; then
  printf '%s\n' \
    'ConditionPathExists=!/var/lib/hololive-postgres-fence/fence.intent' \
    'ConditionPathExists=!/var/lib/hololive-postgres-fence/fenced'
fi
exit 0
EOF_SYSTEMCTL
  cat >"${root}/bin/docker" <<'EOF_DOCKER_FENCE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${FENCE_TOOL_LOG:?}"
case "${1:-}" in
  info) exit 0 ;;
  inspect)
    if [[ "${2:-}" == "-f" ]]; then
      case "${3:-}" in
        *State.Running*) printf 'false\n' ;;
        *RestartPolicy.Name*) printf 'no\n' ;;
      esac
    fi
    exit 0
    ;;
  update|stop) exit 0 ;;
  ps) exit 0 ;;
esac
exit 2
EOF_DOCKER_FENCE
  chmod 0755 "${root}/bin/ip" "${root}/bin/systemctl" "${root}/bin/docker"
}
missing_route_hook_blocks_before_fencing() {
  local root; root="$(setup_case missing-route)"
  seed_ready_state "${root}"
  if run_controller "${root}" --apply down POSTGRES_FAILOVER_ROUTE_COMMAND=; then
    fail "missing required route hook unexpectedly allowed apply"
    return
  fi
  if [[ -s "${root}/hooks.log" ]] || grep -Fq 'pg_promote' "${root}/docker.log"; then
    cat "${root}/hooks.log" "${root}/docker.log" >&2
    fail "missing route hook reached fence or promotion"
    return
  fi
  grep -Fq 'reason=route_hook_required' "${root}/err.log" || { cat "${root}/err.log" >&2; fail "missing route hook guard event"; return; }
  pass "required route hook is validated before fencing"
}
untrusted_hook_parent_blocks_before_fencing() {
  local root; root="$(setup_case untrusted-hook-parent)"
  seed_ready_state "${root}"
  chmod 0777 "${root}/hooks"
  if run_controller "${root}" --apply down; then fail "writable hook parent unexpectedly passed"; return; fi
  [[ ! -s "${root}/hooks.log" ]] || { cat "${root}/hooks.log" >&2; fail "untrusted hook parent reached fence"; return; }
  grep -Fq 'reason=trusted_path_group_or_world_writable' "${root}/err.log" || { cat "${root}/err.log" >&2; fail "missing hook parent trust failure"; return; }
  pass "writable hook parent is rejected before fencing"
}
stale_container_signal_blocks_standby_controller() {
  local root; root="$(setup_case stale-signal)"
  printf 'role=primary\n' >"${root}/container-signal"
  if run_controller "${root}" --dry-run up; then
    fail "standby with stale promotion signal unexpectedly passed"
    return
  fi
  grep -Fq 'reason=stale_container_promotion_signal_while_in_recovery' "${root}/err.log" || { cat "${root}/err.log" >&2; fail "missing stale container promotion signal guard"; return; }
  pass "stale container promotion signal fails closed"
}
unexpected_primary_without_marker_stays_unhealthy() {
  local root; root="$(setup_case unowned-primary)"
  : >"${root}/promoted"
  if run_controller "${root}" --apply down; then fail "unowned primary unexpectedly passed"; return; fi
  [[ ! -e "${root}/container-signal" ]] || { fail "unowned primary received a health signal"; return; }
  grep -Fq 'reason=unexpected_local_primary_without_marker' "${root}/err.log" || { cat "${root}/err.log" >&2; fail "missing unowned-primary guard"; return; }
  pass "primary without durable intent cannot acquire health signal"
}
crash_after_promotion_restores_signal_and_route() {
  local root; root="$(setup_case crash-recovery)"
  : >"${root}/promoted"
  cat >"${root}/state/promotion.intent" <<'EOF_INTENT'
role=promotion-intent
created_at=140
old_primary=100.100.1.8:5433
new_primary=100.100.1.5:5434
fence_token=fence-token-1234
last_primary_lsn=0/20
EOF_INTENT
  chmod 0600 "${root}/state/promotion.intent"
  if ! run_controller "${root}" --apply down; then
    cat "${root}/err.log" >&2
    fail "crash recovery finalization failed"
    return
  fi
  [[ -e "${root}/container-signal" ]] || { fail "crash recovery did not restore container signal"; return; }
  [[ ! -e "${root}/state/promotion.intent" ]] || { fail "crash recovery left intent marker"; return; }
  grep -Fq 'route_state=complete' "${root}/state/promoted" || { cat "${root}/state/promoted" >&2; fail "crash recovery did not complete route"; return; }
  if grep -Fq 'pg_promote' "${root}/docker.log"; then
    cat "${root}/docker.log" >&2
    fail "crash recovery attempted a second promotion"
    return
  fi
  pass "post-promotion crash recovery restores health signal and route"
}
primary_fence_is_persistent_and_idempotent() {
  local root output1 output2
  root="${TMP_DIR}/primary-fence"
  mkdir -p "${root}/state"
  setup_primary_fence_fake_tools "${root}"
  : >"${root}/tools.log"
  output1="$(PATH="${root}/bin:${PATH}" FENCE_TOOL_LOG="${root}/tools.log" POSTGRES_PRIMARY_FENCE_STATE_DIR="${root}/state" POSTGRES_PRIMARY_FENCE_ALLOW_TEST_STATE_DIR=1 POSTGRES_PRIMARY_FENCE_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" POSTGRES_PRIMARY_FENCE_NOW=200 /usr/bin/env bash "${PRIMARY_FENCE}" request-token-1234 100.100.1.8 100.100.1.5 5434)" || { fail "primary fence first run failed"; return; }
  output2="$(PATH="${root}/bin:${PATH}" FENCE_TOOL_LOG="${root}/tools.log" POSTGRES_PRIMARY_FENCE_STATE_DIR="${root}/state" POSTGRES_PRIMARY_FENCE_ALLOW_TEST_STATE_DIR=1 POSTGRES_PRIMARY_FENCE_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" POSTGRES_PRIMARY_FENCE_NOW=201 /usr/bin/env bash "${PRIMARY_FENCE}" another-token-5678 100.100.1.8 100.100.1.5 5434)" || { fail "primary fence idempotent run failed"; return; }
  [[ "${output1}" == 'FENCED|100.100.1.8|100.100.1.5:5434|request-token-1234' ]] || { printf '%s\n' "${output1}" >&2; fail "primary fence acknowledgement invalid"; return; }
  [[ "${output2}" == "${output1}" ]] || { printf '%s\n%s\n' "${output1}" "${output2}" >&2; fail "primary fence was not idempotent"; return; }
  grep -Fq 'stop hololive-compose.service' "${root}/tools.log" || { cat "${root}/tools.log" >&2; fail "compose unit was not stopped"; return; }
  grep -Fq 'update --restart=no deunhealth' "${root}/tools.log" || { cat "${root}/tools.log" >&2; fail "autoheal was not fenced"; return; }
  grep -Fq 'update --restart=no holo-postgres' "${root}/tools.log" || { cat "${root}/tools.log" >&2; fail "database restart policy was not fenced"; return; }
  [[ "$(grep -Fc 'update --restart=no holo-postgres' "${root}/tools.log")" == "4" ]] || { cat "${root}/tools.log" >&2; fail "idempotent fence did not re-verify/reapply database stop"; return; }
  grep -Fq 'state=fenced' "${root}/state/fenced" || { cat "${root}/state/fenced" >&2; fail "persistent fence marker missing"; return; }
  grep -Fq 'new_primary=100.100.1.5:5434' "${root}/state/fenced" || { cat "${root}/state/fenced" >&2; fail "fence candidate binding missing"; return; }
  if PATH="${root}/bin:${PATH}" FENCE_TOOL_LOG="${root}/tools.log" POSTGRES_PRIMARY_FENCE_STATE_DIR="${root}/state" POSTGRES_PRIMARY_FENCE_ALLOW_TEST_STATE_DIR=1 POSTGRES_PRIMARY_FENCE_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" /usr/bin/env bash "${PRIMARY_FENCE}" third-token-9012 100.100.1.8 100.100.1.6 5434 >/dev/null 2>&1; then fail "existing fence was reusable by another candidate"; return; fi
  pass "primary fence is persistent, idempotent, and candidate-bound"
}
for test_case in static_deployment_contracts_are_wired healthy_primary_records_fresh_observation standby_ahead_of_primary_fails_closed dry_run_never_fences_or_promotes \
  invalid_fence_ack_blocks_promotion primary_recovery_before_fence_cancels_failover writable_old_primary_after_fence_blocks_promotion fresh_fenced_standby_is_promoted_and_routed \
  route_failure_is_persisted_and_retried_without_repromotion stale_observation_blocks_promotion missing_route_hook_blocks_before_fencing untrusted_hook_parent_blocks_before_fencing \
  stale_container_signal_blocks_standby_controller unexpected_primary_without_marker_stays_unhealthy crash_after_promotion_restores_signal_and_route primary_fence_is_persistent_and_idempotent; do
  "${test_case}"
done
if (( failures > 0 )); then
  printf '[FAIL] postgres failover tests failed: %s\n' "${failures}" >&2
  exit 1
fi
printf 'ok: postgres failover tests passed\n'
