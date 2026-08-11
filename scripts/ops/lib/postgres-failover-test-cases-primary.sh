#!/usr/bin/env bash
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  printf '%s\n' "source-only helper: ${BASH_SOURCE[0]}" >&2
  exit 1
fi

PRIMARY_FENCE="${ROOT_DIR}/scripts/ops/postgres-primary-fence.sh"
PRIMARY_UNFENCE="${ROOT_DIR}/scripts/ops/postgres-primary-unfence.sh"

setup_primary_fence_fake_tools() {
  local root="$1"
  mkdir -p "${root}/bin" "${root}/fake-docker"
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
case "${1:-}" in
  show)
    [[ " $* " == *' -p NeedDaemonReload '* ]] || exit 2
    printf '%s\n' "${FAKE_NEED_DAEMON_RELOAD:-no}" ;;
  cat)
    printf '%s\n' 'ConditionPathExists=!/var/lib/hololive-postgres-fence/fence.intent' 'ConditionPathExists=!/var/lib/hololive-postgres-fence/fenced' ;;
  *) exit 2 ;;
esac
EOF_SYSTEMCTL
  for container in deunhealth holo-postgres; do
    printf 'true\n' >"${root}/fake-docker/${container}.running"
    printf 'always\n' >"${root}/fake-docker/${container}.restart"
  done
  cat >"${root}/bin/docker" <<'EOF_DOCKER_FENCE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${FENCE_TOOL_LOG:?}"
state_dir="$(dirname -- "${FENCE_TOOL_LOG}")/fake-docker"
last_arg="${!#}"
case "${1:-}" in
  info) ;;
  inspect)
    [[ -r "${state_dir}/${last_arg}.running" ]] || exit 1
    if [[ "${2:-}" == "-f" ]]; then
      running="${FAKE_DOCKER_RUNNING:-$( <"${state_dir}/${last_arg}.running")}"
      restart="${FAKE_DOCKER_RESTART_POLICY:-$( <"${state_dir}/${last_arg}.restart")}"
      case "${3:-}" in
        *State.Running*RestartPolicy.Name*MaximumRetryCount*) printf '%s|%s|0\n' "${running}" "${restart}" ;;
        *State.Running*) printf '%s\n' "${running}" ;;
        *RestartPolicy.Name*) printf '%s\n' "${restart}" ;;
        *MaximumRetryCount*) printf '0\n' ;;
      esac
    fi ;;
  update)
    restart="${2#--restart=}"
    [[ "${restart}" != no || "${FAKE_REQUIRE_FENCE_INTENT:-0}" != 1 || -r "$(dirname -- "${FENCE_TOOL_LOG}")/state/fence.intent" ]] || exit 98
    if [[ -n "${FAKE_DOCKER_UPDATE_FAILURE_CONTAINER:-}" && "${last_arg}" == "${FAKE_DOCKER_UPDATE_FAILURE_CONTAINER}" && "${restart}" == no ]]; then exit 1; fi
    if [[ "${FAKE_DOCKER_ROLLBACK_UPDATE_FAILURE:-0}" == 1 && "${restart}" != no ]]; then exit 1; fi
    printf '%s\n' "${restart%%:*}" >"${state_dir}/${last_arg}.restart"
    [[ "${FAKE_FENCE_CRASH_AFTER:-}" != "update:${last_arg}" || "${restart}" != no ]] || "$(dirname -- "${FENCE_TOOL_LOG}")/bin/fence-crash" ;;
  stop)
    [[ "${FAKE_DOCKER_STOP_STAYS_RUNNING_CONTAINER:-}" == "${last_arg}" ]] || printf 'false\n' >"${state_dir}/${last_arg}.running" ;;
  start) printf 'true\n' >"${state_dir}/${last_arg}.running" ;;
  ps) ;;
  *) exit 2 ;;
