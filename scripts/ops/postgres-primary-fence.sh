#!/usr/bin/env bash
# Runs on the old primary through a restricted sudo/SSH command.
# A persistent fence marker blocks hololive-compose.service on later boots. The
# running consumer containers stay up while Autoheal and PostgreSQL are stopped.

set -euo pipefail

REQUEST_ID="${1:-}"
EXPECTED_PRIMARY_HOST="${2:-}"
NEW_PRIMARY_HOST="${3:-}"
NEW_PRIMARY_PORT="${4:-}"
TAILSCALE_SERVICE="${5:-}"
COMPOSE_UNIT="${POSTGRES_PRIMARY_FENCE_COMPOSE_UNIT:-hololive-compose.service}"
POSTGRES_CONTAINER="${POSTGRES_PRIMARY_FENCE_CONTAINER:-holo-postgres}"
AUTOHEAL_CONTAINER="${POSTGRES_PRIMARY_FENCE_AUTOHEAL_CONTAINER:-deunhealth}"
TAILSCALE_PATH="${POSTGRES_PRIMARY_FENCE_TAILSCALE_PATH:-/usr/bin/tailscale}"
STATE_DIR="${POSTGRES_PRIMARY_FENCE_STATE_DIR:-/var/lib/hololive-postgres-fence}"
ALLOW_TEST_STATE_DIR="${POSTGRES_PRIMARY_FENCE_ALLOW_TEST_STATE_DIR:-0}"
ALLOW_NON_ROOT_TEST="${POSTGRES_PRIMARY_FENCE_ALLOW_NON_ROOT_FOR_TEST:-0}"
FENCED_MARKER="${STATE_DIR}/fenced"
INTENT_MARKER="${STATE_DIR}/fence.intent"
LOCK_FILE="${STATE_DIR}/transition.lock"
NOW="${POSTGRES_PRIMARY_FENCE_NOW:-$(/usr/bin/date +%s)}"

is_token() { [[ "$1" =~ ^[A-Za-z0-9._:-]{8,128}$ ]]; }
is_host() { [[ "$1" =~ ^[A-Za-z0-9._:-]+$ ]]; }
is_unit() { [[ "$1" =~ ^[A-Za-z0-9_.@:-]+\.service$ ]]; }
is_container() { [[ "$1" =~ ^[A-Za-z0-9_.-]+$ ]]; }
is_tailscale_service() { [[ "$1" =~ ^svc:[a-z0-9][a-z0-9-]{0,62}$ ]]; }

