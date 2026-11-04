#!/usr/bin/env bash
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  printf '%s\n' "source-only helper: ${BASH_SOURCE[0]}" >&2
  exit 1
fi

setup_primary_unfence_fake_tools() {
  local root="$1"
  mkdir -p "${root}/bin" "${root}/fake-unfence"
  printf 'inactive\n' >"${root}/fake-unfence/unit.state"
  printf 'true|no||hololive-bot_holo-pg-data\n' >"${root}/fake-unfence/container"
  printf '#!/usr/bin/env bash\nprintf '\''1: tailscale0    inet 100.100.1.8/32 scope global tailscale0\\n'\''' >"${root}/bin/ip"
  cat >"${root}/bin/systemctl" <<'EOF_SYSTEMCTL_UNFENCE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${UNFENCE_TOOL_LOG:?}"
state_dir="$(dirname -- "${UNFENCE_TOOL_LOG}")/fake-unfence"
state_root="$(dirname -- "${UNFENCE_TOOL_LOG}")/state"
case "${1:-}" in
  show)
    case " $* " in
      *' -p ActiveState '*) printf '%s\n' "${FAKE_UNFENCE_UNIT_ACTIVE:-$(<"${state_dir}/unit.state")}" ;;
      *' -p NeedDaemonReload '*) printf '%s\n' "${FAKE_UNFENCE_NEED_DAEMON_RELOAD:-no}" ;;
      *) exit 2 ;;
    esac ;;
  start)
    # 실제 unit처럼 표식이 남아 있으면 조건으로 건너뛴다(성공 반환, inactive 유지).
    [[ ! -e "${state_root}/fenced" ]] || exit 0
    [[ "${FAKE_UNFENCE_START_FAILURE:-0}" != 1 ]] || exit 1
    printf 'active\n' >"${state_dir}/unit.state"
    printf 'true|no|holo-postgres.service|hololive-bot_holo-pg-data\n' >"${state_dir}/container" ;;
  stop) printf 'inactive\n' >"${state_dir}/unit.state" ;;
  *) exit 2 ;;
esac
EOF_SYSTEMCTL_UNFENCE
  cat >"${root}/bin/podman" <<'EOF_PODMAN_UNFENCE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${UNFENCE_TOOL_LOG:?}"
state_dir="$(dirname -- "${UNFENCE_TOOL_LOG}")/fake-unfence"
case "${1:-}" in
  inspect) printf '%s\n' "$(<"${state_dir}/container")" ;;
  exec)
    if [[ " $* " == *" -h 100.100.1.5 "* ]]; then
      printf '%s\n' "${UNFENCE_REMOTE_STATUS:-f|off}"
    elif [[ "$(<"${state_dir}/unit.state")" == active && -n "${UNFENCE_UNIT_LOCAL_STATUS:-}" ]]; then
      printf '%s\n' "${UNFENCE_UNIT_LOCAL_STATUS}"
    else
      printf '%s\n' "${UNFENCE_LOCAL_STATUS:-t|on|f|streaming|100.100.1.5|5434}"
    fi ;;
  *) exit 2 ;;
esac
EOF_PODMAN_UNFENCE
  chmod 0755 "${root}/bin/ip" "${root}/bin/systemctl" "${root}/bin/podman"
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
write_unfence_marker() {
  local root="$1"
  cat >"${root}/state/fenced" <<'EOF_FENCED'
state=fenced
request_id=first-request-1234
fence_token=durable-fence-1234
primary_host=100.100.1.8
new_primary=100.100.1.5:5434
fenced_at=200
EOF_FENCED
  chmod 0600 "${root}/state/fenced"
}
reset_unfence_runtime() {
  local root="$1"
  printf 'inactive\n' >"${root}/fake-unfence/unit.state"
  printf 'true|no||hololive-bot_holo-pg-data\n' >"${root}/fake-unfence/container"
  write_unfence_marker "${root}"
}
primary_unfence_requires_reseeded_streaming_standby() {
  local root output marker
  root="${TMP_DIR}/primary-unfence"
  mkdir -p "${root}/state"
  chmod 0700 "${root}/state"
  setup_primary_unfence_fake_tools "${root}"
  : >"${root}/tools.log"
  write_unfence_marker "${root}"
  marker="$(<"${root}/state/fenced")"
  output="$(run_primary_unfence "${root}" durable-fence-1234 100.100.1.8 100.100.1.5 5434)" || {
      fail "safe primary unfence rejected a verified standby"
      return
    }
  [[ "${output}" == 'UNFENCED|100.100.1.8|100.100.1.5:5434|durable-fence-1234' ]] || { printf '%s\n' "${output}" >&2; fail "unfence acknowledgement invalid"; return; }
  [[ ! -e "${root}/state/fenced" ]] || { fail "verified unfence left the fence marker"; return; }
  [[ "$(<"${root}/fake-unfence/unit.state")" == active ]] || {
    cat "${root}/tools.log" >&2
    fail "verified unfence did not hand the standby to holo-postgres.service"
    return
  }

  reset_unfence_runtime "${root}"
  if run_primary_unfence "${root}" durable-fence-1234 100.100.1.8 100.100.1.5 5434 \
    UNFENCE_LOCAL_STATUS='f|off|f|||' >/dev/null 2>&1; then
    fail "unfence accepted a local writer"
    return
  fi
  [[ -e "${root}/state/fenced" ]] || { fail "failed unfence removed the durable fence"; return; }

  reset_unfence_runtime "${root}"
  if run_primary_unfence "${root}" durable-fence-1234 100.100.1.8 100.100.1.5 5434 \
    FAKE_UNFENCE_UNIT_ACTIVE=activating >/dev/null 2>&1; then
    fail "unfence accepted a transitioning PostgreSQL unit"
    return
  fi
  [[ -e "${root}/state/fenced" ]] || { fail "transitioning-unit guard removed the durable fence"; return; }

  for container in 'true|always||hololive-bot_holo-pg-data' 'true|no|holo-postgres.service|hololive-bot_holo-pg-data' 'true|no||other-volume' 'false|no||hololive-bot_holo-pg-data'; do
    reset_unfence_runtime "${root}"
    printf '%s\n' "${container}" >"${root}/fake-unfence/container"
    if run_primary_unfence "${root}" durable-fence-1234 100.100.1.8 100.100.1.5 5434 >/dev/null 2>&1; then
      fail "unfence accepted a non-manual or foreign standby container: ${container}"
      return
    fi
    [[ -e "${root}/state/fenced" ]] || { fail "standby container guard removed the durable fence"; return; }
  done

  for failure in FAKE_UNFENCE_START_FAILURE=1 'UNFENCE_UNIT_LOCAL_STATUS=f|off|f|||'; do
    reset_unfence_runtime "${root}"
    if run_primary_unfence "${root}" durable-fence-1234 100.100.1.8 100.100.1.5 5434 "${failure}" >/dev/null 2>&1; then
      fail "unfence ignored a unit handoff failure: ${failure}"
      return
    fi
    [[ "$(<"${root}/state/fenced")" == "${marker}" && "$(<"${root}/fake-unfence/unit.state")" == inactive ]] || {
      fail "failed unit handoff did not restore the same fence and stop the unit: ${failure}"
      return
    }
  done
  pass "unfence removes boot fencing only after streaming-standby verification"
}