esac
EOF_DOCKER_FENCE
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
  chmod 0755 "${root}/bin/ip" "${root}/bin/fence-crash" "${root}/bin/systemctl" "${root}/bin/docker" "${root}/bin/tailscale"
}
run_primary_fence() {
  local root="$1" request_id="$2" expected_host="$3" new_host="$4" new_port="$5"
  shift 5
  env PATH="${root}/bin:${PATH}" FENCE_TOOL_LOG="${root}/tools.log" \
    POSTGRES_PRIMARY_FENCE_TAILSCALE_PATH="${root}/bin/tailscale" \
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
  if grep -Fq 'stop hololive-compose.service' "${root}/tools.log"; then
    cat "${root}/tools.log" >&2
    fail "database fencing stopped stable-endpoint consumers"
    return
  fi
  grep -Fq 'update --restart=no deunhealth' "${root}/tools.log" || { cat "${root}/tools.log" >&2; fail "autoheal was not fenced"; return; }
  grep -Fq 'update --restart=no holo-postgres' "${root}/tools.log" || { cat "${root}/tools.log" >&2; fail "database restart policy was not fenced"; return; }
  [[ "$(grep -Fc 'update --restart=no holo-postgres' "${root}/tools.log")" == "4" ]] || { cat "${root}/tools.log" >&2; fail "idempotent fence did not re-verify/reapply database stop"; return; }
  [[ "$(grep -Fc 'serve drain svc:hololive-postgres' "${root}/tools.log")" == "2" ]] || { cat "${root}/tools.log" >&2; fail "Tailscale Service was not drained for each fence invocation"; return; }
  grep -Fq 'state=fenced' "${root}/state/fenced" || { cat "${root}/state/fenced" >&2; fail "persistent fence marker missing"; return; }
  grep -Fq 'new_primary=100.100.1.5:5434' "${root}/state/fenced" || { cat "${root}/state/fenced" >&2; fail "fence candidate binding missing"; return; }
  if run_primary_fence "${root}" third-token-9012 100.100.1.8 100.100.1.6 5434 >/dev/null 2>&1; then fail "existing fence was reusable by another candidate"; return; fi
  pass "primary fence is persistent, idempotent, and candidate-bound"
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
  if grep -Eq 'stop hololive-compose.service|stop -t' "${root}/tools.log"; then
    cat "${root}/tools.log" >&2
    fail "database or Compose stop ran after failed Tailscale drain"
    return
  fi
  grep -Fq 'update --restart=always holo-postgres' "${root}/tools.log" || { cat "${root}/tools.log" >&2; fail "failed drain did not restore database restart policy"; return; }
  grep -Fq 'serve advertise svc:hololive-postgres' "${root}/tools.log" || { cat "${root}/tools.log" >&2; fail "failed drain did not restore service advertisement"; return; }
  [[ ! -e "${root}/state/fenced" && ! -e "${root}/state/fence.intent" ]] || { fail "failed Tailscale drain left durable fence state"; return; }
  pass "failed Tailscale drain blocks durable fencing and database stop"
}
primary_fence_restores_route_after_incomplete_fence() {
  local root stopped_root
  root="${TMP_DIR}/primary-fence-route-rollback"
  mkdir -p "${root}/state"
  setup_primary_fence_fake_tools "${root}"
  if run_primary_fence "${root}" request-token-1234 100.100.1.8 100.100.1.5 5434 \
    FAKE_DOCKER_RUNNING=true FAKE_DOCKER_UPDATE_FAILURE_CONTAINER=holo-postgres \
    >/dev/null 2>"${root}/err.log"; then
    fail "primary fence ignored a restart-policy failure"
    return
  fi
  if grep -Eq 'serve drain|serve advertise' "${root}/tools.log"; then
    cat "${root}/tools.log" >&2
    fail "pre-drain restart-policy failure mutated the service route"
    return
  fi
  grep -Fq 'update --restart=always deunhealth' "${root}/tools.log" || { cat "${root}/tools.log" >&2; fail "pre-drain failure did not restore autoheal policy"; return; }
  [[ ! -e "${root}/state/fenced" && ! -e "${root}/state/fence.intent" ]] || {
    fail "pre-drain rollback left durable fence state"
    return
  }
  if grep -Fq 'stop hololive-compose.service' "${root}/tools.log"; then
    fail "restart-policy failure reached Compose stop"
    return
  fi
  stopped_root="${TMP_DIR}/primary-fence-route-stays-drained"
  mkdir -p "${stopped_root}/state"
  setup_primary_fence_fake_tools "${stopped_root}"
  if run_primary_fence "${stopped_root}" request-token-1234 100.100.1.8 100.100.1.5 5434 \
    FAKE_DOCKER_RUNNING=false FAKE_DOCKER_UPDATE_FAILURE_CONTAINER=holo-postgres \
    >/dev/null 2>"${stopped_root}/err.log"; then
    fail "primary fence ignored a stopped-container verification failure"
    return
  fi
  if grep -Fq 'serve advertise svc:hololive-postgres' "${stopped_root}/tools.log"; then
    cat "${stopped_root}/tools.log" >&2
    fail "incomplete fence advertised the service without a running PostgreSQL"
    return
  fi
  pass "incomplete pre-drain fence restores runtime only while PostgreSQL is running"
}
primary_fence_crash_points_preserve_intent() {
  local root crash_point slug
  for crash_point in update:deunhealth update:holo-postgres drain; do
    slug="${crash_point//[:]/-}"
    root="${TMP_DIR}/primary-fence-crash-${slug}"
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
  local root failed_root advertise_failed_root postgres_restore_line autoheal_restore_line advertise_line
  root="${TMP_DIR}/primary-fence-post-intent-rollback"
  mkdir -p "${root}/state"
  setup_primary_fence_fake_tools "${root}"
  if run_primary_fence "${root}" request-token-1234 100.100.1.8 100.100.1.5 5434 \
    FAKE_DOCKER_STOP_STAYS_RUNNING_CONTAINER=deunhealth >/dev/null 2>"${root}/err.log"; then
    fail "primary fence ignored a post-intent stop verification failure"
    return
  fi
  [[ ! -e "${root}/state/fence.intent" && ! -e "${root}/state/fenced" ]] || {
    fail "successful rollback left durable fence state"
    return
  }
  [[ "$(<"${root}/fake-docker/holo-postgres.restart")" == always \
    && "$(<"${root}/fake-docker/deunhealth.restart")" == always \
    && "$(<"${root}/fake-docker/holo-postgres.running")" == true \
    && "$(<"${root}/fake-docker/deunhealth.running")" == true ]] || {
    fail "post-intent rollback did not restore container runtime state"
    return
  }
  postgres_restore_line="$(grep -nF 'update --restart=always holo-postgres' "${root}/tools.log" | cut -d: -f1)"
  autoheal_restore_line="$(grep -nF 'update --restart=always deunhealth' "${root}/tools.log" | cut -d: -f1)"
  advertise_line="$(grep -nF 'serve advertise svc:hololive-postgres' "${root}/tools.log" | cut -d: -f1)"
  [[ "${postgres_restore_line}" =~ ^[0-9]+$ && "${autoheal_restore_line}" =~ ^[0-9]+$ \
    && "${advertise_line}" =~ ^[0-9]+$ && "${advertise_line}" -gt "${postgres_restore_line}" \
    && "${advertise_line}" -gt "${autoheal_restore_line}" ]] || {
    cat "${root}/tools.log" >&2
    fail "post-intent rollback did not advertise last"
    return
  }

  failed_root="${TMP_DIR}/primary-fence-post-intent-rollback-failure"
  mkdir -p "${failed_root}/state"
  setup_primary_fence_fake_tools "${failed_root}"
  if run_primary_fence "${failed_root}" request-token-1234 100.100.1.8 100.100.1.5 5434 \
    FAKE_DOCKER_STOP_STAYS_RUNNING_CONTAINER=deunhealth FAKE_DOCKER_ROLLBACK_UPDATE_FAILURE=1 \
    >/dev/null 2>"${failed_root}/err.log"; then
    fail "primary fence ignored a rollback failure"
    return
  fi
  [[ -e "${failed_root}/state/fence.intent" && ! -e "${failed_root}/state/fenced" ]] || {
    fail "failed rollback did not preserve fail-closed intent state"
    return
  }
  if grep -Fq 'serve advertise svc:hololive-postgres' "${failed_root}/tools.log"; then
    fail "failed rollback advertised the service"
    return
  fi

  advertise_failed_root="${TMP_DIR}/primary-fence-post-intent-advertise-failure"
  mkdir -p "${advertise_failed_root}/state"
  setup_primary_fence_fake_tools "${advertise_failed_root}"
  if run_primary_fence "${advertise_failed_root}" request-token-1234 100.100.1.8 100.100.1.5 5434 \
    FAKE_DOCKER_STOP_STAYS_RUNNING_CONTAINER=deunhealth FAKE_TAILSCALE_ADVERTISE_RESULT=failure \
    >/dev/null 2>"${advertise_failed_root}/err.log"; then
    fail "primary fence ignored a service advertisement rollback failure"
    return
  fi
  [[ -e "${advertise_failed_root}/state/fence.intent" && ! -e "${advertise_failed_root}/state/fenced" ]] || {
    fail "advertisement failure did not preserve fail-closed intent state"
    return
  }
  grep -Fq 'serve advertise svc:hololive-postgres' "${advertise_failed_root}/tools.log" || {
    fail "advertisement failure path did not attempt service recovery"
    return
  }
  pass "post-intent fence failure restores runtime state or remains fail-closed"
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
  if grep -Eq 'serve drain|stop hololive-compose.service|stop -t|update --restart=no holo-postgres' "${root}/tools.log"; then
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
