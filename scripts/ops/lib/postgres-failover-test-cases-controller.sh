#!/usr/bin/env bash
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  printf '%s\n' "source-only helper: ${BASH_SOURCE[0]}" >&2
  exit 1
fi

LAUNCHER="${ROOT_DIR}/scripts/ops/postgres-failover-launch.sh"

static_deployment_contracts_are_wired() {
  local standby_compose="${ROOT_DIR}/deploy/compose/docker-compose.standby.yml"
  local compose_unit="${ROOT_DIR}/scripts/systemd/hololive-compose.service"
  local pattern
  for pattern in 'HOLOLIVE_STANDBY_POSTGRES_BIND_IP' 'health.signal' 'pg_is_in_recovery()'; do
    grep -Fq "${pattern}" "${standby_compose}" || { fail "standby compose missing contract: ${pattern}"; return; }
  done
  grep -Fq 'User=hololive-pg-failover' "${ROOT_DIR}/scripts/ops/postgres-failover.service" || { fail "failover controller is not assigned to its dedicated user"; return; }
  grep -Fq 'POSTGRES_FAILOVER_PSQL_PATH:-/usr/lib/postgresql/18/bin/psql' "${ROOT_DIR}/scripts/ops/postgres-failover.sh" || { fail "failover controller does not pin the canonical PostgreSQL 18 client"; return; }
  if grep -Fq 'docker exec' "${ROOT_DIR}/scripts/ops/postgres-failover.sh" "${ROOT_DIR}/scripts/ops/lib/postgres-failover-lib.sh" "${ROOT_DIR}/scripts/ops/lib/postgres-failover-transition-lib.sh"; then
    fail "failover controller still depends on root-equivalent Docker access"
    return
  fi
  for pattern in 'ConditionPathExists=!/var/lib/hololive-postgres-fence/fence.intent' 'ConditionPathExists=!/var/lib/hololive-postgres-fence/fenced'; do
    grep -Fq "${pattern}" "${compose_unit}" || { fail "compose unit missing fence condition: ${pattern}"; return; }
  done
  for pattern in 'postgres-failover-launch.sh' 'UnsetEnvironment=BASH_ENV ENV LD_PRELOAD LD_LIBRARY_PATH' \
    'RuntimeDirectory=hololive-postgres-failover' 'RuntimeDirectoryMode=0700' 'RuntimeDirectoryPreserve=no'; do
    grep -Fq "${pattern}" "${ROOT_DIR}/scripts/ops/postgres-failover.service" || { fail "failover unit missing trusted launcher contract: ${pattern}"; return; }
  done
  if grep -Fq 'EnvironmentFile=' "${ROOT_DIR}/scripts/ops/postgres-failover.service"; then
    fail "failover unit still lets systemd inject the configuration file"
    return
  fi
  grep -Fq '${NEW_PRIMARY_PORT} ${TAILSCALE_SERVICE}' "${ROOT_DIR}/scripts/ops/postgres-failover-fence-ssh.sh" || {
    fail "SSH fencing does not bind the durable fence to the drained Tailscale Service"
    return
  }
  for pattern in \
    'PermitUserEnvironment no' \
    'AuthorizedKeysFile /etc/ssh/authorized_keys/hololive-pg-fence' \
    'ForceCommand /usr/local/libexec/hololive-postgres-failover/postgres-failover-ssh-dispatch.sh fence' \
    'AuthorizedKeysFile /etc/ssh/authorized_keys/hololive-pg-route' \
    'ForceCommand /usr/local/libexec/hololive-postgres-failover/postgres-failover-ssh-dispatch.sh route' \
    'DisableForwarding yes'; do
    grep -Fq "${pattern}" "${ROOT_DIR}/scripts/ops/hololive-postgres-failover.sshd.conf" || {
      fail "SSH server command confinement is missing: ${pattern}"
      return
    }
  done
  grep -Fq '/var/empty /bin/dash' "${ROOT_DIR}/scripts/systemd/hololive-postgres-fence.sysusers.conf" || {
    fail "fence SSH account does not use the non-Bash restricted runtime profile"
    return
  }
  grep -Fq '/var/empty /bin/dash' "${ROOT_DIR}/scripts/systemd/hololive-postgres-route.sysusers.conf" || {
    fail "route SSH account does not use the non-Bash restricted runtime profile"
    return
  }
  for pattern in 'POSTGRES_FAILOVER_ROUTE_COMMAND=/usr/local/libexec/hololive-postgres-failover/postgres-failover-route-ssh.sh' \
    'POSTGRES_FAILOVER_SSH_TARGET=hololive-pg-fence@100.100.1.8' \
    'POSTGRES_FAILOVER_ROUTE_SSH_TARGET=hololive-pg-route@100.100.1.5' \
    'POSTGRES_FAILOVER_TAILSCALE_SERVICE=svc:hololive-postgres'; do
    grep -Fq "${pattern}" "${ROOT_DIR}/scripts/ops/postgres-failover.env.example" || {
      fail "failover environment example is missing: ${pattern}"
      return
    }
  done
  for pattern in \
    'LoadCredential=route-ssh-key:/etc/stack-secrets/hololive-bot/postgres-failover/route_id_ed25519' \
    'LoadCredential=route-known-hosts:/etc/stack-secrets/hololive-bot/postgres-failover/route_known_hosts' \
    'Environment=POSTGRES_FAILOVER_ROUTE_SSH_IDENTITY_FILE=%d/route-ssh-key' \
    'Environment=POSTGRES_FAILOVER_ROUTE_SSH_KNOWN_HOSTS_FILE=%d/route-known-hosts'; do
    grep -Fq "${pattern}" "${ROOT_DIR}/scripts/ops/postgres-failover-apply.conf.example" || {
      fail "apply drop-in is missing: ${pattern}"
      return
    }
  done
  pass "standby health, bind, and old-primary boot fencing are wired"
}
launcher_rejects_environment_injection() {
  local root payload credential_dir
  root="${TMP_DIR}/launcher"
  if [[ "${CONTROLLER_TEST_MODE}" == "0" ]]; then
    SYSTEM_CREDENTIAL_TEST_ROOT="/run/credentials/postgres-failover-test.$$"
    credential_dir="${SYSTEM_CREDENTIAL_TEST_ROOT}"
    test ! -e "${SYSTEM_CREDENTIAL_TEST_ROOT}" || { fail "system credential fixture already exists"; return; }
    mkdir -p "${credential_dir}"
    chmod 0700 "${SYSTEM_CREDENTIAL_TEST_ROOT}" "${credential_dir}"
  else
    credential_dir="${root}/run/credentials/postgres-failover.service"
    mkdir -p "${credential_dir}"
    chmod 0700 "${root}/run" "${root}/run/credentials" "${credential_dir}"
  fi
  mkdir -p "${root}/private"
  chmod 0700 "${root}/private"
  cat >"${root}/private/controller.sh" <<'CONTROLLER'
#!/usr/bin/env bash
printf '%s|%s|%s\n' "${PATH}" "${POSTGRES_FAILOVER_PRIMARY_HOST:-}" "$1" >"${FAKE_LAUNCH_LOG:?}"
CONTROLLER
  chmod 0700 "${root}/private/controller.sh"
  cat >"${credential_dir}/failover.env" <<'ENV_OK'
POSTGRES_FAILOVER_PRIMARY_HOST=100.100.1.8
POSTGRES_FAILOVER_PRIMARY_PORT=5433
ENV_OK
  chmod 0440 "${credential_dir}/failover.env"
  env -u BASH_ENV -u ENV -u LD_PRELOAD -u LD_LIBRARY_PATH \
    PATH=/tmp/attacker \
    FAKE_LAUNCH_LOG="${root}/launch.log" \
    POSTGRES_FAILOVER_ENV_FILE="${credential_dir}/failover.env" \
    POSTGRES_FAILOVER_CONTROLLER="${root}/private/controller.sh" \
    POSTGRES_FAILOVER_SERVICE_USER="$(id -un)" \
    POSTGRES_FAILOVER_LAUNCH_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" \
    /usr/bin/bash "${LAUNCHER}" --dry-run || { fail "trusted launcher rejected valid allowlisted input"; return; }
  grep -Fxq '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin|100.100.1.8|--dry-run' "${root}/launch.log" || { cat "${root}/launch.log" >&2; fail "launcher did not sanitize environment"; return; }

  cp "${credential_dir}/failover.env" "${root}/private/group-readable.env"
  chmod 0440 "${root}/private/group-readable.env"
  if env -u BASH_ENV -u ENV -u LD_PRELOAD -u LD_LIBRARY_PATH \
    FAKE_LAUNCH_LOG="${root}/launch.log" \
    POSTGRES_FAILOVER_ENV_FILE="${root}/private/group-readable.env" \
    POSTGRES_FAILOVER_CONTROLLER="${root}/private/controller.sh" \
    POSTGRES_FAILOVER_SERVICE_USER="$(id -un)" \
    POSTGRES_FAILOVER_LAUNCH_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" \
    /usr/bin/bash "${LAUNCHER}" --dry-run >/dev/null 2>&1; then
    fail "launcher accepted group-readable input outside the credential directory"
    return
  fi
  chmod 0640 "${credential_dir}/failover.env"
  if env -u BASH_ENV -u ENV -u LD_PRELOAD -u LD_LIBRARY_PATH \
    FAKE_LAUNCH_LOG="${root}/launch.log" \
    POSTGRES_FAILOVER_ENV_FILE="${credential_dir}/failover.env" \
    POSTGRES_FAILOVER_CONTROLLER="${root}/private/controller.sh" \
    POSTGRES_FAILOVER_SERVICE_USER="$(id -un)" \
    POSTGRES_FAILOVER_LAUNCH_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" \
    /usr/bin/bash "${LAUNCHER}" --dry-run >/dev/null 2>&1; then
    fail "launcher accepted a writable group-readable credential"
    return
  fi

  if [[ "${CONTROLLER_TEST_MODE}" == "0" ]]; then
    chmod 0440 "${credential_dir}/failover.env"
    chown 0:1 "${credential_dir}/failover.env"
    if env -u BASH_ENV -u ENV -u LD_PRELOAD -u LD_LIBRARY_PATH \
      FAKE_LAUNCH_LOG="${root}/launch.log" \
      POSTGRES_FAILOVER_ENV_FILE="${credential_dir}/failover.env" \
      POSTGRES_FAILOVER_CONTROLLER="${root}/private/controller.sh" \
      POSTGRES_FAILOVER_SERVICE_USER="$(id -un)" \
      POSTGRES_FAILOVER_LAUNCH_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" \
      /usr/bin/bash "${LAUNCHER}" --dry-run >/dev/null 2>&1; then
      fail "launcher accepted a credential outside root group ownership"
      return
    fi
    chown 0:0 "${credential_dir}/failover.env"
  fi

  mkdir -p "${credential_dir}/nested"
  cp "${credential_dir}/failover.env" "${credential_dir}/nested/failover.env"
  chmod 0440 "${credential_dir}/nested/failover.env"
  if env -u BASH_ENV -u ENV -u LD_PRELOAD -u LD_LIBRARY_PATH \
    FAKE_LAUNCH_LOG="${root}/launch.log" \
    POSTGRES_FAILOVER_ENV_FILE="${credential_dir}/nested/failover.env" \
    POSTGRES_FAILOVER_CONTROLLER="${root}/private/controller.sh" \
    POSTGRES_FAILOVER_SERVICE_USER="$(id -un)" \
    POSTGRES_FAILOVER_LAUNCH_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" \
    /usr/bin/bash "${LAUNCHER}" --dry-run >/dev/null 2>&1; then
    fail "launcher accepted a nested credential path"
    return
  fi

  payload="${root}/payload-ran"
  chmod 0600 "${credential_dir}/failover.env"
  cat >"${credential_dir}/failover.env" <<ENV_BAD
BASH_ENV=${root}/payload.sh
POSTGRES_FAILOVER_PRIMARY_HOST=100.100.1.8
ENV_BAD
  if env -u BASH_ENV -u ENV -u LD_PRELOAD -u LD_LIBRARY_PATH \
    FAKE_LAUNCH_LOG="${root}/launch.log" \
    POSTGRES_FAILOVER_ENV_FILE="${credential_dir}/failover.env" \
    POSTGRES_FAILOVER_CONTROLLER="${root}/private/controller.sh" \
    POSTGRES_FAILOVER_SERVICE_USER="$(id -un)" \
    POSTGRES_FAILOVER_LAUNCH_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" \
    /usr/bin/bash "${LAUNCHER}" --dry-run >/dev/null 2>&1; then
    fail "launcher accepted BASH_ENV injection"
    return
  fi
  [[ ! -e "${payload}" ]] || { fail "environment injection payload executed"; return; }
  pass "failover configuration is parsed through a strict allowlist"
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
  if grep -Fq 'pg_promote' "${root}/psql.log" || [[ -e "${root}/promoted" ]]; then
    cat "${root}/psql.log" >&2
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
  if FAKE_FENCE_OUTPUT='NOPE|100.100.1.8|100.100.1.5:5434|request-token-1234|bad-token-1234' run_controller "${root}" --apply down; then
    fail "invalid fence acknowledgement unexpectedly succeeded"
    return
  fi
  if grep -Fq 'pg_promote' "${root}/psql.log"; then
    cat "${root}/psql.log" >&2
    fail "promotion ran after invalid fence acknowledgement"
    return
  fi
  grep -Fq 'event=fence_invalid_ack' "${root}/err.log" || { cat "${root}/err.log" >&2; fail "missing fence_invalid_ack event"; return; }
  pass "invalid fence acknowledgement blocks promotion"
}
stale_fence_ack_blocks_promotion() {
  local root; root="$(setup_case stale-fence-ack)"
  seed_ready_state "${root}"
  if FAKE_FENCE_OUTPUT='FENCED|100.100.1.8|100.100.1.5:5434|stale-request-1234|durable-fence-1234' run_controller "${root}" --apply down; then
    fail "stale fence acknowledgement unexpectedly succeeded"
    return
  fi
  grep -Fq 'event=fence_invalid_ack' "${root}/err.log" || { cat "${root}/err.log" >&2; fail "missing stale ACK guard"; return; }
  if grep -Fq 'pg_promote' "${root}/psql.log"; then
    fail "promotion ran after stale fence acknowledgement"
    return
  fi
  pass "fence acknowledgement is bound to the current invocation"
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
  if grep -Fq 'pg_promote' "${root}/psql.log"; then
    cat "${root}/psql.log" >&2
    fail "promotion ran while old primary still accepted writes"
    return
  fi
  grep -Fq 'reason=old_primary_writable_post_fence' "${root}/err.log" || { cat "${root}/err.log" >&2; fail "missing post-fence writable-primary guard"; return; }
  pass "post-fence read/write reprobe prevents split brain"
}
promotion_failure_on_standby_resumes_from_intent() {
  local root fence_token
  root="$(setup_case promotion-retry)"
  seed_ready_state "${root}"
  if run_controller "${root}" --apply down FAKE_PROMOTE_RESULT=fail_standby; then
    fail "standby promotion failure unexpectedly succeeded"
    return
  fi
  [[ -r "${root}/state/promotion.intent" ]] || { fail "promotion failure did not retain intent"; return; }
  fence_token="$(awk -F= '$1 == "fence_token" {print $2}' "${root}/state/promotion.intent")"
  [[ -n "${fence_token}" ]] || { fail "promotion intent did not retain fence token"; return; }
  if ! run_controller "${root}" --apply down "FAKE_FENCE_TOKEN=${fence_token}"; then
    cat "${root}/err.log" >&2
    fail "promotion intent recovery failed"
    return
  fi
  [[ ! -e "${root}/state/promotion.intent" ]] || { fail "recovered promotion left intent marker"; return; }
  grep -Fq 'route_state=complete' "${root}/state/promoted" || { cat "${root}/state/promoted" >&2; fail "recovered promotion did not route"; return; }
  [[ "$(grep -Fc 'pg_promote' "${root}/psql.log")" == "2" ]] || { cat "${root}/psql.log" >&2; fail "promotion recovery did not retry exactly once"; return; }
  pass "promotion failure while still standby resumes from durable intent"
}
ambiguous_promotion_timeout_reconciles_primary() {
  local root
  root="$(setup_case promotion-ambiguous)"
  seed_ready_state "${root}"
  if ! run_controller "${root}" --apply down FAKE_PROMOTE_RESULT=fail_primary; then
    cat "${root}/err.log" >&2
    fail "ambiguous promotion result was not reconciled"
    return
  fi
  grep -Fq 'event=promotion_reconciled result=primary_after_ambiguous_command' "${root}/err.log" || { cat "${root}/err.log" >&2; fail "missing ambiguous promotion reconciliation"; return; }
  grep -Fq 'route_state=complete' "${root}/state/promoted" || { fail "ambiguous promotion did not finalize route"; return; }
  pass "ambiguous pg_promote result is reconciled from local role"
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
  [[ -e "${root}/state/health.signal" ]] || { fail "promotion health signal was not written"; return; }
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
  [[ "$(grep -Fc 'pg_promote' "${root}/psql.log")" == "1" ]] || { cat "${root}/psql.log" >&2; fail "route retry issued another promotion"; return; }
  pass "route failure retries without a second promotion"
}
stale_observation_blocks_promotion() {
  local root; root="$(setup_case stale)"
  seed_ready_state "${root}" 1 1 1
  if run_controller "${root}" --apply down; then
    fail "stale observation unexpectedly allowed promotion"
    return
  fi
  if grep -Fq 'pg_promote' "${root}/psql.log"; then
    fail "promotion ran with stale observation"
    return
  fi
  grep -Fq 'reason=stale_healthy_observation' "${root}/err.log" || { cat "${root}/err.log" >&2; fail "missing stale observation guard"; return; }
  pass "stale healthy observation blocks promotion"
}
post_fence_clock_advance_blocks_promotion() {
  local root; root="$(setup_case post-fence-stale)"
  seed_ready_state "${root}" 1 100 90
  if run_controller "${root}" --apply down POSTGRES_FAILOVER_POST_FENCE_NOW=211; then
    fail "post-fence stale observation unexpectedly allowed promotion"
    return
  fi
  grep -Fq 'fence' "${root}/hooks.log" || { cat "${root}/hooks.log" >&2; fail "post-fence freshness test did not reach fence"; return; }
  if grep -Fq 'pg_promote' "${root}/psql.log"; then
    cat "${root}/psql.log" >&2
    fail "promotion ran after freshness expired during fencing"
    return
  fi
  grep -Fq 'reason=stale_healthy_observation age_sec=121' "${root}/err.log" || { cat "${root}/err.log" >&2; fail "missing post-fence freshness guard"; return; }
  pass "freshness is re-evaluated with a post-fence clock sample"
}