trusted_executable() {
  local label="$1" path="$2" current owner mode_hex real
  [[ "${path}" == /* ]] || { echo "${label} must be absolute" >&2; return 1; }
  real="$(realpath -e -- "${path}")" || { echo "${label} is missing: ${path}" >&2; return 1; }
  [[ "${real}" == "${path}" && -f "${path}" && ! -L "${path}" && -x "${path}" ]] || {
    echo "${label} must be a canonical executable file" >&2
    return 1
  }
  current="${path}"
  while :; do
    [[ ! -L "${current}" && -e "${current}" ]] || {
      echo "${label} path contains a symlink or missing component: ${current}" >&2
      return 1
    }
    if [[ "${current}" == "/tmp" && "${ALLOW_TEST_STATE_DIR}" == "1" ]]; then
      break
    fi
    owner="$(stat -c '%u' -- "${current}")" || { echo "cannot stat ${label}: ${current}" >&2; return 1; }
    [[ "${owner}" == "0" || ( "${ALLOW_NON_ROOT_TEST}" == "1" && "${owner}" == "${CURRENT_UID}" ) ]] || {
      echo "${label} path must be root-owned: ${current}" >&2
      return 1
    }
    mode_hex="$(stat -c '%f' -- "${current}")" || { echo "cannot stat ${label}: ${current}" >&2; return 1; }
    (( (0x${mode_hex} & 0x0012) == 0 )) || {
      echo "${label} path must not be group/world writable: ${current}" >&2
      return 1
    }
    [[ "${current}" == "/" ]] && break
    current="$(dirname -- "${current}")"
  done
}

CURRENT_UID="$(/usr/bin/id -u)"
[[ "${ALLOW_NON_ROOT_TEST}" == "0" || "${ALLOW_NON_ROOT_TEST}" == "1" ]] || { echo "invalid non-root test flag" >&2; exit 2; }
if [[ "${ALLOW_NON_ROOT_TEST}" == "1" && ( "${CURRENT_UID}" == "0" || "${STATE_DIR}" != /tmp/* ) ]]; then
  echo "non-root test mode requires a non-root uid and /tmp state" >&2
  exit 2
fi
[[ "${CURRENT_UID}" == "0" || "${ALLOW_NON_ROOT_TEST}" == "1" ]] || { echo "primary fence requires root" >&2; exit 1; }
[[ "$#" -eq 5 ]] || { echo "request id, primary hosts, port, and Tailscale Service are required" >&2; exit 2; }
is_token "${REQUEST_ID}" || { echo "invalid request id" >&2; exit 2; }
is_host "${EXPECTED_PRIMARY_HOST}" || { echo "invalid expected primary host" >&2; exit 2; }
is_host "${NEW_PRIMARY_HOST}" || { echo "invalid new primary host" >&2; exit 2; }
if [[ ! "${NEW_PRIMARY_PORT}" =~ ^[0-9]+$ || ${#NEW_PRIMARY_PORT} -gt 5 ]] \
  || (( 10#${NEW_PRIMARY_PORT} <= 0 || 10#${NEW_PRIMARY_PORT} > 65535 )); then
  echo "invalid new primary port" >&2
  exit 2
fi
NEW_PRIMARY_PORT=$((10#${NEW_PRIMARY_PORT}))
is_unit "${COMPOSE_UNIT}" || { echo "invalid compose unit" >&2; exit 2; }
is_container "${POSTGRES_CONTAINER}" || { echo "invalid postgres container" >&2; exit 2; }
is_container "${AUTOHEAL_CONTAINER}" || { echo "invalid autoheal container" >&2; exit 2; }
is_tailscale_service "${TAILSCALE_SERVICE}" || { echo "invalid Tailscale Service" >&2; exit 2; }
[[ "${TAILSCALE_SERVICE}" == "svc:hololive-postgres" ]] || { echo "unexpected Tailscale Service" >&2; exit 2; }
[[ "${NOW}" =~ ^[0-9]+$ ]] || { echo "invalid timestamp" >&2; exit 2; }
[[ "${ALLOW_TEST_STATE_DIR}" == "0" || "${ALLOW_TEST_STATE_DIR}" == "1" ]] || { echo "invalid test state flag" >&2; exit 2; }
if [[ "${ALLOW_TEST_STATE_DIR}" == "1" && "${STATE_DIR}" != /tmp/* ]]; then
  echo "test fence state must be below /tmp" >&2
  exit 2
fi
if [[ "${ALLOW_TEST_STATE_DIR}" == "0" ]]; then
  PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
  export PATH
fi
[[ "${STATE_DIR}" == "/var/lib/hololive-postgres-fence" || "${ALLOW_TEST_STATE_DIR}" == "1" ]] || {
  echo "refusing noncanonical fence state directory" >&2
  exit 2
}
[[ "${STATE_DIR}" == /* ]] || { echo "state dir must be absolute" >&2; exit 2; }
trusted_executable "Tailscale binary" "${TAILSCALE_PATH}" || exit 1

# The request must name an address actually configured on this host. This keeps a
# misrouted SSH command from fencing the wrong machine.
if ! ip -o addr show scope global | awk '{print $4}' | cut -d/ -f1 | grep -Fxq -- "${EXPECTED_PRIMARY_HOST}"; then
  echo "expected primary address is not configured on this host" >&2
  exit 1
fi

install -d -m 0700 -- "${STATE_DIR}"
state_real="$(realpath -e -- "${STATE_DIR}")"
state_owner="$(stat -c '%u' -- "${STATE_DIR}")"
[[ "${state_real}" == "${STATE_DIR}" ]] || { echo "fence state directory must be canonical" >&2; exit 1; }
[[ "${state_owner}" == "0" || ( "${ALLOW_NON_ROOT_TEST}" == "1" && "${state_owner}" == "${CURRENT_UID}" ) ]] || {
  echo "fence state directory must be root-owned" >&2
  exit 1
}
state_mode_hex="$(stat -c '%f' -- "${STATE_DIR}")"
(( (0x${state_mode_hex} & 0x0012) == 0 )) || { echo "fence state directory is writable by group/other" >&2; exit 1; }

exec 9>"${LOCK_FILE}"
flock -x 9

marker_value() {
  local file="$1" key="$2"
  awk -F= -v key="${key}" '$1 == key {sub(/^[^=]*=/, ""); print; exit}' "${file}" 2>/dev/null || true
}

atomic_write() {
  local target="$1"
  local tmp="${target}.tmp.$$"
  umask 077
  cat >"${tmp}"
  chmod 0600 "${tmp}"
  sync -f "${tmp}"
  mv -f -- "${tmp}" "${target}"
  sync -f "${target}"
  sync -f "${STATE_DIR}"
}

# Persistent boot fencing is part of the acknowledgement contract. Refuse to
# claim success against an old unit that would restart Compose after reboot.
need_daemon_reload="$(systemctl show "${COMPOSE_UNIT}" -p NeedDaemonReload --value)" || {
  echo "cannot inspect reload state for ${COMPOSE_UNIT}" >&2
  exit 1
}
[[ "${need_daemon_reload}" == "no" ]] || {
  echo "${COMPOSE_UNIT} requires daemon-reload before fencing" >&2
  exit 1
}
unit_text="$(systemctl cat "${COMPOSE_UNIT}")" || { echo "cannot inspect ${COMPOSE_UNIT}" >&2; exit 1; }
for condition in \
  'ConditionPathExists=!/var/lib/hololive-postgres-fence/fence.intent' \
  'ConditionPathExists=!/var/lib/hololive-postgres-fence/fenced'; do
  grep -Fqx -- "${condition}" <<<"${unit_text}" || {
    echo "${COMPOSE_UNIT} is missing persistent fence condition: ${condition}" >&2
    exit 1
  }
done

fence_token="${REQUEST_ID}"
if [[ -r "${FENCED_MARKER}" ]]; then
  existing_state="$(marker_value "${FENCED_MARKER}" state)"
  existing_host="$(marker_value "${FENCED_MARKER}" primary_host)"
  existing_token="$(marker_value "${FENCED_MARKER}" fence_token)"
  if [[ -z "${existing_token}" ]]; then
    existing_token="$(marker_value "${FENCED_MARKER}" request_id)"
  fi
  existing_new_primary="$(marker_value "${FENCED_MARKER}" new_primary)"
  existing_fenced_at="$(marker_value "${FENCED_MARKER}" fenced_at)"
  if [[ "${existing_state}" != "fenced" || "${existing_host}" != "${EXPECTED_PRIMARY_HOST}" \
    || "${existing_new_primary}" != "${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" ]] \
    || ! is_token "${existing_token}" || [[ ! "${existing_fenced_at}" =~ ^[0-9]+$ ]]; then
    echo "existing fence marker is invalid" >&2
    exit 1
  fi
  fence_token="${existing_token}"
fi

docker info >/dev/null 2>&1 || { echo "cannot verify Docker state" >&2; exit 1; }

container_is_absent() {
  local container="$1" names
  names="$(docker ps -a --filter "name=^/${container}$" --format '{{.Names}}')" || return 1
  ! grep -Fxq -- "${container}" <<<"${names}"
}

declare -A ORIGINAL_PRESENT=()
declare -A ORIGINAL_RUNNING=()
declare -A ORIGINAL_RESTART=()

capture_container_state() {
  local container="$1" raw running restart_name restart_max restart_spec extra
  if ! docker inspect "${container}" >/dev/null 2>&1; then
    container_is_absent "${container}" || return 1
    ORIGINAL_PRESENT["${container}"]=0
    ORIGINAL_RUNNING["${container}"]=false
    ORIGINAL_RESTART["${container}"]=no
    return 0
  fi
  raw="$(docker inspect -f '{{.State.Running}}|{{.HostConfig.RestartPolicy.Name}}|{{.HostConfig.RestartPolicy.MaximumRetryCount}}' "${container}")" || return 1
  IFS='|' read -r running restart_name restart_max extra <<<"${raw}"
  [[ -z "${extra:-}" && ( "${running}" == "true" || "${running}" == "false" ) \
    && "${restart_max}" =~ ^[0-9]+$ ]] || return 1
  case "${restart_name}" in
    no|always|unless-stopped) restart_spec="${restart_name}" ;;
    on-failure) restart_spec="on-failure:${restart_max}" ;;
    *) return 1 ;;
  esac
  ORIGINAL_PRESENT["${container}"]=1
  ORIGINAL_RUNNING["${container}"]="${running}"
  ORIGINAL_RESTART["${container}"]="${restart_spec}"
}

restore_container_state() {
  local container="$1" expected_running expected_restart running restart_name restart_max actual_restart
  [[ "${ORIGINAL_PRESENT[${container}]}" == "1" ]] || return 0
  expected_running="${ORIGINAL_RUNNING[${container}]}"
  expected_restart="${ORIGINAL_RESTART[${container}]}"
  docker update "--restart=${expected_restart}" "${container}" >/dev/null || return 1
  running="$(docker inspect -f '{{.State.Running}}' "${container}" 2>/dev/null || printf 'unknown')"
  if [[ "${expected_running}" == "true" && "${running}" == "false" ]]; then
    docker start "${container}" >/dev/null || return 1
  elif [[ "${expected_running}" == "false" && "${running}" == "true" ]]; then
    docker stop -t 15 "${container}" >/dev/null || return 1
  fi
  running="$(docker inspect -f '{{.State.Running}}' "${container}" 2>/dev/null || printf 'unknown')"
  restart_name="$(docker inspect -f '{{.HostConfig.RestartPolicy.Name}}' "${container}" 2>/dev/null || printf 'unknown')"
  restart_max="$(docker inspect -f '{{.HostConfig.RestartPolicy.MaximumRetryCount}}' "${container}" 2>/dev/null || printf 'unknown')"
  actual_restart="${restart_name}"
  [[ "${restart_name}" != "on-failure" ]] || actual_restart="on-failure:${restart_max}"
  [[ "${running}" == "${expected_running}" && "${actual_restart}" == "${expected_restart}" ]]
}

capture_container_state "${AUTOHEAL_CONTAINER}" || { echo "cannot capture autoheal state" >&2; exit 1; }
capture_container_state "${POSTGRES_CONTAINER}" || { echo "cannot capture PostgreSQL state" >&2; exit 1; }

MUTATION_STARTED=0
SERVICE_ROUTE_MUTATED=0
restore_service_after_incomplete_fence() {
  local rc=$? running rollback_ok=1
  trap - EXIT
  if (( rc != 0 && MUTATION_STARTED == 1 )) && [[ ! -r "${FENCED_MARKER}" ]]; then
    running="$(docker inspect -f '{{.State.Running}}' "${POSTGRES_CONTAINER}" 2>/dev/null || printf 'unknown')"
    if [[ "${running}" == "true" && "${ORIGINAL_PRESENT[${POSTGRES_CONTAINER}]}" == "1" \
      && "${ORIGINAL_RUNNING[${POSTGRES_CONTAINER}]}" == "true" ]]; then
      restore_container_state "${POSTGRES_CONTAINER}" || rollback_ok=0
      restore_container_state "${AUTOHEAL_CONTAINER}" || rollback_ok=0
      if (( rollback_ok == 1 && SERVICE_ROUTE_MUTATED == 1 )) \
        && ! "${TAILSCALE_PATH}" serve advertise "${TAILSCALE_SERVICE}" >/dev/null 2>&1; then
        rollback_ok=0
      fi
      if (( rollback_ok == 1 )); then
        rm -f -- "${INTENT_MARKER}" || rollback_ok=0
        sync -f "${STATE_DIR}" || rollback_ok=0
      fi
      (( rollback_ok == 1 )) || echo "failed to restore the primary after incomplete fence" >&2
    fi
  fi
  exit "${rc}"
}
trap restore_service_after_incomplete_fence EXIT
disable_container_restart() {
  local container="$1" restart_policy
  if ! docker inspect "${container}" >/dev/null 2>&1; then
    container_is_absent "${container}" || { echo "container lookup was inconsistent: ${container}" >&2; return 1; }
    return 0
  fi
  docker update --restart=no "${container}" >/dev/null || return 1
  restart_policy="$(docker inspect -f '{{.HostConfig.RestartPolicy.Name}}' "${container}" 2>/dev/null || printf 'unknown')"
  [[ "${restart_policy}" == "no" ]] || { echo "container restart policy is not fenced: ${container}" >&2; return 1; }
}
durably_stop_container() {
  local container="$1" timeout="$2" running restart_policy
  disable_container_restart "${container}" || return 1
  if ! docker inspect "${container}" >/dev/null 2>&1; then
    container_is_absent "${container}" || { echo "container lookup was inconsistent: ${container}" >&2; return 1; }
    return 0
  fi
  docker stop -t "${timeout}" "${container}" >/dev/null || true
  running="$(docker inspect -f '{{.State.Running}}' "${container}" 2>/dev/null || printf 'unknown')"
  restart_policy="$(docker inspect -f '{{.HostConfig.RestartPolicy.Name}}' "${container}" 2>/dev/null || printf 'unknown')"
  [[ "${running}" == "false" && "${restart_policy}" == "no" ]] || {
    echo "container is not durably stopped: ${container}" >&2
    return 1
  }
}

if [[ ! -r "${FENCED_MARKER}" ]]; then
  if [[ -r "${INTENT_MARKER}" ]]; then
    intent_state="$(marker_value "${INTENT_MARKER}" state)"
    intent_host="$(marker_value "${INTENT_MARKER}" primary_host)"
    intent_primary="$(marker_value "${INTENT_MARKER}" new_primary)"
    intent_token="$(marker_value "${INTENT_MARKER}" fence_token)"
    [[ -n "${intent_token}" ]] || intent_token="$(marker_value "${INTENT_MARKER}" request_id)"
    if [[ "${intent_state}" != fencing || "${intent_host}" != "${EXPECTED_PRIMARY_HOST}" \
      || "${intent_primary}" != "${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" ]] \
      || ! is_token "${intent_token}"; then
      echo "existing fence intent is invalid" >&2
      exit 1
    fi
    fence_token="${intent_token}"
  else
    atomic_write "${INTENT_MARKER}" <<EOF_INTENT
state=fencing
request_id=${REQUEST_ID}
fence_token=${fence_token}
primary_host=${EXPECTED_PRIMARY_HOST}
new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}
started_at=${NOW}
EOF_INTENT
  fi
  MUTATION_STARTED=1
fi

disable_container_restart "${AUTOHEAL_CONTAINER}" || exit 1
disable_container_restart "${POSTGRES_CONTAINER}" || exit 1

SERVICE_ROUTE_MUTATED=1
if ! "${TAILSCALE_PATH}" serve drain "${TAILSCALE_SERVICE}" >/dev/null 2>&1; then
  echo "cannot drain Tailscale Service: ${TAILSCALE_SERVICE}" >&2
  exit 1
fi

# Disable autoheal before stopping PostgreSQL. Consumer containers keep their
# stable endpoint connections and reconnect after the route moves.
durably_stop_container "${AUTOHEAL_CONTAINER}" 15 || exit 1
durably_stop_container "${POSTGRES_CONTAINER}" 60 || exit 1

if [[ ! -r "${FENCED_MARKER}" ]]; then
  atomic_write "${FENCED_MARKER}" <<EOF_FENCED
state=fenced
request_id=${REQUEST_ID}
fence_token=${fence_token}
primary_host=${EXPECTED_PRIMARY_HOST}
new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}
fenced_at=${NOW}
EOF_FENCED
fi
rm -f -- "${INTENT_MARKER}"
sync -f "${STATE_DIR}"
printf 'FENCED|%s|%s:%s|%s|%s\n' \
  "${EXPECTED_PRIMARY_HOST}" "${NEW_PRIMARY_HOST}" "${NEW_PRIMARY_PORT}" "${REQUEST_ID}" "${fence_token}"
