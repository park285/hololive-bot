#!/usr/bin/env bash
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  printf '%s\n' "source-only helper: ${BASH_SOURCE[0]}" >&2
  exit 1
fi

PRIMARY_FENCE="${ROOT_DIR}/scripts/ops/postgres-primary-fence.sh"
PRIMARY_UNFENCE="${ROOT_DIR}/scripts/ops/postgres-primary-unfence.sh"

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
setup_primary_unfence_fake_tools() {
  local root="$1"
  mkdir -p "${root}/bin"
  cat >"${root}/bin/ip" <<'EOF_IP_UNFENCE'
#!/usr/bin/env bash
printf '1: tailscale0    inet 100.100.1.8/32 scope global tailscale0\n'
EOF_IP_UNFENCE
  cat >"${root}/bin/systemctl" <<'EOF_SYSTEMCTL_UNFENCE'
#!/usr/bin/env bash
if [[ "${1:-}" == "is-active" ]]; then exit 3; fi
exit 2
EOF_SYSTEMCTL_UNFENCE
  cat >"${root}/bin/docker" <<'EOF_DOCKER_UNFENCE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${UNFENCE_TOOL_LOG:?}"
if [[ "${1:-}" == "inspect" && "${2:-}" == "-f" ]]; then
  case "${3:-}" in
    *State.Running*) printf 'true\n' ;;
    *RestartPolicy.Name*) printf 'no\n' ;;
  esac
  exit 0
fi
if [[ "${1:-}" == "exec" ]]; then
  if [[ " $* " == *" -h 100.100.1.5 "* ]]; then
    printf '%s\n' "${UNFENCE_REMOTE_STATUS:-f|off}"
  else
    printf '%s\n' "${UNFENCE_LOCAL_STATUS:-t|on|f|streaming|100.100.1.5|5434}"
  fi
  exit 0
