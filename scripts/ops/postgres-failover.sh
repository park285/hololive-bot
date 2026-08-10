#!/usr/bin/env bash
# postgres-failover.sh — PostgreSQL standby promotion watchdog (single evaluation)
#
# Default mode is --dry-run. --apply requires a trusted fence hook and promotes only
# after the old primary is fenced and a second read/write probe no longer succeeds.

set -euo pipefail

MODE="${1:---dry-run}"
case "${MODE}" in
  --dry-run|--apply) ;;
  *) printf 'Usage: %s [--dry-run|--apply]\n' "$0" >&2; exit 2 ;;
esac

STANDBY_CONTAINER="${POSTGRES_FAILOVER_STANDBY_CONTAINER:-holo-postgres-standby}"
DB_NAME="${POSTGRES_FAILOVER_DB_NAME:-hololive}"
LOCAL_DB_USER="${POSTGRES_FAILOVER_LOCAL_DB_USER:-postgres_admin}"
PROBE_USER="${POSTGRES_FAILOVER_PROBE_USER:-hololive_replicator}"
PRIMARY_HOST="${POSTGRES_FAILOVER_PRIMARY_HOST:-100.100.1.8}"
PRIMARY_PORT="${POSTGRES_FAILOVER_PRIMARY_PORT:-5433}"
NEW_PRIMARY_HOST="${POSTGRES_FAILOVER_NEW_PRIMARY_HOST:-100.100.1.5}"
NEW_PRIMARY_PORT="${POSTGRES_FAILOVER_NEW_PRIMARY_PORT:-5434}"
CONTAINER_PGPASS_FILE="${POSTGRES_FAILOVER_CONTAINER_PGPASS_FILE:-/run/hololive-bot/postgres/pgpass}"
CONTAINER_CA_FILE="${POSTGRES_FAILOVER_CONTAINER_CA_FILE:-/run/hololive-bot/certs/postgres-ca.pem}"
STATE_DIR="${POSTGRES_FAILOVER_STATE_DIR:-/var/lib/hololive-postgres-failover}"
STATE_FILE="${POSTGRES_FAILOVER_STATE_FILE:-${STATE_DIR}/state.tsv}"
LOCK_FILE="${POSTGRES_FAILOVER_LOCK_FILE:-${STATE_DIR}/controller.lock}"
INTENT_MARKER="${POSTGRES_FAILOVER_INTENT_MARKER:-${STATE_DIR}/promotion.intent}"
PROMOTED_MARKER="${POSTGRES_FAILOVER_PROMOTED_MARKER:-${STATE_DIR}/promoted}"
CONTAINER_PROMOTED_MARKER="${POSTGRES_FAILOVER_CONTAINER_PROMOTED_MARKER:-/var/lib/postgresql/pgdata/.hololive-promoted}"
FENCE_SCRIPT="${POSTGRES_FAILOVER_FENCE_COMMAND:-}"
ROUTE_SCRIPT="${POSTGRES_FAILOVER_ROUTE_COMMAND:-}"
FAILURE_THRESHOLD="${POSTGRES_FAILOVER_FAILURE_THRESHOLD:-4}"
MIN_OUTAGE_SEC="${POSTGRES_FAILOVER_MIN_OUTAGE_SEC:-45}"
MAX_LAST_HEALTHY_AGE_SEC="${POSTGRES_FAILOVER_MAX_LAST_HEALTHY_AGE_SEC:-120}"
MAX_KNOWN_LAG_BYTES="${POSTGRES_FAILOVER_MAX_KNOWN_LAG_BYTES:-0}"
PROBE_TIMEOUT_SEC="${POSTGRES_FAILOVER_PROBE_TIMEOUT_SEC:-3}"
PROMOTE_TIMEOUT_SEC="${POSTGRES_FAILOVER_PROMOTE_TIMEOUT_SEC:-60}"
FENCE_HOOK_TIMEOUT_SEC="${POSTGRES_FAILOVER_FENCE_HOOK_TIMEOUT_SEC:-180}"
ROUTE_HOOK_TIMEOUT_SEC="${POSTGRES_FAILOVER_ROUTE_HOOK_TIMEOUT_SEC:-90}"
REQUIRE_ROUTE_HOOK="${POSTGRES_FAILOVER_REQUIRE_ROUTE_HOOK:-1}"
ALLOW_NON_ROOT="${POSTGRES_FAILOVER_ALLOW_NON_ROOT_FOR_TEST:-0}"
NOW="${POSTGRES_FAILOVER_NOW:-$(/usr/bin/date +%s)}"

