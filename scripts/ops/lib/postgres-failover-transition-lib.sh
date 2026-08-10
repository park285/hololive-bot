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
  local request_id output tag host endpoint token extra
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
  IFS='|' read -r tag host endpoint token extra <<<"${output}"
  if [[ -n "${extra:-}" || "${tag}" != "FENCED" || "${host}" != "${PRIMARY_HOST}" \
    || "${endpoint}" != "${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" ]] || ! is_token "${token:-}"; then
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
  journal "promotion_start" "container=${STANDBY_CONTAINER}" "last_primary_lsn=${LAST_PRIMARY_LSN}"
  output="$(/usr/bin/timeout --foreground --kill-after=5 "${command_timeout}s" \
    docker exec "${STANDBY_CONTAINER}" psql -X -v ON_ERROR_STOP=1 -At \
    -U "${LOCAL_DB_USER}" -d "${DB_NAME}" \
    -c "SELECT pg_promote(true, ${PROMOTE_TIMEOUT_SEC})")" || return 1
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
  [[ "${old_primary}" == "${PRIMARY_HOST}:${PRIMARY_PORT}" ]] || return 1
  [[ "${new_primary}" == "${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" ]] || return 1
  is_token "${token}" || return 1
  is_lsn "${lsn}" || return 1
}

ensure_container_promotion_signal() {
  container_promotion_signal_exists && return 0
  if [[ "${MODE}" == "--dry-run" ]]; then
    journal "promotion_signal_would_be_restored" "path=${CONTAINER_PROMOTED_MARKER}"
    return 0
  fi
  write_container_promotion_signal || die "promotion_signal_restore_failed" "path=${CONTAINER_PROMOTED_MARKER}"
  journal "promotion_signal_restored" "path=${CONTAINER_PROMOTED_MARKER}"
}

recover_or_handle_promoted_primary() {
  if [[ -r "${PROMOTED_MARKER}" ]]; then
    validate_transition_marker "${PROMOTED_MARKER}" "primary" "promoted_at" || die "invalid_promoted_marker"
    FENCE_TOKEN="$(marker_value "${PROMOTED_MARKER}" fence_token)"
    LAST_PRIMARY_LSN="$(marker_value "${PROMOTED_MARKER}" last_primary_lsn)"
    ensure_container_promotion_signal
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
    write_state
    journal "already_promoted" "new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" "route_state=${route_state}"
    return 0
  fi

  if [[ -r "${INTENT_MARKER}" ]]; then
    validate_transition_marker "${INTENT_MARKER}" "promotion-intent" "created_at" || die "invalid_promotion_intent"
    FENCE_TOKEN="$(marker_value "${INTENT_MARKER}" fence_token)"
    LAST_PRIMARY_LSN="$(marker_value "${INTENT_MARKER}" last_primary_lsn)"
    PROMOTED_AT="$(marker_value "${INTENT_MARKER}" created_at)"
    ensure_container_promotion_signal
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

  die "unexpected_local_primary_without_marker" "container=${STANDBY_CONTAINER}"
}
