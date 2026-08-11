#!/usr/bin/env bash
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  printf '%s\n' "source-only helper: ${BASH_SOURCE[0]}" >&2
  exit 1
fi

setup_primary_unfence_fake_tools() {
  local root="$1"
  mkdir -p "${root}/bin" "${root}/fake-unfence-docker"
  printf 'true\n' >"${root}/fake-unfence-docker/holo-postgres.running"
  printf 'no\n' >"${root}/fake-unfence-docker/holo-postgres.restart"
  printf 'false\n' >"${root}/fake-unfence-docker/deunhealth.running"
  printf 'no\n' >"${root}/fake-unfence-docker/deunhealth.restart"
  printf '#!/usr/bin/env bash\nprintf '\''1: tailscale0    inet 100.100.1.8/32 scope global tailscale0\\n'\''' >"${root}/bin/ip"
  cat >"${root}/bin/systemctl" <<'EOF_SYSTEMCTL_UNFENCE'
#!/usr/bin/env bash
if [[ "${1:-}" == show ]]; then
  case " $* " in
    *' -p ActiveState '*) printf '%s\n' "${FAKE_UNFENCE_UNIT_ACTIVE:-active}" ;;
    *' -p SubState '*) printf '%s\n' "${FAKE_UNFENCE_UNIT_SUBSTATE:-exited}" ;;
    *' -p NeedDaemonReload '*) printf '%s\n' "${FAKE_UNFENCE_NEED_DAEMON_RELOAD:-no}" ;;
    *) exit 2 ;;
  esac
  exit 0
fi
exit 2
EOF_SYSTEMCTL_UNFENCE
  cat >"${root}/bin/docker" <<'EOF_DOCKER_UNFENCE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${UNFENCE_TOOL_LOG:?}"
state_dir="$(dirname -- "${UNFENCE_TOOL_LOG}")/fake-unfence-docker"
last_arg="${!#}"
case "${1:-}" in
  inspect)
    [[ -r "${state_dir}/${last_arg}.running" ]] || exit 1
    if [[ "${2:-}" == -f ]]; then
      case "${3:-}" in
        *State.Running*) printf '%s\n' "$(<"${state_dir}/${last_arg}.running")" ;;
        *RestartPolicy.Name*) printf '%s\n' "$(<"${state_dir}/${last_arg}.restart")" ;;
      esac
    fi ;;
  update)
    [[ "${FAKE_UNFENCE_UPDATE_FAILURE_CONTAINER:-}" != "${last_arg}" || "${2:-}" != --restart=always ]] || exit 1
    printf '%s\n' "${2#--restart=}" >"${state_dir}/${last_arg}.restart" ;;
  start) printf 'true\n' >"${state_dir}/${last_arg}.running" ;;
  stop) printf 'false\n' >"${state_dir}/${last_arg}.running" ;;
  exec)
    if [[ " $* " == *" -h 100.100.1.5 "* ]]; then printf '%s\n' "${UNFENCE_REMOTE_STATUS:-f|off}"; else printf '%s\n' "${UNFENCE_LOCAL_STATUS:-t|on|f|streaming|100.100.1.5|5434}"; fi ;;
  *) exit 2 ;;
esac
EOF_DOCKER_UNFENCE
  chmod 0755 "${root}/bin/ip" "${root}/bin/systemctl" "${root}/bin/docker"
}
run_primary_unfence() {
  local root="$1" fence_token="$2" expected_host="$3" new_host="$4" new_port="$5"
  shift 5
  env PATH="${root}/bin:${PATH}" UNFENCE_TOOL_LOG="${root}/tools.log" \
    POSTGRES_PRIMARY_UNFENCE_STATE_DIR="${root}/state" \
    POSTGRES_PRIMARY_UNFENCE_ALLOW_TEST_STATE_DIR=1 \
    POSTGRES_PRIMARY_UNFENCE_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" "$@" \
    /usr/bin/env bash "${PRIMARY_UNFENCE}" "${fence_token}" "${expected_host}" "${new_host}" "${new_port}"
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
  output="$(run_primary_unfence "${root}" durable-fence-1234 100.100.1.8 100.100.1.5 5434)" || {
      fail "safe primary unfence rejected a verified standby"
      return
    }
  [[ "${output}" == 'UNFENCED|100.100.1.8|100.100.1.5:5434|durable-fence-1234' ]] || { printf '%s\n' "${output}" >&2; fail "unfence acknowledgement invalid"; return; }
  [[ ! -e "${root}/state/fenced" ]] || { fail "verified unfence left the fence marker"; return; }
  [[ "$(<"${root}/fake-unfence-docker/holo-postgres.restart")" == always \
    && "$(<"${root}/fake-unfence-docker/deunhealth.restart")" == always \
    && "$(<"${root}/fake-unfence-docker/holo-postgres.running")" == true \
    && "$(<"${root}/fake-unfence-docker/deunhealth.running")" == true ]] || {
    fail "verified unfence did not restore the Compose container lifecycle"
    return
  }

  cat >"${root}/state/fenced" <<'EOF_FENCED_AGAIN'
state=fenced
request_id=first-request-1234
fence_token=durable-fence-1234
primary_host=100.100.1.8
new_primary=100.100.1.5:5434
fenced_at=200
EOF_FENCED_AGAIN
  chmod 0600 "${root}/state/fenced"
  if run_primary_unfence "${root}" durable-fence-1234 100.100.1.8 100.100.1.5 5434 \
    UNFENCE_LOCAL_STATUS='f|off|f|||' >/dev/null 2>&1; then
    fail "unfence accepted a local writer"
    return
  fi
  [[ -e "${root}/state/fenced" ]] || { fail "failed unfence removed the durable fence"; return; }
  if run_primary_unfence "${root}" durable-fence-1234 100.100.1.8 100.100.1.5 5434 \
    FAKE_UNFENCE_UNIT_SUBSTATE=running >/dev/null 2>&1; then
    fail "unfence accepted a transitioning Compose unit"
    return
  fi
  [[ -e "${root}/state/fenced" ]] || { fail "transitioning-unit guard removed the durable fence"; return; }
  printf 'no\n' >"${root}/fake-unfence-docker/holo-postgres.restart"
  printf 'no\n' >"${root}/fake-unfence-docker/deunhealth.restart"
  printf 'false\n' >"${root}/fake-unfence-docker/deunhealth.running"
  if run_primary_unfence "${root}" durable-fence-1234 100.100.1.8 100.100.1.5 5434 \
    FAKE_UNFENCE_UPDATE_FAILURE_CONTAINER=deunhealth >/dev/null 2>&1; then
    fail "unfence ignored a lifecycle reconciliation failure"
    return
  fi
  [[ -e "${root}/state/fenced" \
    && "$(<"${root}/fake-unfence-docker/holo-postgres.restart")" == no \
    && "$(<"${root}/fake-unfence-docker/deunhealth.restart")" == no \
    && "$(<"${root}/fake-unfence-docker/deunhealth.running")" == false ]] || {
    fail "failed lifecycle reconciliation did not remain fenced"
    return
  }
  pass "unfence removes boot fencing only after streaming-standby verification"
}
