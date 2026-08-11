#!/usr/bin/env bash
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  printf '%s\n' "source-only helper: ${BASH_SOURCE[0]}" >&2
  exit 1
fi

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
