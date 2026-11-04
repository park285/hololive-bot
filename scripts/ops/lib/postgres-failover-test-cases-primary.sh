#!/usr/bin/env bash
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  printf '%s\n' "source-only helper: ${BASH_SOURCE[0]}" >&2
  exit 1
fi

PRIMARY_FENCE="${ROOT_DIR}/scripts/ops/postgres-primary-fence.sh"
PRIMARY_UNFENCE="${ROOT_DIR}/scripts/ops/postgres-primary-unfence.sh"

setup_primary_fence_fake_tools() {
  local root="$1"
  mkdir -p "${root}/bin" "${root}/fake-unit"
  printf 'active\n' >"${root}/fake-unit/state"
  printf '#!/usr/bin/env bash\nprintf '\''1: tailscale0    inet 100.100.1.8/32 scope global tailscale0\\n'\''' >"${root}/bin/ip"
  cat >"${root}/bin/fence-crash" <<'EOF_FENCE_CRASH'
#!/usr/bin/env bash
candidate="${PPID}"
while [[ "${candidate}" =~ ^[0-9]+$ && "${candidate}" -gt 1 ]]; do
  matched=0
  while IFS= read -r -d '' arg; do
    [[ "${arg}" != "${FAKE_FENCE_PROCESS_MATCH:?}" ]] || matched=1
  done <"/proc/${candidate}/cmdline"
  if (( matched == 1 )); then kill -KILL "${candidate}"; exit 137; fi
  candidate="$(awk '/^PPid:/ {print $2}' "/proc/${candidate}/status")"
done
exit 97
EOF_FENCE_CRASH
  cat >"${root}/bin/systemctl" <<'EOF_SYSTEMCTL'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${FENCE_TOOL_LOG:?}"
root="$(dirname -- "${FENCE_TOOL_LOG}")"
case "${1:-}" in
  show)
    case " $* " in
      *' -p NeedDaemonReload '*) printf '%s\n' "${FAKE_NEED_DAEMON_RELOAD:-no}" ;;
      *' -p ActiveState '*) printf '%s\n' "$(<"${root}/fake-unit/state")" ;;
      *) exit 2 ;;
    esac ;;
  cat)
    printf '%s\n' 'ConditionPathExists=!/var/lib/hololive-postgres-fence/fence.intent' 'ConditionPathExists=!/var/lib/hololive-postgres-fence/fenced' ;;
  stop)
    [[ "${FAKE_REQUIRE_FENCE_INTENT:-0}" != 1 || -r "${root}/state/fence.intent" ]] || exit 98
    printf '%s\n' "${FAKE_STOP_RESULT_STATE:-inactive}" >"${root}/fake-unit/state"
    [[ "${FAKE_FENCE_CRASH_AFTER:-}" != stop ]] || "${root}/bin/fence-crash" ;;
  *) exit 2 ;;
esac
EOF_SYSTEMCTL
  cat >"${root}/bin/tailscale" <<'EOF_TAILSCALE_FENCE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${FENCE_TOOL_LOG:?}"
if [[ "${2:-}" == advertise ]]; then
  [[ "${FAKE_TAILSCALE_ADVERTISE_RESULT:-success}" == success ]]
else
  [[ "${2:-}" != drain || "${FAKE_REQUIRE_FENCE_INTENT:-0}" != 1 || -r "$(dirname -- "${FENCE_TOOL_LOG}")/state/fence.intent" ]] || exit 98
  [[ "${2:-}" != drain || "${FAKE_FENCE_CRASH_AFTER:-}" != drain ]] || exec "$(dirname -- "${FENCE_TOOL_LOG}")/bin/fence-crash"
  [[ "${FAKE_TAILSCALE_RESULT:-success}" == success ]]
fi
EOF_TAILSCALE_FENCE
  # Quadlet container는 --rm이라 unit이 멈추면 사라진다. 수동 container(FAKE_MANUAL_POSTGRES)는 PGDATA volume으로 잡힌다.
  cat >"${root}/bin/podman" <<'EOF_PODMAN_FENCE'
#!/usr/bin/env bash
printf 'podman %s\n' "$*" >>"${FENCE_TOOL_LOG:?}"
root="$(dirname -- "${FENCE_TOOL_LOG}")"
[[ "${FAKE_PODMAN_RESULT:-success}" == success ]] || exit 125
case " $* " in
  *' name='*) [[ "$(<"${root}/fake-unit/state")" != active ]] || printf 'holo-postgres running\n' ;;
  *' volume='*) [[ -z "${FAKE_MANUAL_POSTGRES:-}" ]] || printf 'holo-postgres-manual running\n' ;;
