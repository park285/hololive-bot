# shellcheck shell=bash
# Promotion, fencing, and route transitions for postgres-failover.sh.
new_request_id() {
  local id
  id="$(cat /proc/sys/kernel/random/uuid 2>/dev/null || true)"
  if ! is_token "${id}"; then
    id="failover-${NOW}-$$"
  fi
  printf '%s\n' "${id}"
}

run_fence_hook() {
  validate_hook_script "fence" "${FENCE_SCRIPT}"
  local request_id output tag host endpoint acknowledged_request token extra
  request_id="$(new_request_id)"
  export POSTGRES_FAILOVER_REQUEST_ID="${request_id}"
  export POSTGRES_FAILOVER_PRIMARY_HOST="${PRIMARY_HOST}"
  export POSTGRES_FAILOVER_PRIMARY_PORT="${PRIMARY_PORT}"
  export POSTGRES_FAILOVER_NEW_PRIMARY_HOST="${NEW_PRIMARY_HOST}"
  export POSTGRES_FAILOVER_NEW_PRIMARY_PORT="${NEW_PRIMARY_PORT}"
  export POSTGRES_FAILOVER_FAILURE_SINCE="${FIRST_FAILURE_AT}"
  export POSTGRES_FAILOVER_LAST_PRIMARY_LSN="${LAST_PRIMARY_LSN}"
  journal "fence_start" "primary=${PRIMARY_HOST}:${PRIMARY_PORT}" "request_id=${request_id}"
  if ! output="$(/usr/bin/timeout --foreground --kill-after=5 "${FENCE_HOOK_TIMEOUT_SEC}s" /usr/bin/env bash "${FENCE_SCRIPT}")"; then
    journal "fence_failed" "primary=${PRIMARY_HOST}:${PRIMARY_PORT}" "request_id=${request_id}"
    return 1
  fi
  if [[ "${output}" == *$'\n'* ]]; then
    journal "fence_invalid_ack" "reason=multiple_lines" "primary=${PRIMARY_HOST}:${PRIMARY_PORT}" "request_id=${request_id}"
    return 1
  fi
  IFS='|' read -r tag host endpoint acknowledged_request token extra <<<"${output}"
  if [[ -n "${extra:-}" || "${tag}" != "FENCED" || "${host}" != "${PRIMARY_HOST}" \
    || "${endpoint}" != "${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" \
    || "${acknowledged_request:-}" != "${request_id}" ]] || ! is_token "${token:-}"; then
    journal "fence_invalid_ack" "primary=${PRIMARY_HOST}:${PRIMARY_PORT}" "request_id=${request_id}"
    return 1
  fi
  FENCE_TOKEN="${token}"
  journal "fence_acknowledged" "primary=${PRIMARY_HOST}:${PRIMARY_PORT}" "token=${FENCE_TOKEN}"
  return 0
}

run_route_hook() {
  [[ -n "${ROUTE_SCRIPT}" ]] || return 2
  validate_hook_script "route" "${ROUTE_SCRIPT}"
  export POSTGRES_FAILOVER_OLD_PRIMARY_HOST="${PRIMARY_HOST}"
  export POSTGRES_FAILOVER_OLD_PRIMARY_PORT="${PRIMARY_PORT}"
  export POSTGRES_FAILOVER_NEW_PRIMARY_HOST="${NEW_PRIMARY_HOST}"
  export POSTGRES_FAILOVER_NEW_PRIMARY_PORT="${NEW_PRIMARY_PORT}"
  export POSTGRES_FAILOVER_FENCE_TOKEN="${FENCE_TOKEN}"
  local output tag endpoint token extra
  journal "route_start" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}"
  if ! output="$(/usr/bin/timeout --foreground --kill-after=5 "${ROUTE_HOOK_TIMEOUT_SEC}s" /usr/bin/env bash "${ROUTE_SCRIPT}")"; then
    journal "route_failed" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}"
    return 1
  fi
  if [[ "${output}" == *$'\n'* ]]; then
    journal "route_invalid_ack" "reason=multiple_lines" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}"
    return 1
  fi
  IFS='|' read -r tag endpoint token extra <<<"${output}"
  if [[ -n "${extra:-}" || "${tag}" != "ROUTED" || "${endpoint}" != "${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" || "${token:-}" != "${FENCE_TOKEN}" ]]; then
    journal "route_invalid_ack" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}"
    return 1
  fi
  journal "route_complete" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" "token=${token}"
  return 0
}

write_intent_marker() {
  atomic_write_file "${INTENT_MARKER}" <<EOF_INTENT
role=promotion-intent
created_at=${NOW}
old_primary=${PRIMARY_HOST}:${PRIMARY_PORT}
new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}
fence_token=${FENCE_TOKEN}
last_primary_lsn=${LAST_PRIMARY_LSN}
EOF_INTENT
}