fi
exit 2
EOF_DOCKER_UNFENCE
  chmod 0755 "${root}/bin/ip" "${root}/bin/systemctl" "${root}/bin/docker"
}
missing_route_hook_blocks_before_fencing() {
  local root; root="$(setup_case missing-route)"
  seed_ready_state "${root}"
  if run_controller "${root}" --apply down POSTGRES_FAILOVER_ROUTE_COMMAND=; then
    fail "missing required route hook unexpectedly allowed apply"
    return
  fi
  if [[ -s "${root}/hooks.log" ]] || grep -Fq 'pg_promote' "${root}/psql.log"; then
    cat "${root}/hooks.log" "${root}/psql.log" >&2
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
stale_health_signal_blocks_standby_controller() {
  local root; root="$(setup_case stale-signal)"
  printf 'role=primary\n' >"${root}/state/health.signal"
  if run_controller "${root}" --dry-run up; then
    fail "standby with stale promotion signal unexpectedly passed"
    return
  fi
  grep -Fq 'reason=stale_promotion_signal_while_in_recovery' "${root}/err.log" || { cat "${root}/err.log" >&2; fail "missing stale health signal guard"; return; }
  pass "stale host promotion signal fails closed"
}
unexpected_primary_without_marker_stays_unhealthy() {
  local root; root="$(setup_case unowned-primary)"
  : >"${root}/promoted"
  if run_controller "${root}" --apply down; then fail "unowned primary unexpectedly passed"; return; fi
  [[ ! -e "${root}/state/health.signal" ]] || { fail "unowned primary received a health signal"; return; }
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
  [[ -e "${root}/state/health.signal" ]] || { fail "crash recovery did not restore health signal"; return; }
  [[ ! -e "${root}/state/promotion.intent" ]] || { fail "crash recovery left intent marker"; return; }
  grep -Fq 'route_state=complete' "${root}/state/promoted" || { cat "${root}/state/promoted" >&2; fail "crash recovery did not complete route"; return; }
  if grep -Fq 'pg_promote' "${root}/psql.log"; then
    cat "${root}/psql.log" >&2
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
  [[ "${output1}" == 'FENCED|100.100.1.8|100.100.1.5:5434|request-token-1234|request-token-1234' ]] || { printf '%s\n' "${output1}" >&2; fail "primary fence acknowledgement invalid"; return; }
  [[ "${output2}" == 'FENCED|100.100.1.8|100.100.1.5:5434|another-token-5678|request-token-1234' ]] || { printf '%s\n%s\n' "${output1}" "${output2}" >&2; fail "primary fence did not separate request freshness from durable token"; return; }
  grep -Fq 'stop hololive-compose.service' "${root}/tools.log" || { cat "${root}/tools.log" >&2; fail "compose unit was not stopped"; return; }
  grep -Fq 'update --restart=no deunhealth' "${root}/tools.log" || { cat "${root}/tools.log" >&2; fail "autoheal was not fenced"; return; }
  grep -Fq 'update --restart=no holo-postgres' "${root}/tools.log" || { cat "${root}/tools.log" >&2; fail "database restart policy was not fenced"; return; }
  [[ "$(grep -Fc 'update --restart=no holo-postgres' "${root}/tools.log")" == "4" ]] || { cat "${root}/tools.log" >&2; fail "idempotent fence did not re-verify/reapply database stop"; return; }
  grep -Fq 'state=fenced' "${root}/state/fenced" || { cat "${root}/state/fenced" >&2; fail "persistent fence marker missing"; return; }
  grep -Fq 'new_primary=100.100.1.5:5434' "${root}/state/fenced" || { cat "${root}/state/fenced" >&2; fail "fence candidate binding missing"; return; }
  if PATH="${root}/bin:${PATH}" FENCE_TOOL_LOG="${root}/tools.log" POSTGRES_PRIMARY_FENCE_STATE_DIR="${root}/state" POSTGRES_PRIMARY_FENCE_ALLOW_TEST_STATE_DIR=1 POSTGRES_PRIMARY_FENCE_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" /usr/bin/env bash "${PRIMARY_FENCE}" third-token-9012 100.100.1.8 100.100.1.6 5434 >/dev/null 2>&1; then fail "existing fence was reusable by another candidate"; return; fi
  pass "primary fence is persistent, idempotent, and candidate-bound"
}
primary_unfence_requires_reseeded_streaming_standby() {
  local root output
  root="${TMP_DIR}/primary-unfence"
  mkdir -p "${root}/state"
  chmod 0700 "${root}/state"
  setup_primary_unfence_fake_tools "${root}"
  : >"${root}/tools.log"
  cat >"${root}/state/fenced" <<'EOF_FENCED'
state=fenced
request_id=first-request-1234
fence_token=durable-fence-1234
primary_host=100.100.1.8
new_primary=100.100.1.5:5434
fenced_at=200
EOF_FENCED
  chmod 0600 "${root}/state/fenced"
  output="$(PATH="${root}/bin:${PATH}" UNFENCE_TOOL_LOG="${root}/tools.log" \
    POSTGRES_PRIMARY_UNFENCE_STATE_DIR="${root}/state" \
    POSTGRES_PRIMARY_UNFENCE_ALLOW_TEST_STATE_DIR=1 \
    POSTGRES_PRIMARY_UNFENCE_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" \
    /usr/bin/bash "${PRIMARY_UNFENCE}" durable-fence-1234 100.100.1.8 100.100.1.5 5434)" || {
      fail "safe primary unfence rejected a verified standby"
      return
    }
  [[ "${output}" == 'UNFENCED|100.100.1.8|100.100.1.5:5434|durable-fence-1234' ]] || { printf '%s\n' "${output}" >&2; fail "unfence acknowledgement invalid"; return; }
  [[ ! -e "${root}/state/fenced" ]] || { fail "verified unfence left the fence marker"; return; }

  cat >"${root}/state/fenced" <<'EOF_FENCED_AGAIN'
state=fenced
request_id=first-request-1234
fence_token=durable-fence-1234
primary_host=100.100.1.8
new_primary=100.100.1.5:5434
fenced_at=200
EOF_FENCED_AGAIN
  chmod 0600 "${root}/state/fenced"
  if PATH="${root}/bin:${PATH}" UNFENCE_TOOL_LOG="${root}/tools.log" \
    UNFENCE_LOCAL_STATUS='f|off|f|||' \
    POSTGRES_PRIMARY_UNFENCE_STATE_DIR="${root}/state" \
    POSTGRES_PRIMARY_UNFENCE_ALLOW_TEST_STATE_DIR=1 \
    POSTGRES_PRIMARY_UNFENCE_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" \
    /usr/bin/bash "${PRIMARY_UNFENCE}" durable-fence-1234 100.100.1.8 100.100.1.5 5434 >/dev/null 2>&1; then
    fail "unfence accepted a local writer"
    return
  fi
  [[ -e "${root}/state/fenced" ]] || { fail "failed unfence removed the durable fence"; return; }
  pass "unfence removes boot fencing only after streaming-standby verification"
}
fence_and_unfence_share_transition_lock() {
  grep -Fq 'LOCK_FILE="${STATE_DIR}/transition.lock"' "${PRIMARY_FENCE}" || { fail "fence does not use the shared transition lock"; return; }
  grep -Fq 'LOCK_FILE="${STATE_DIR}/transition.lock"' "${PRIMARY_UNFENCE}" || { fail "unfence does not use the shared transition lock"; return; }
  pass "fence and unfence serialize through one transition lock"
}