esac
EOF_PODMAN_FENCE
  chmod 0755 "${root}/bin/ip" "${root}/bin/fence-crash" "${root}/bin/systemctl" "${root}/bin/tailscale" "${root}/bin/podman"
}
run_primary_fence() {
  local root="$1" request_id="$2" expected_host="$3" new_host="$4" new_port="$5"
  shift 5
  env PATH="${root}/bin:${PATH}" FENCE_TOOL_LOG="${root}/tools.log" \
    POSTGRES_PRIMARY_FENCE_TAILSCALE_PATH="${root}/bin/tailscale" \
    POSTGRES_PRIMARY_FENCE_PODMAN_PATH="${root}/bin/podman" \
    POSTGRES_PRIMARY_FENCE_STATE_DIR="${root}/state" \
    POSTGRES_PRIMARY_FENCE_ALLOW_TEST_STATE_DIR=1 \
    POSTGRES_PRIMARY_FENCE_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" "$@" \
    /usr/bin/env bash "${PRIMARY_FENCE}" "${request_id}" "${expected_host}" "${new_host}" "${new_port}" svc:hololive-postgres
}
primary_fence_is_persistent_and_idempotent() {
  local root output1 output2
  root="${TMP_DIR}/primary-fence"
  mkdir -p "${root}/state"
  setup_primary_fence_fake_tools "${root}"
  : >"${root}/tools.log"
  output1="$(run_primary_fence "${root}" request-token-1234 100.100.1.8 100.100.1.5 5434 FAKE_REQUIRE_FENCE_INTENT=1 POSTGRES_PRIMARY_FENCE_NOW=200)" || { fail "primary fence first run failed"; return; }
  output2="$(run_primary_fence "${root}" another-token-5678 100.100.1.8 100.100.1.5 5434 POSTGRES_PRIMARY_FENCE_NOW=201)" || { fail "primary fence idempotent run failed"; return; }
  [[ "${output1}" == 'FENCED|100.100.1.8|100.100.1.5:5434|request-token-1234|request-token-1234' ]] || { printf '%s\n' "${output1}" >&2; fail "primary fence acknowledgement invalid"; return; }
  [[ "${output2}" == 'FENCED|100.100.1.8|100.100.1.5:5434|another-token-5678|request-token-1234' ]] || { printf '%s\n%s\n' "${output1}" "${output2}" >&2; fail "primary fence did not separate request freshness from durable token"; return; }
  if grep -E '^stop ' "${root}/tools.log" | grep -Fvxq 'stop holo-postgres.service'; then
    cat "${root}/tools.log" >&2
    fail "database fencing stopped stable-endpoint consumers"
    return
  fi
  [[ "$(grep -Fxc 'stop holo-postgres.service' "${root}/tools.log")" == "2" ]] || { cat "${root}/tools.log" >&2; fail "idempotent fence did not re-verify/reapply database stop"; return; }
  [[ "$(grep -Fc 'serve drain svc:hololive-postgres' "${root}/tools.log")" == "2" ]] || { cat "${root}/tools.log" >&2; fail "Tailscale Service was not drained for each fence invocation"; return; }
  grep -Fq 'state=fenced' "${root}/state/fenced" || { cat "${root}/state/fenced" >&2; fail "persistent fence marker missing"; return; }
  grep -Fq 'new_primary=100.100.1.5:5434' "${root}/state/fenced" || { cat "${root}/state/fenced" >&2; fail "fence candidate binding missing"; return; }
  if run_primary_fence "${root}" third-token-9012 100.100.1.8 100.100.1.6 5434 >/dev/null 2>&1; then fail "existing fence was reusable by another candidate"; return; fi
  pass "primary fence is persistent, idempotent, and candidate-bound"
}
# unit이 inactive여도 PGDATA를 쓰는 수동 container가 돌거나 container 조회가 실패하면 FENCED를 내지 않는다(이중 primary 방지).
primary_fence_requires_stopped_postgres_containers() {
  local root env_case
  for env_case in FAKE_MANUAL_POSTGRES=1 FAKE_PODMAN_RESULT=failure; do
    root="${TMP_DIR}/primary-fence-container-${env_case%%=*}"
    mkdir -p "${root}/state"
    setup_primary_fence_fake_tools "${root}"
    if run_primary_fence "${root}" request-token-1234 100.100.1.8 100.100.1.5 5434 "${env_case}" \
      >"${root}/out.log" 2>"${root}/err.log"; then
      cat "${root}/out.log" "${root}/err.log" >&2
      fail "primary fence acknowledged while a PostgreSQL container was running or unverifiable (${env_case})"
      return
    fi
    if grep -Fq 'FENCED|' "${root}/out.log" || [[ -e "${root}/state/fenced" ]]; then
      fail "primary fence recorded a fence without a verified container stop (${env_case})"
      return
    fi
  done
  pass "primary fence requires the PostgreSQL containers to be verifiably stopped"
}
primary_fence_drain_failure_blocks_durable_fence() {
  local root
  root="${TMP_DIR}/primary-fence-drain-failure"
  mkdir -p "${root}/state"
  setup_primary_fence_fake_tools "${root}"
  if run_primary_fence "${root}" request-token-1234 100.100.1.8 100.100.1.5 5434 \
    FAKE_TAILSCALE_RESULT=failure FAKE_REQUIRE_FENCE_INTENT=1 >/dev/null 2>"${root}/err.log"; then
    fail "primary fence accepted a failed Tailscale drain"
    return
  fi
  grep -Fq 'serve drain svc:hololive-postgres' "${root}/tools.log" || { cat "${root}/tools.log" >&2; fail "failed Tailscale drain was not attempted"; return; }
  if grep -Eq '^stop ' "${root}/tools.log"; then
    cat "${root}/tools.log" >&2
    fail "database stop ran after failed Tailscale drain"
    return
  fi
  grep -Fq 'serve advertise svc:hololive-postgres' "${root}/tools.log" || { cat "${root}/tools.log" >&2; fail "failed drain did not restore service advertisement"; return; }
  [[ ! -e "${root}/state/fenced" && ! -e "${root}/state/fence.intent" ]] || { fail "failed Tailscale drain left durable fence state"; return; }
  pass "failed Tailscale drain blocks durable fencing and database stop"
}
primary_fence_restores_route_after_incomplete_fence() {
  local root stopped_root
  root="${TMP_DIR}/primary-fence-transitioning"
  mkdir -p "${root}/state" "${root}/fake-unit"
  setup_primary_fence_fake_tools "${root}"
  printf 'deactivating\n' >"${root}/fake-unit/state"
  if run_primary_fence "${root}" request-token-1234 100.100.1.8 100.100.1.5 5434 >/dev/null 2>"${root}/err.log"; then
    fail "primary fence accepted a transitioning PostgreSQL unit"
    return
  fi
  if grep -Eq 'serve drain|serve advertise|^stop ' "${root}/tools.log"; then
    cat "${root}/tools.log" >&2
    fail "transitioning unit guard mutated the service route or unit"
    return
  fi
  [[ ! -e "${root}/state/fenced" && ! -e "${root}/state/fence.intent" ]] || { fail "transitioning unit guard left durable fence state"; return; }
  stopped_root="${TMP_DIR}/primary-fence-route-stays-drained"
  mkdir -p "${stopped_root}/state"
  setup_primary_fence_fake_tools "${stopped_root}"
  printf 'inactive\n' >"${stopped_root}/fake-unit/state"
  if run_primary_fence "${stopped_root}" request-token-1234 100.100.1.8 100.100.1.5 5434 \
    FAKE_STOP_RESULT_STATE=deactivating >/dev/null 2>"${stopped_root}/err.log"; then
    fail "primary fence ignored a stopped-unit verification failure"
    return
  fi
  if grep -Fq 'serve advertise svc:hololive-postgres' "${stopped_root}/tools.log"; then
    cat "${stopped_root}/tools.log" >&2
    fail "incomplete fence advertised the service without a running PostgreSQL"
    return
  fi
  [[ -e "${stopped_root}/state/fence.intent" ]] || { fail "incomplete fence without a running PostgreSQL dropped its intent"; return; }
  pass "incomplete fence restores the route only while PostgreSQL is running"
}
primary_fence_crash_points_preserve_intent() {
  local root crash_point
  for crash_point in drain stop; do
    root="${TMP_DIR}/primary-fence-crash-${crash_point}"
    mkdir -p "${root}/state"
    setup_primary_fence_fake_tools "${root}"
    if ( run_primary_fence "${root}" request-token-1234 100.100.1.8 100.100.1.5 5434 \
      FAKE_REQUIRE_FENCE_INTENT=1 FAKE_FENCE_CRASH_AFTER="${crash_point}" \
      FAKE_FENCE_PROCESS_MATCH="${PRIMARY_FENCE}" ) >/dev/null 2>"${root}/err.log"; then
      fail "primary fence survived injected crash after ${crash_point}"
      return
    fi
    [[ -r "${root}/state/fence.intent" && ! -e "${root}/state/fenced" ]] || {
      fail "crash after ${crash_point} did not preserve boot-visible intent"
      return
    }
    grep -Fq 'state=fencing' "${root}/state/fence.intent" || {
      fail "crash after ${crash_point} preserved an invalid intent"
      return
    }
  done
  pass "every pre-fence mutation crash preserves boot-visible intent"
}
primary_fence_rolls_back_post_intent_failure() {
  local root advertise_failed_root
  root="${TMP_DIR}/primary-fence-post-intent-rollback"
  mkdir -p "${root}/state"
  setup_primary_fence_fake_tools "${root}"
  if run_primary_fence "${root}" request-token-1234 100.100.1.8 100.100.1.5 5434 \
    FAKE_STOP_RESULT_STATE=active >/dev/null 2>"${root}/err.log"; then
    fail "primary fence ignored a post-intent stop verification failure"
    return
  fi
  [[ ! -e "${root}/state/fence.intent" && ! -e "${root}/state/fenced" ]] || {
    fail "successful rollback left durable fence state"
    return
  }
  grep -Fq 'serve advertise svc:hololive-postgres' "${root}/tools.log" || {
    cat "${root}/tools.log" >&2
    fail "post-intent rollback did not restore the service advertisement"
    return
  }

  advertise_failed_root="${TMP_DIR}/primary-fence-post-intent-advertise-failure"
  mkdir -p "${advertise_failed_root}/state"
  setup_primary_fence_fake_tools "${advertise_failed_root}"
  if run_primary_fence "${advertise_failed_root}" request-token-1234 100.100.1.8 100.100.1.5 5434 \
    FAKE_STOP_RESULT_STATE=active FAKE_TAILSCALE_ADVERTISE_RESULT=failure \
    >/dev/null 2>"${advertise_failed_root}/err.log"; then
    fail "primary fence ignored a service advertisement rollback failure"
    return
  fi
  [[ -e "${advertise_failed_root}/state/fence.intent" && ! -e "${advertise_failed_root}/state/fenced" ]] || {
    fail "advertisement failure did not preserve fail-closed intent state"
    return
  }
  pass "post-intent fence failure restores the route or remains fail-closed"
}
primary_fence_reload_guard_blocks_before_drain() {
  local root
  root="${TMP_DIR}/primary-fence-reload-guard"
  mkdir -p "${root}/state"
  setup_primary_fence_fake_tools "${root}"
  if run_primary_fence "${root}" request-token-1234 100.100.1.8 100.100.1.5 5434 \
    FAKE_NEED_DAEMON_RELOAD=yes >/dev/null 2>"${root}/err.log"; then
    fail "primary fence ignored NeedDaemonReload"
    return
  fi
  grep -Fq 'requires daemon-reload' "${root}/err.log" || { cat "${root}/err.log" >&2; fail "reload guard error was not reported"; return; }
  if grep -Eq 'serve drain|^stop ' "${root}/tools.log"; then
    cat "${root}/tools.log" >&2
    fail "reload guard allowed fence mutation"
    return
  fi
  [[ ! -e "${root}/state/fenced" && ! -e "${root}/state/fence.intent" ]] || { fail "reload guard left durable fence state"; return; }
  pass "pending daemon reload blocks fencing before service drain"
}
fence_and_unfence_share_transition_lock() {
  grep -Fq "LOCK_FILE=\"\${STATE_DIR}/transition.lock\"" "${PRIMARY_FENCE}" || { fail "fence does not use the shared transition lock"; return; }
  grep -Fq "LOCK_FILE=\"\${STATE_DIR}/transition.lock\"" "${PRIMARY_UNFENCE}" || { fail "unfence does not use the shared transition lock"; return; }
  pass "fence and unfence serialize through one transition lock"
}