write_promoted_marker() {
  local route_state="$1" promoted_at="${PROMOTED_AT:-${NOW}}" existing_promoted_at
  if [[ -r "${PROMOTED_MARKER}" ]]; then
    existing_promoted_at="$(marker_value "${PROMOTED_MARKER}" promoted_at)"
    if is_uint "${existing_promoted_at}"; then
      promoted_at="${existing_promoted_at}"
    fi
  fi
  atomic_write_file "${PROMOTED_MARKER}" <<EOF_PROMOTED
role=primary
promoted_at=${promoted_at}
old_primary=${PRIMARY_HOST}:${PRIMARY_PORT}
new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}
fence_token=${FENCE_TOKEN}
last_primary_lsn=${LAST_PRIMARY_LSN}
route_state=${route_state}
EOF_PROMOTED
}

promote_local() {
  local output command_timeout=$((PROMOTE_TIMEOUT_SEC + 10))
  journal "promotion_start" "local=${LOCAL_HOST}:${LOCAL_PORT}" "last_primary_lsn=${LAST_PRIMARY_LSN}"
  output="$(PGPASSFILE="${PGPASS_FILE}" PGSSLMODE=verify-full PGSSLROOTCERT="${CA_FILE}" \
    PGCONNECT_TIMEOUT="${PROBE_TIMEOUT_SEC}" /usr/bin/timeout --foreground --kill-after=5 "${command_timeout}s" \
    "${PSQL_PATH}" -X -v ON_ERROR_STOP=1 -At -h "${LOCAL_HOST}" -p "${LOCAL_PORT}" \
    -U "${PROBE_USER}" -d "${DB_NAME}" -c "SELECT pg_promote(true, ${PROMOTE_TIMEOUT_SEC})")" || return 1
  [[ "${output}" == "t" ]] || return 1
  return 0
}

complete_route_state() {
  # Persist the promoted role before invoking an external route mutation. If the
  # hook or controller crashes, the next evaluation retries only the route and
  # can never issue a second pg_promote().
  write_promoted_marker "pending" || {
    journal "promoted_marker_write_failed" "route_state=pending"
    return 2
  }
  PROMOTION_STATE="promoted_route_failed"
  if [[ -z "${ROUTE_SCRIPT}" ]]; then
    if [[ "${REQUIRE_ROUTE_HOOK}" == "1" ]]; then
      journal "route_required_missing_after_promotion" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}"
      return 1
    fi
    write_promoted_marker "not_configured" || {
      journal "promoted_marker_write_failed" "route_state=not_configured"
      return 1
    }
    PROMOTION_STATE="promoted"
    journal "route_not_configured" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}"
    return 0
  fi
  if run_route_hook; then
    write_promoted_marker "complete" || {
      journal "promoted_marker_write_failed" "route_state=complete"
      return 1
    }
    PROMOTION_STATE="promoted"
    return 0
  fi
  return 1
}