STATE_VERSION=1
STATE_VALID=1
FAILURE_COUNT=0
FIRST_FAILURE_AT=0
LAST_HEALTHY_AT=0
LAST_PRIMARY_LSN="0/0"
LAST_REPLAY_LSN="0/0"
PROMOTION_STATE="monitoring"
FENCE_TOKEN="-"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
CONTROLLER_PATH="$0"
if [[ "${CONTROLLER_PATH}" != /* ]]; then
  CONTROLLER_PATH="$(pwd -P)/${CONTROLLER_PATH#./}"
fi
CURRENT_UID="$(/usr/bin/id -u)"
case "${ALLOW_NON_ROOT}" in
  0) ;;
  1)
    if [[ "${CURRENT_UID}" == "0" || "${STATE_DIR}" != /tmp/* ]]; then
      printf '[postgres-failover] non-root test mode requires a non-root uid and /tmp state\n' >&2
      exit 1
    fi
    ;;
  *)
    printf '[postgres-failover] invalid POSTGRES_FAILOVER_ALLOW_NON_ROOT_FOR_TEST\n' >&2
    exit 2
    ;;
esac
verify_root_exec_tree() {
  local path current real owner mode_hex
  for path in \
    "${CONTROLLER_PATH}" \
    "${SCRIPT_DIR}/lib/postgres-failover-lib.sh" \
    "${SCRIPT_DIR}/lib/postgres-failover-transition-lib.sh"; do
    real="$(realpath -e -- "${path}" 2>/dev/null)" || {
      printf '[postgres-failover] untrusted executable path missing: %s\n' "${path}" >&2
      exit 1
    }
    [[ "${real}" == "${path}" && -f "${path}" && ! -L "${path}" ]] || {
      printf '[postgres-failover] executable path must be a canonical regular file: %s\n' "${path}" >&2
      exit 1
    }
    current="${path}"
    while :; do
      owner="$(stat -c '%u' -- "${current}")"
      mode_hex="$(stat -c '%f' -- "${current}")"
      if [[ "${owner}" != "0" && !( "${ALLOW_NON_ROOT}" == "1" && "${owner}" == "${CURRENT_UID}" ) ]]; then
        printf '[postgres-failover] executable path has an untrusted owner: %s\n' "${current}" >&2
        exit 1
      fi
      if (( (0x${mode_hex} & 0x0012) != 0 )) \
        && { [[ ! -d "${current}" || "${owner}" != "0" ]] || (( (0x${mode_hex} & 0x0200) == 0 )); }; then
        printf '[postgres-failover] executable path is not root-owned/read-only to non-root: %s\n' "${current}" >&2
        exit 1
      fi
      [[ "${current}" == "/" ]] && break
      current="$(dirname -- "${current}")"
    done
  done
}
verify_root_exec_tree
# shellcheck source=scripts/ops/lib/postgres-failover-lib.sh
source "${SCRIPT_DIR}/lib/postgres-failover-lib.sh"
# shellcheck source=scripts/ops/lib/postgres-failover-transition-lib.sh
source "${SCRIPT_DIR}/lib/postgres-failover-transition-lib.sh"

validate_scalar_inputs
validate_state_dir

exec 9>"${LOCK_FILE}"
if ! flock -n 9; then
  journal "lock_busy"
  exit 0
fi

read_state

LOCAL_RAW="$(local_status)" || die "local_probe_failed" "container=${STANDBY_CONTAINER}"
parse_local_status "${LOCAL_RAW}" || die "local_probe_invalid_output"

if [[ "${LOCAL_RECOVERY}" == "f" ]]; then
  [[ "${LOCAL_READ_ONLY}" == "off" ]] || die "local_primary_read_only"
  recover_or_handle_promoted_primary
  exit $?
fi

if [[ -e "${PROMOTED_MARKER}" || -e "${INTENT_MARKER}" ]]; then
  die "stale_promotion_marker_while_in_recovery"
fi
if container_promotion_signal_exists; then
  die "stale_container_promotion_signal_while_in_recovery" "path=${CONTAINER_PROMOTED_MARKER}"
fi

PRIMARY_FAILURE_REASON="unreachable"
if PRIMARY_RAW="$(primary_status 2>/dev/null)"; then
  if parse_primary_status "${PRIMARY_RAW}" && [[ "${PRIMARY_RECOVERY}" == "f" && "${PRIMARY_READ_ONLY}" == "off" ]]; then
    FAILURE_COUNT=0
    FIRST_FAILURE_AT=0
    PROMOTION_STATE="monitoring"
    FENCE_TOKEN="-"
    KNOWN_LAG="$(lsn_lag_bytes "${PRIMARY_LSN}" "${LOCAL_REPLAY_LSN}")" || die "lsn_diff_failed"
    LSN_ORDER="$(lsn_compare "${LOCAL_REPLAY_LSN}" "${PRIMARY_LSN}")" || die "lsn_compare_failed"
    (( LSN_ORDER <= 0 )) || die "standby_replay_ahead_of_primary" "primary_lsn=${PRIMARY_LSN}" "replay_lsn=${LOCAL_REPLAY_LSN}"
    if [[ "${LOCAL_REPLAY_PAUSED}" == "f" ]] && (( KNOWN_LAG <= MAX_KNOWN_LAG_BYTES )); then
      LAST_HEALTHY_AT="${NOW}"
      LAST_PRIMARY_LSN="${PRIMARY_LSN}"
      LAST_REPLAY_LSN="${LOCAL_REPLAY_LSN}"
      STATE_VALID=1
      journal "primary_healthy" "primary_lsn=${PRIMARY_LSN}" "replay_lsn=${LOCAL_REPLAY_LSN}" "lag_bytes=${KNOWN_LAG}"
    else
      journal "primary_healthy_standby_not_fresh" "primary_lsn=${PRIMARY_LSN}" "replay_lsn=${LOCAL_REPLAY_LSN}" "lag_bytes=${KNOWN_LAG}" "replay_paused=${LOCAL_REPLAY_PAUSED}"
    fi
    write_state
    exit 0
  fi
  PRIMARY_FAILURE_REASON="role_invalid"
fi

if (( FAILURE_COUNT == 0 )); then
  FIRST_FAILURE_AT="${NOW}"
fi
FAILURE_COUNT=$((FAILURE_COUNT + 1))
PROMOTION_STATE="monitoring"
write_state
OUTAGE_SEC=$((NOW - FIRST_FAILURE_AT))
journal "primary_probe_failed" "reason=${PRIMARY_FAILURE_REASON}" "failure_count=${FAILURE_COUNT}" "outage_sec=${OUTAGE_SEC}"

if (( FAILURE_COUNT < FAILURE_THRESHOLD || OUTAGE_SEC < MIN_OUTAGE_SEC )); then
  exit 0
fi

freshness_guard || exit 1

if [[ "${MODE}" == "--dry-run" ]]; then
  journal "promotion_would_run" "primary=${PRIMARY_HOST}:${PRIMARY_PORT}" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" "failure_count=${FAILURE_COUNT}"
  exit 0
fi

if [[ "$(/usr/bin/id -u)" != "0" && "${ALLOW_NON_ROOT}" != "1" ]]; then
  die "apply_requires_root"
fi
[[ -n "${FENCE_SCRIPT}" ]] || die "fence_hook_required"
validate_hook_script "fence" "${FENCE_SCRIPT}"
if [[ "${REQUIRE_ROUTE_HOOK}" == "1" ]]; then
  [[ -n "${ROUTE_SCRIPT}" ]] || die "route_hook_required"
  validate_hook_script "route" "${ROUTE_SCRIPT}"
elif [[ -n "${ROUTE_SCRIPT}" ]]; then
  validate_hook_script "route" "${ROUTE_SCRIPT}"
fi

# Close the gap between the threshold probe and the destructive fence. A primary
# that recovered while hooks were being validated must not be fenced.
if PRIMARY_PRE_FENCE="$(primary_status 2>/dev/null)"; then
  parse_primary_status "${PRIMARY_PRE_FENCE}" || die "old_primary_pre_fence_invalid_output"
  if [[ "${PRIMARY_RECOVERY}" == "f" && "${PRIMARY_READ_ONLY}" == "off" ]]; then
    FAILURE_COUNT=0
    FIRST_FAILURE_AT=0
    PROMOTION_STATE="monitoring"
    FENCE_TOKEN="-"
    write_state
    journal "promotion_cancelled" "reason=primary_recovered_before_fence" "primary=${PRIMARY_HOST}:${PRIMARY_PORT}"
    exit 0
  fi
  journal "old_primary_pre_fence_nonwritable" "recovery=${PRIMARY_RECOVERY}" "read_only=${PRIMARY_READ_ONLY}"
fi

run_fence_hook || exit 1

if PRIMARY_AFTER_FENCE="$(primary_status 2>/dev/null)"; then
  parse_primary_status "${PRIMARY_AFTER_FENCE}" || die "old_primary_post_fence_invalid_output"
  if [[ "${PRIMARY_RECOVERY}" == "f" && "${PRIMARY_READ_ONLY}" == "off" ]]; then
    journal "promotion_blocked" "reason=old_primary_still_writable_after_fence" "primary=${PRIMARY_HOST}:${PRIMARY_PORT}"
    exit 1
  fi
  [[ "${PRIMARY_READ_ONLY}" == "on" ]] || die "old_primary_post_fence_role_inconsistent" "recovery=${PRIMARY_RECOVERY}" "read_only=${PRIMARY_READ_ONLY}"
  journal "old_primary_post_fence_nonwritable" "recovery=${PRIMARY_RECOVERY}" "read_only=${PRIMARY_READ_ONLY}"
fi

LOCAL_RAW="$(local_status)" || die "local_reprobe_failed"
parse_local_status "${LOCAL_RAW}" || die "local_reprobe_invalid_output"
freshness_guard || exit 1

write_intent_marker || die "promotion_intent_write_failed"
if ! promote_local; then
  journal "promotion_failed" "container=${STANDBY_CONTAINER}"
  exit 1
fi

LOCAL_RAW="$(local_status)" || die "post_promotion_probe_failed"
parse_local_status "${LOCAL_RAW}" || die "post_promotion_probe_invalid_output"
if [[ "${LOCAL_RECOVERY}" != "f" || "${LOCAL_READ_ONLY}" != "off" ]]; then
  die "post_promotion_role_invalid" "recovery=${LOCAL_RECOVERY}" "read_only=${LOCAL_READ_ONLY}"
fi
write_container_promotion_signal || die "promotion_signal_write_failed" "path=${CONTAINER_PROMOTED_MARKER}"
journal "promotion_signal_written" "path=${CONTAINER_PROMOTED_MARKER}"

if complete_route_state; then
  remove_durable_file "${INTENT_MARKER}" || die "promotion_intent_remove_failed"
  write_state
  journal "promotion_complete" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" "fence_token=${FENCE_TOKEN}"
  exit 0
else
  ROUTE_RESULT=$?
fi

if [[ "${ROUTE_RESULT}" == "1" && -r "${PROMOTED_MARKER}" ]]; then
  remove_durable_file "${INTENT_MARKER}" || die "promotion_intent_remove_failed"
fi
write_state
if [[ "${ROUTE_RESULT}" == "1" ]]; then
  journal "promotion_complete_route_pending" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" "fence_token=${FENCE_TOKEN}"
else
  journal "promotion_finalize_persistence_failed" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" "fence_token=${FENCE_TOKEN}"
fi
exit "${ROUTE_RESULT}"
