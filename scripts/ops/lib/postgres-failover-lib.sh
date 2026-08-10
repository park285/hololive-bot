# shellcheck shell=bash
# Internal library for postgres-failover.sh. Source only from the root-owned deploy tree.
journal() {
  local event="$1"
  shift || true
  local line="ts=${NOW} mode=${MODE} event=${event}"
  if (( $# > 0 )); then
    line+=" $*"
  fi
  printf '[postgres-failover] %s\n' "${line}" >&2
}
die() {
  local reason="$1"
  shift || true
  journal "fatal" "reason=${reason}" "$@"
  exit 1
}
is_uint() { [[ "$1" =~ ^[0-9]+$ ]] && (( ${#1} <= 18 )); }
is_bool01() { [[ "$1" == "0" || "$1" == "1" ]]; }
is_lsn() { [[ "$1" =~ ^[0-9A-Fa-f]{1,8}/[0-9A-Fa-f]{1,8}$ ]]; }
is_token() { [[ "$1" =~ ^[A-Za-z0-9._:-]{8,128}$ ]]; }
is_host() { [[ "$1" =~ ^[A-Za-z0-9._:-]+$ ]]; }
is_simple_name() { [[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]]; }
is_clean_abs_path() {
  [[ "$1" == /* && "$1" != *$'\n'* && "$1" != *$'\r'* && "/$1/" != */../* && "/$1/" != */./* ]]
}
validate_scalar_inputs() {
  local name value
  while IFS='=' read -r name value; do
    is_uint "${value}" || die "invalid_integer" "input=${name}" "value=${value}"
  done <<EOF_NUMERIC
PRIMARY_PORT=${PRIMARY_PORT}
NEW_PRIMARY_PORT=${NEW_PRIMARY_PORT}
LOCAL_PORT=${LOCAL_PORT}
FAILURE_THRESHOLD=${FAILURE_THRESHOLD}
MIN_OUTAGE_SEC=${MIN_OUTAGE_SEC}
MAX_LAST_HEALTHY_AGE_SEC=${MAX_LAST_HEALTHY_AGE_SEC}
MAX_KNOWN_LAG_BYTES=${MAX_KNOWN_LAG_BYTES}
PROBE_TIMEOUT_SEC=${PROBE_TIMEOUT_SEC}
PROMOTE_TIMEOUT_SEC=${PROMOTE_TIMEOUT_SEC}
FENCE_HOOK_TIMEOUT_SEC=${FENCE_HOOK_TIMEOUT_SEC}
ROUTE_HOOK_TIMEOUT_SEC=${ROUTE_HOOK_TIMEOUT_SEC}
NOW=${NOW}
EOF_NUMERIC
  PRIMARY_PORT=$((10#${PRIMARY_PORT}))
  NEW_PRIMARY_PORT=$((10#${NEW_PRIMARY_PORT}))
  LOCAL_PORT=$((10#${LOCAL_PORT}))
  FAILURE_THRESHOLD=$((10#${FAILURE_THRESHOLD}))
  MIN_OUTAGE_SEC=$((10#${MIN_OUTAGE_SEC}))
  MAX_LAST_HEALTHY_AGE_SEC=$((10#${MAX_LAST_HEALTHY_AGE_SEC}))
  MAX_KNOWN_LAG_BYTES=$((10#${MAX_KNOWN_LAG_BYTES}))
  PROBE_TIMEOUT_SEC=$((10#${PROBE_TIMEOUT_SEC}))
  PROMOTE_TIMEOUT_SEC=$((10#${PROMOTE_TIMEOUT_SEC}))
  FENCE_HOOK_TIMEOUT_SEC=$((10#${FENCE_HOOK_TIMEOUT_SEC}))
  ROUTE_HOOK_TIMEOUT_SEC=$((10#${ROUTE_HOOK_TIMEOUT_SEC}))
  NOW=$((10#${NOW}))
  (( PRIMARY_PORT > 0 && PRIMARY_PORT <= 65535 )) || die "invalid_port" "input=PRIMARY_PORT" "value=${PRIMARY_PORT}"
  (( NEW_PRIMARY_PORT > 0 && NEW_PRIMARY_PORT <= 65535 )) || die "invalid_port" "input=NEW_PRIMARY_PORT" "value=${NEW_PRIMARY_PORT}"
  (( LOCAL_PORT > 0 && LOCAL_PORT <= 65535 )) || die "invalid_port" "input=LOCAL_PORT" "value=${LOCAL_PORT}"
  (( FAILURE_THRESHOLD > 0 )) || die "invalid_threshold" "value=${FAILURE_THRESHOLD}"
  (( PROBE_TIMEOUT_SEC > 0 )) || die "invalid_probe_timeout" "value=${PROBE_TIMEOUT_SEC}"
  (( PROMOTE_TIMEOUT_SEC > 0 )) || die "invalid_promote_timeout" "value=${PROMOTE_TIMEOUT_SEC}"
  (( FENCE_HOOK_TIMEOUT_SEC > 0 )) || die "invalid_fence_hook_timeout" "value=${FENCE_HOOK_TIMEOUT_SEC}"
  (( ROUTE_HOOK_TIMEOUT_SEC > 0 )) || die "invalid_route_hook_timeout" "value=${ROUTE_HOOK_TIMEOUT_SEC}"
  (( MAX_KNOWN_LAG_BYTES <= 4294967295 )) || die "lag_budget_too_large" "value=${MAX_KNOWN_LAG_BYTES}"
  is_bool01 "${REQUIRE_ROUTE_HOOK}" || die "invalid_boolean" "input=POSTGRES_FAILOVER_REQUIRE_ROUTE_HOOK" "value=${REQUIRE_ROUTE_HOOK}"
  is_bool01 "${ALLOW_NON_ROOT}" || die "invalid_boolean" "input=POSTGRES_FAILOVER_ALLOW_NON_ROOT_FOR_TEST" "value=${ALLOW_NON_ROOT}"
  if [[ "${ALLOW_NON_ROOT}" == "1" && ( "$(/usr/bin/id -u)" == "0" || "${STATE_DIR}" != /tmp/* ) ]]; then
    die "test_bypass_requires_tmp_state" "path=${STATE_DIR}"
  fi
  is_host "${PRIMARY_HOST}" || die "invalid_primary_host" "value=${PRIMARY_HOST}"
  is_host "${NEW_PRIMARY_HOST}" || die "invalid_new_primary_host" "value=${NEW_PRIMARY_HOST}"
  is_host "${LOCAL_HOST}" || die "invalid_local_host" "value=${LOCAL_HOST}"
  is_simple_name "${SERVICE_USER}" || die "invalid_service_user" "value=${SERVICE_USER}"
  is_simple_name "${DB_NAME}" || die "invalid_database_name" "value=${DB_NAME}"
  is_simple_name "${PROBE_USER}" || die "invalid_probe_database_user" "value=${PROBE_USER}"
  is_clean_abs_path "${PGPASS_FILE}" || die "invalid_pgpass_path" "path=${PGPASS_FILE}"
  is_clean_abs_path "${CA_FILE}" || die "invalid_ca_path" "path=${CA_FILE}"
  is_clean_abs_path "${PSQL_PATH}" || die "invalid_psql_path" "path=${PSQL_PATH}"
}
path_component_is_trusted() {
  local label="$1" path="$2" owner mode_hex mode
  [[ ! -L "${path}" && -e "${path}" ]] || die "trusted_path_missing_or_symlink" "path_label=${label}" "path=${path}"
  owner="$(stat -c '%u' -- "${path}")" || die "trusted_path_stat_failed" "path_label=${label}" "path=${path}"
  if [[ "${owner}" != "0" && "${owner}" != "$(/usr/bin/id -u)" ]]; then
    die "trusted_path_invalid_owner" "path_label=${label}" "path=${path}" "owner=${owner}"
  fi
  mode_hex="$(stat -c '%f' -- "${path}")" || die "trusted_path_stat_failed" "path_label=${label}" "path=${path}"
  mode=$((0x${mode_hex}))
  if (( (mode & 0x0012) != 0 )); then
    # Test fixtures live below the conventional root-owned sticky /tmp. A sticky
    # root directory is safe as a parent because every descendant component is
    # checked separately and the hook file itself must be owned by the caller.
    if [[ ! -d "${path}" || "${owner}" != "0" ]] || (( (mode & 0x0200) == 0 )); then
      die "trusted_path_group_or_world_writable" "path_label=${label}" "path=${path}"
    fi
  fi
}
validate_trusted_path_chain() {
  local label="$1" path="$2" current
  current="${path}"
  while :; do
    path_component_is_trusted "${label}" "${current}"
    [[ "${current}" == "/" ]] && break
    current="$(dirname -- "${current}")"
  done
}
path_is_direct_child() {
  local path="$1"
  [[ "${path}" == "${STATE_DIR}/"* ]] || return 1
  [[ "${path#${STATE_DIR}/}" != */* ]]
}
validate_state_dir() {
  [[ "${STATE_DIR}" == /* ]] || die "state_dir_not_absolute" "path=${STATE_DIR}"
  install -d -m 0700 -- "${STATE_DIR}"
  local real owner mode_hex
  real="$(realpath -e -- "${STATE_DIR}")" || die "state_dir_realpath_failed" "path=${STATE_DIR}"
  [[ "${real}" == "${STATE_DIR}" ]] || die "state_dir_symlink_or_noncanonical" "path=${STATE_DIR}" "real=${real}"
  owner="$(stat -c '%u' -- "${STATE_DIR}")" || die "state_dir_stat_failed" "path=${STATE_DIR}"
  [[ "${owner}" == "$(/usr/bin/id -u)" ]] || die "state_dir_invalid_owner" "path=${STATE_DIR}" "owner=${owner}"
  mode_hex="$(stat -c '%f' -- "${STATE_DIR}")" || die "state_dir_stat_failed" "path=${STATE_DIR}"
  (( (0x${mode_hex} & 0x0012) == 0 )) || die "state_dir_group_or_world_writable" "path=${STATE_DIR}"
  local managed
  for managed in "${STATE_FILE}" "${LOCK_FILE}" "${INTENT_MARKER}" "${PROMOTED_MARKER}" "${HEALTH_SIGNAL}"; do
    path_is_direct_child "${managed}" || die "managed_path_outside_state_dir" "path=${managed}"
  done
}
validate_hook_script() {
  local label="$1" path="$2" real
  [[ -n "${path}" ]] || return 1
  [[ "${path}" == /* ]] || die "hook_not_absolute" "hook=${label}" "path=${path}"
  real="$(realpath -e -- "${path}" 2>/dev/null)" || die "hook_missing" "hook=${label}" "path=${path}"
  [[ "${real}" == "${path}" ]] || die "hook_symlink_or_noncanonical" "hook=${label}" "path=${path}" "real=${real}"
  [[ -f "${path}" ]] || die "hook_not_regular_file" "hook=${label}" "path=${path}"
  validate_trusted_path_chain "hook:${label}" "${path}"
}
read_state() {
  [[ -r "${STATE_FILE}" ]] || return 0
  local version extra
  IFS=$'\t' read -r version FAILURE_COUNT FIRST_FAILURE_AT LAST_HEALTHY_AT LAST_PRIMARY_LSN LAST_REPLAY_LSN PROMOTION_STATE FENCE_TOKEN extra <"${STATE_FILE}" || STATE_VALID=0
  [[ -z "${extra:-}" ]] || STATE_VALID=0
  [[ "${version:-}" == "${STATE_VERSION}" ]] || STATE_VALID=0
  is_uint "${FAILURE_COUNT:-}" || STATE_VALID=0
  is_uint "${FIRST_FAILURE_AT:-}" || STATE_VALID=0
  is_uint "${LAST_HEALTHY_AT:-}" || STATE_VALID=0
  is_lsn "${LAST_PRIMARY_LSN:-}" || STATE_VALID=0
  is_lsn "${LAST_REPLAY_LSN:-}" || STATE_VALID=0
  [[ "${PROMOTION_STATE:-}" =~ ^(monitoring|promoted|promoted_route_failed)$ ]] || STATE_VALID=0
  [[ "${FENCE_TOKEN:-}" == "-" ]] || is_token "${FENCE_TOKEN:-}" || STATE_VALID=0
  if [[ "${STATE_VALID}" == "1" ]]; then
    FAILURE_COUNT=$((10#${FAILURE_COUNT}))
    FIRST_FAILURE_AT=$((10#${FIRST_FAILURE_AT}))
    LAST_HEALTHY_AT=$((10#${LAST_HEALTHY_AT}))
  else
    FAILURE_COUNT=0
    FIRST_FAILURE_AT=0
    LAST_HEALTHY_AT=0
    LAST_PRIMARY_LSN="0/0"
    LAST_REPLAY_LSN="0/0"
    PROMOTION_STATE="monitoring"
    FENCE_TOKEN="-"
    journal "state_invalid" "path=${STATE_FILE}"
  fi
}
write_state() {
  local tmp="${STATE_FILE}.tmp.$$"
  umask 077
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "${STATE_VERSION}" "${FAILURE_COUNT}" "${FIRST_FAILURE_AT}" "${LAST_HEALTHY_AT}" \
    "${LAST_PRIMARY_LSN}" "${LAST_REPLAY_LSN}" "${PROMOTION_STATE}" "${FENCE_TOKEN}" >"${tmp}" || return 1
  chmod 0600 "${tmp}" || { rm -f -- "${tmp}"; return 1; }
  sync -f "${tmp}" || { rm -f -- "${tmp}"; return 1; }
  mv -f -- "${tmp}" "${STATE_FILE}" || { rm -f -- "${tmp}"; return 1; }
  sync -f "${STATE_FILE}" && sync -f "${STATE_DIR}"
}
atomic_write_file() {
  local target="$1"
  local tmp="${target}.tmp.$$"
  umask 077
  cat >"${tmp}" || return 1
  chmod 0600 "${tmp}" || { rm -f -- "${tmp}"; return 1; }
  sync -f "${tmp}" || { rm -f -- "${tmp}"; return 1; }
  mv -f -- "${tmp}" "${target}" || { rm -f -- "${tmp}"; return 1; }
  sync -f "${target}" && sync -f "$(dirname -- "${target}")"
}
remove_durable_file() {
  local target="$1"
  rm -f -- "${target}" || return 1
  sync -f "$(dirname -- "${target}")"
}
marker_value() {
  local file="$1" key="$2"
  awk -F= -v key="${key}" '$1 == key {sub(/^[^=]*=/, ""); print; exit}' "${file}" 2>/dev/null || true
}
validate_client_inputs() {
  local label path private mode_hex
  [[ -x "${PSQL_PATH}" ]] || die "psql_not_executable" "path=${PSQL_PATH}"
  validate_trusted_path_chain "psql" "${PSQL_PATH}"
  for label in pgpass ca; do
    if [[ "${label}" == "pgpass" ]]; then path="${PGPASS_FILE}"; private=1; else path="${CA_FILE}"; private=0; fi
    [[ -r "${path}" && -f "${path}" && ! -L "${path}" ]] || die "client_file_unreadable" "file=${label}" "path=${path}"
    validate_trusted_path_chain "client:${label}" "${path}"
    if [[ "${private}" == "1" ]]; then
      mode_hex="$(stat -c '%f' -- "${path}")" || die "client_file_stat_failed" "file=${label}"
      (( (0x${mode_hex} & 0x003f) == 0 )) || die "client_file_not_private" "file=${label}"
    fi
  done
}
promotion_signal_exists() {
  [[ -r "${HEALTH_SIGNAL}" ]] && grep -qx 'role=primary' "${HEALTH_SIGNAL}"
}
write_promotion_signal() {
  local tmp="${HEALTH_SIGNAL}.tmp.$$"
  umask 022
  printf 'role=primary\n' >"${tmp}" || return 1
  chmod 0644 "${tmp}" || { rm -f -- "${tmp}"; return 1; }
  sync -f "${tmp}" || { rm -f -- "${tmp}"; return 1; }
  mv -f -- "${tmp}" "${HEALTH_SIGNAL}" || { rm -f -- "${tmp}"; return 1; }
  sync -f "${HEALTH_SIGNAL}" && sync -f "${STATE_DIR}"
}
psql_status() {
  local host="$1" port="$2" sql="$3" command_timeout=$((PROBE_TIMEOUT_SEC + 2))
  PGPASSFILE="${PGPASS_FILE}" PGSSLMODE=verify-full PGSSLROOTCERT="${CA_FILE}" \
    PGCONNECT_TIMEOUT="${PROBE_TIMEOUT_SEC}" PGOPTIONS="-c statement_timeout=${PROBE_TIMEOUT_SEC}s" \
    /usr/bin/timeout --foreground --kill-after=2 "${command_timeout}s" \
    "${PSQL_PATH}" -X -v ON_ERROR_STOP=1 -AtF '|' -h "${host}" -p "${port}" \
    -U "${PROBE_USER}" -d "${DB_NAME}" -c "${sql}"
}
local_status() {
  psql_status "${LOCAL_HOST}" "${LOCAL_PORT}" "WITH role AS (SELECT pg_is_in_recovery() AS in_recovery) SELECT in_recovery, CASE WHEN in_recovery THEN COALESCE(pg_last_wal_receive_lsn()::text,'0/0') ELSE '0/0' END, CASE WHEN in_recovery THEN COALESCE(pg_last_wal_replay_lsn()::text,'0/0') ELSE '0/0' END, CASE WHEN in_recovery THEN pg_is_wal_replay_paused() ELSE false END, current_setting('transaction_read_only') FROM role"
}
primary_status() {
  psql_status "${PRIMARY_HOST}" "${PRIMARY_PORT}" "SELECT pg_is_in_recovery(), pg_current_wal_lsn()::text, current_setting('transaction_read_only')"
}
parse_local_status() {
  local raw="$1" extra
  [[ "${raw}" != *$'\n'* ]] || return 1
  IFS='|' read -r LOCAL_RECOVERY LOCAL_RECEIVE_LSN LOCAL_REPLAY_LSN LOCAL_REPLAY_PAUSED LOCAL_READ_ONLY extra <<<"${raw}"
  [[ -z "${extra:-}" ]] || return 1
  [[ "${LOCAL_RECOVERY}" == "t" || "${LOCAL_RECOVERY}" == "f" ]] || return 1
  is_lsn "${LOCAL_RECEIVE_LSN}" || return 1
  is_lsn "${LOCAL_REPLAY_LSN}" || return 1
  [[ "${LOCAL_REPLAY_PAUSED}" == "t" || "${LOCAL_REPLAY_PAUSED}" == "f" ]] || return 1
  [[ "${LOCAL_READ_ONLY}" == "on" || "${LOCAL_READ_ONLY}" == "off" ]] || return 1
}

parse_primary_status() {
  local raw="$1" extra
  [[ "${raw}" != *$'\n'* ]] || return 1
  IFS='|' read -r PRIMARY_RECOVERY PRIMARY_LSN PRIMARY_READ_ONLY extra <<<"${raw}"
  [[ -z "${extra:-}" ]] || return 1
  [[ "${PRIMARY_RECOVERY}" == "t" || "${PRIMARY_RECOVERY}" == "f" ]] || return 1
  is_lsn "${PRIMARY_LSN}" || return 1
  [[ "${PRIMARY_READ_ONLY}" == "on" || "${PRIMARY_READ_ONLY}" == "off" ]] || return 1
}

lsn_compare() {
  local left="$1" right="$2" left_hi left_lo right_hi right_lo
  is_lsn "${left}" && is_lsn "${right}" || return 1
  left_hi=$((16#${left%/*})); left_lo=$((16#${left#*/}))
  right_hi=$((16#${right%/*})); right_lo=$((16#${right#*/}))
  if (( left_hi < right_hi || (left_hi == right_hi && left_lo < right_lo) )); then
    printf '%s\n' -1
  elif (( left_hi == right_hi && left_lo == right_lo )); then
    printf '%s\n' 0
  else
    printf '%s\n' 1
  fi
}
lsn_lag_bytes() {
  local ahead="$1" behind="$2" cmp ahead_hi ahead_lo behind_hi behind_lo hi_diff
  cmp="$(lsn_compare "${ahead}" "${behind}")" || return 1
  if (( cmp <= 0 )); then printf '0\n'; return 0; fi
  ahead_hi=$((16#${ahead%/*})); ahead_lo=$((16#${ahead#*/}))
  behind_hi=$((16#${behind%/*})); behind_lo=$((16#${behind#*/}))
  hi_diff=$((ahead_hi - behind_hi))
  if (( hi_diff == 0 )); then
    printf '%s\n' "$((ahead_lo - behind_lo))"
  elif (( hi_diff == 1 )); then
    printf '%s\n' "$((4294967296 - behind_lo + ahead_lo))"
  else
    # The configured budget is capped below 4 GiB, so a larger high-word gap
    # only needs a stable over-budget sentinel and never risks signed overflow.
    printf '4294967296\n'
  fi
}
local_has_replayed_saved_primary() {
  local cmp
  cmp="$(lsn_compare "${LOCAL_REPLAY_LSN}" "${LAST_PRIMARY_LSN}")" || return 1
  (( cmp >= 0 ))
}

freshness_guard() {
  local age saved_lag
  [[ "${STATE_VALID}" == "1" ]] || { journal "promotion_blocked" "reason=invalid_state"; return 1; }
  (( LAST_HEALTHY_AT > 0 )) || { journal "promotion_blocked" "reason=no_healthy_observation"; return 1; }
  (( LAST_HEALTHY_AT <= NOW )) || { journal "promotion_blocked" "reason=healthy_observation_from_future"; return 1; }
  age=$((NOW - LAST_HEALTHY_AT))
  (( age <= MAX_LAST_HEALTHY_AGE_SEC )) || { journal "promotion_blocked" "reason=stale_healthy_observation" "age_sec=${age}"; return 1; }
  [[ "${LOCAL_RECOVERY}" == "t" && "${LOCAL_READ_ONLY}" == "on" ]] || { journal "promotion_blocked" "reason=local_not_read_only_standby"; return 1; }
  [[ "${LOCAL_REPLAY_PAUSED}" == "f" ]] || { journal "promotion_blocked" "reason=wal_replay_paused"; return 1; }
  saved_lag="$(lsn_lag_bytes "${LAST_PRIMARY_LSN}" "${LAST_REPLAY_LSN}")" || { journal "promotion_blocked" "reason=invalid_saved_lsn"; return 1; }
  (( saved_lag <= MAX_KNOWN_LAG_BYTES )) || { journal "promotion_blocked" "reason=saved_lag_exceeds_budget" "lag_bytes=${saved_lag}"; return 1; }
  local_has_replayed_saved_primary || { journal "promotion_blocked" "reason=saved_primary_lsn_not_replayed" "primary_lsn=${LAST_PRIMARY_LSN}" "replay_lsn=${LOCAL_REPLAY_LSN}"; return 1; }
  return 0
}

refresh_now() {
  local refreshed
  if [[ -n "${POSTGRES_FAILOVER_NOW:-}" ]]; then
    refreshed="${POSTGRES_FAILOVER_POST_FENCE_NOW:-${POSTGRES_FAILOVER_NOW}}"
  else
    refreshed="$(/usr/bin/date +%s)" || return 1
  fi
  is_uint "${refreshed}" || return 1
  refreshed=$((10#${refreshed}))
  (( refreshed >= NOW )) || return 1
  NOW="${refreshed}"
}