validate_transition_marker() {
  local file="$1" expected_role="$2" timestamp_key="$3" timestamp old_primary new_primary token lsn
  [[ "$(marker_value "${file}" role)" == "${expected_role}" ]] || return 1
  timestamp="$(marker_value "${file}" "${timestamp_key}")"
  old_primary="$(marker_value "${file}" old_primary)"
  new_primary="$(marker_value "${file}" new_primary)"
  token="$(marker_value "${file}" fence_token)"
  lsn="$(marker_value "${file}" last_primary_lsn)"
  is_uint "${timestamp}" || return 1
  (( 10#${timestamp} <= NOW )) || return 1
  [[ "${old_primary}" == "${PRIMARY_HOST}:${PRIMARY_PORT}" ]] || return 1
  [[ "${new_primary}" == "${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" ]] || return 1
  is_token "${token}" || return 1
  is_lsn "${lsn}" || return 1
}

validate_apply_hooks() {
  [[ -n "${FENCE_SCRIPT}" ]] || die "fence_hook_required"
  validate_hook_script "fence" "${FENCE_SCRIPT}"
  if [[ "${REQUIRE_ROUTE_HOOK}" == "1" ]]; then
    [[ -n "${ROUTE_SCRIPT}" ]] || die "route_hook_required"
    validate_hook_script "route" "${ROUTE_SCRIPT}"
  elif [[ -n "${ROUTE_SCRIPT}" ]]; then
    validate_hook_script "route" "${ROUTE_SCRIPT}"
  fi
}

verify_old_primary_not_writable() {
  local phase="$1" raw
  if raw="$(primary_status 2>/dev/null)"; then
    parse_primary_status "${raw}" || die "old_primary_${phase}_invalid_output"
    if [[ "${PRIMARY_RECOVERY}" == "f" && "${PRIMARY_READ_ONLY}" == "off" ]]; then
      journal "promotion_blocked" "reason=old_primary_writable_${phase}" "primary=${PRIMARY_HOST}:${PRIMARY_PORT}"
      return 1
    fi
    [[ "${PRIMARY_READ_ONLY}" == "on" ]] || die "old_primary_${phase}_role_inconsistent" "recovery=${PRIMARY_RECOVERY}" "read_only=${PRIMARY_READ_ONLY}"
    journal "old_primary_${phase}_nonwritable" "recovery=${PRIMARY_RECOVERY}" "read_only=${PRIMARY_READ_ONLY}"
  fi
}

promote_and_reconcile_local() {
  local promote_failed=0
  if ! promote_local; then
    promote_failed=1
    journal "promotion_result_ambiguous" "local=${LOCAL_HOST}:${LOCAL_PORT}"
  fi
  LOCAL_RAW="$(local_status)" || {
    journal "promotion_reconcile_failed" "reason=local_probe_failed" "local=${LOCAL_HOST}:${LOCAL_PORT}"
    return 1
  }
  parse_local_status "${LOCAL_RAW}" || die "post_promotion_probe_invalid_output"
  if [[ "${LOCAL_RECOVERY}" == "f" && "${LOCAL_READ_ONLY}" == "off" ]]; then
    if (( promote_failed == 1 )); then
      journal "promotion_reconciled" "result=primary_after_ambiguous_command"
    fi
    return 0
  fi
  if (( promote_failed == 1 )) && [[ "${LOCAL_RECOVERY}" == "t" && "${LOCAL_READ_ONLY}" == "on" ]]; then
    journal "promotion_not_complete" "result=standby" "intent=${INTENT_MARKER}"
    return 1
  fi
  die "post_promotion_role_invalid" "recovery=${LOCAL_RECOVERY}" "read_only=${LOCAL_READ_ONLY}"
}

finalize_promoted_local() {
  local route_result
  write_promotion_signal || die "promotion_signal_write_failed" "path=${HEALTH_SIGNAL}"
  journal "promotion_signal_written" "path=${HEALTH_SIGNAL}"

  if complete_route_state; then
    remove_durable_file "${INTENT_MARKER}" || die "promotion_intent_remove_failed"
    write_state
    journal "promotion_complete" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" "fence_token=${FENCE_TOKEN}"
    return 0
  else
    route_result=$?
  fi

  if [[ "${route_result}" == "1" && -r "${PROMOTED_MARKER}" ]]; then
    remove_durable_file "${INTENT_MARKER}" || die "promotion_intent_remove_failed"
  fi
  write_state
  if [[ "${route_result}" == "1" ]]; then
    journal "promotion_complete_route_pending" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" "fence_token=${FENCE_TOKEN}"
  else
    journal "promotion_finalize_persistence_failed" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" "fence_token=${FENCE_TOKEN}"
  fi
  return "${route_result}"
}

recover_promotion_intent_on_standby() {
  local expected_fence_token
  validate_transition_marker "${INTENT_MARKER}" "promotion-intent" "created_at" || die "invalid_promotion_intent"
  expected_fence_token="$(marker_value "${INTENT_MARKER}" fence_token)"
  FENCE_TOKEN="${expected_fence_token}"
  LAST_PRIMARY_LSN="$(marker_value "${INTENT_MARKER}" last_primary_lsn)"
  journal "promotion_recovery_detected" "reason=standby_with_intent" "intent=${INTENT_MARKER}"

  if [[ "${MODE}" == "--dry-run" ]]; then
    freshness_guard || return 1
    journal "promotion_recovery_would_run" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}"
    return 0
  fi
  validate_apply_hooks
  freshness_guard || return 1
  verify_old_primary_not_writable "intent_pre_fence" || return 1
  run_fence_hook || return 1
  if [[ "${FENCE_TOKEN}" != "${expected_fence_token}" ]]; then
    journal "promotion_blocked" "reason=fence_token_changed_during_recovery"
    return 1
  fi
  verify_old_primary_not_writable "intent_post_fence" || return 1

  LOCAL_RAW="$(local_status)" || die "local_reprobe_failed"
  parse_local_status "${LOCAL_RAW}" || die "local_reprobe_invalid_output"
  refresh_now || die "current_time_refresh_failed"
  freshness_guard || return 1
  promote_and_reconcile_local || return 1
  finalize_promoted_local
}

ensure_promotion_signal() {
  promotion_signal_exists && return 0
  if [[ "${MODE}" == "--dry-run" ]]; then
    journal "promotion_signal_would_be_restored" "path=${HEALTH_SIGNAL}"
    return 0
  fi
  write_promotion_signal || die "promotion_signal_restore_failed" "path=${HEALTH_SIGNAL}"
  journal "promotion_signal_restored" "path=${HEALTH_SIGNAL}"
}

recover_or_handle_promoted_primary() {
  if [[ -r "${PROMOTED_MARKER}" ]]; then
    validate_transition_marker "${PROMOTED_MARKER}" "primary" "promoted_at" || die "invalid_promoted_marker"
    FENCE_TOKEN="$(marker_value "${PROMOTED_MARKER}" fence_token)"
    LAST_PRIMARY_LSN="$(marker_value "${PROMOTED_MARKER}" last_primary_lsn)"
    ensure_promotion_signal
    if [[ -e "${INTENT_MARKER}" ]]; then
      validate_transition_marker "${INTENT_MARKER}" "promotion-intent" "created_at" || die "invalid_stale_promotion_intent"
      [[ "$(marker_value "${INTENT_MARKER}" fence_token)" == "${FENCE_TOKEN}" ]] || die "promotion_marker_token_mismatch"
      [[ "$(marker_value "${INTENT_MARKER}" last_primary_lsn)" == "${LAST_PRIMARY_LSN}" ]] || die "promotion_marker_lsn_mismatch"
      if [[ "${MODE}" == "--dry-run" ]]; then
        journal "stale_promotion_intent_would_be_removed"
      else
        remove_durable_file "${INTENT_MARKER}" || die "promotion_intent_remove_failed"
        journal "stale_promotion_intent_removed"
      fi
    fi
    local route_state
    route_state="$(marker_value "${PROMOTED_MARKER}" route_state)"
    if [[ "${route_state}" == "pending" ]]; then
      if [[ -z "${ROUTE_SCRIPT}" ]]; then
        if [[ "${REQUIRE_ROUTE_HOOK}" == "0" ]]; then
          if [[ "${MODE}" == "--dry-run" ]]; then
            journal "route_optional_finalize_would_run" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}"
            return 0
          fi
          write_promoted_marker "not_configured" || die "promoted_marker_write_failed" "route_state=not_configured"
          PROMOTION_STATE="promoted"
          write_state
          journal "route_optional_finalize_complete" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}"
          return 0
        fi
        PROMOTION_STATE="promoted_route_failed"
        write_state
        journal "route_retry_blocked" "reason=route_hook_missing"
        return 1
      fi
      if [[ "${MODE}" == "--dry-run" ]]; then
        journal "route_retry_would_run" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}"
        return 0
      fi
      if run_route_hook; then
        write_promoted_marker "complete" || die "promoted_marker_write_failed" "route_state=complete"
        PROMOTION_STATE="promoted"
        write_state
        journal "route_retry_complete" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}"
        return 0
      fi
      PROMOTION_STATE="promoted_route_failed"
      write_state
      return 1
    fi
    case "${route_state}" in
      complete) PROMOTION_STATE="promoted" ;;
      not_configured)
        [[ "${REQUIRE_ROUTE_HOOK}" == "0" ]] || die "required_route_marked_not_configured"
        PROMOTION_STATE="promoted"
        ;;
      *) die "invalid_promoted_route_state" "route_state=${route_state:-empty}" ;;
    esac
    write_state "${PROMOTION_STATE}"
    journal "already_promoted" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" "route_state=${route_state}"
    return 0
  fi

  if [[ -r "${INTENT_MARKER}" ]]; then
    validate_transition_marker "${INTENT_MARKER}" "promotion-intent" "created_at" || die "invalid_promotion_intent"
    FENCE_TOKEN="$(marker_value "${INTENT_MARKER}" fence_token)"
    LAST_PRIMARY_LSN="$(marker_value "${INTENT_MARKER}" last_primary_lsn)"
    PROMOTED_AT="$(marker_value "${INTENT_MARKER}" created_at)"
    ensure_promotion_signal
    journal "promotion_recovery_detected" "reason=primary_without_final_marker"
    if [[ "${MODE}" == "--dry-run" ]]; then
      journal "promotion_finalize_would_run"
      return 0
    fi
    if complete_route_state; then
      remove_durable_file "${INTENT_MARKER}" || die "promotion_intent_remove_failed"
      write_state
      journal "promotion_recovered_after_crash"
      return 0
    fi
    local route_rc=$?
    if [[ "${route_rc}" == "1" && -r "${PROMOTED_MARKER}" ]]; then
      remove_durable_file "${INTENT_MARKER}" || die "promotion_intent_remove_failed"
    fi
    write_state
    return "${route_rc}"
  fi

  die "unexpected_local_primary_without_marker" "local=${LOCAL_HOST}:${LOCAL_PORT}"
}
