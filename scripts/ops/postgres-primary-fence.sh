#!/usr/bin/env bash
# Runs on the old primary through a restricted sudo/SSH command.
# A persistent fence marker blocks hololive-compose.service on later boots; the
# Autoheal and PostgreSQL restart policies are set to no before both are stopped.

set -euo pipefail

REQUEST_ID="${1:-}"
EXPECTED_PRIMARY_HOST="${2:-}"
NEW_PRIMARY_HOST="${3:-}"
NEW_PRIMARY_PORT="${4:-}"
COMPOSE_UNIT="${POSTGRES_PRIMARY_FENCE_COMPOSE_UNIT:-hololive-compose.service}"
POSTGRES_CONTAINER="${POSTGRES_PRIMARY_FENCE_CONTAINER:-holo-postgres}"
AUTOHEAL_CONTAINER="${POSTGRES_PRIMARY_FENCE_AUTOHEAL_CONTAINER:-deunhealth}"
STATE_DIR="${POSTGRES_PRIMARY_FENCE_STATE_DIR:-/var/lib/hololive-postgres-fence}"
ALLOW_TEST_STATE_DIR="${POSTGRES_PRIMARY_FENCE_ALLOW_TEST_STATE_DIR:-0}"
ALLOW_NON_ROOT_TEST="${POSTGRES_PRIMARY_FENCE_ALLOW_NON_ROOT_FOR_TEST:-0}"
FENCED_MARKER="${STATE_DIR}/fenced"
INTENT_MARKER="${STATE_DIR}/fence.intent"
LOCK_FILE="${STATE_DIR}/fence.lock"
NOW="${POSTGRES_PRIMARY_FENCE_NOW:-$(/usr/bin/date +%s)}"

is_token() { [[ "$1" =~ ^[A-Za-z0-9._:-]{8,128}$ ]]; }
is_host() { [[ "$1" =~ ^[A-Za-z0-9._:-]+$ ]]; }
is_unit() { [[ "$1" =~ ^[A-Za-z0-9_.@:-]+\.service$ ]]; }
is_container() { [[ "$1" =~ ^[A-Za-z0-9_.-]+$ ]]; }

CURRENT_UID="$(/usr/bin/id -u)"
[[ "${ALLOW_NON_ROOT_TEST}" == "0" || "${ALLOW_NON_ROOT_TEST}" == "1" ]] || { echo "invalid non-root test flag" >&2; exit 2; }
if [[ "${ALLOW_NON_ROOT_TEST}" == "1" && ( "${CURRENT_UID}" == "0" || "${STATE_DIR}" != /tmp/* ) ]]; then
  echo "non-root test mode requires a non-root uid and /tmp state" >&2
  exit 2
fi
[[ "${CURRENT_UID}" == "0" || "${ALLOW_NON_ROOT_TEST}" == "1" ]] || { echo "primary fence requires root" >&2; exit 1; }
is_token "${REQUEST_ID}" || { echo "invalid request id" >&2; exit 2; }
is_host "${EXPECTED_PRIMARY_HOST}" || { echo "invalid expected primary host" >&2; exit 2; }
is_host "${NEW_PRIMARY_HOST}" || { echo "invalid new primary host" >&2; exit 2; }
[[ "${NEW_PRIMARY_PORT}" =~ ^[0-9]+$ && ${#NEW_PRIMARY_PORT} -le 5 ]] \
  && (( 10#${NEW_PRIMARY_PORT} > 0 && 10#${NEW_PRIMARY_PORT} <= 65535 )) \
  || { echo "invalid new primary port" >&2; exit 2; }
NEW_PRIMARY_PORT=$((10#${NEW_PRIMARY_PORT}))
is_unit "${COMPOSE_UNIT}" || { echo "invalid compose unit" >&2; exit 2; }
is_container "${POSTGRES_CONTAINER}" || { echo "invalid postgres container" >&2; exit 2; }
is_container "${AUTOHEAL_CONTAINER}" || { echo "invalid autoheal container" >&2; exit 2; }
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
unit_text="$(systemctl cat "${COMPOSE_UNIT}")" || { echo "cannot inspect ${COMPOSE_UNIT}" >&2; exit 1; }
for condition in \
  'ConditionPathExists=!/var/lib/hololive-postgres-fence/fence.intent' \
  'ConditionPathExists=!/var/lib/hololive-postgres-fence/fenced'; do
  grep -Fqx -- "${condition}" <<<"${unit_text}" || {
    echo "${COMPOSE_UNIT} is missing persistent fence condition: ${condition}" >&2
    exit 1
  }
done

docker info >/dev/null 2>&1 || { echo "cannot verify Docker state" >&2; exit 1; }
container_is_absent() {
  local container="$1" names
  names="$(docker ps -a --filter "name=^/${container}$" --format '{{.Names}}')" || return 1
  ! grep -Fxq -- "${container}" <<<"${names}"
}
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

# Disable Docker-managed resurrection before the durable intent exists. A crash
# in this pre-intent window can reduce availability, but cannot lead the standby
# controller to promote because no fencing acknowledgement has been returned.
disable_container_restart "${AUTOHEAL_CONTAINER}" || exit 1
disable_container_restart "${POSTGRES_CONTAINER}" || exit 1

ack_token="${REQUEST_ID}"
if [[ -r "${FENCED_MARKER}" ]]; then
  existing_state="$(marker_value "${FENCED_MARKER}" state)"
  existing_host="$(marker_value "${FENCED_MARKER}" primary_host)"
  existing_token="$(marker_value "${FENCED_MARKER}" request_id)"
  existing_new_primary="$(marker_value "${FENCED_MARKER}" new_primary)"
  existing_fenced_at="$(marker_value "${FENCED_MARKER}" fenced_at)"
  if [[ "${existing_state}" != "fenced" || "${existing_host}" != "${EXPECTED_PRIMARY_HOST}" \
    || "${existing_new_primary}" != "${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" ]] \
    || ! is_token "${existing_token}" || [[ ! "${existing_fenced_at}" =~ ^[0-9]+$ ]]; then
    echo "existing fence marker is invalid" >&2
    exit 1
  fi
  ack_token="${existing_token}"
else
  atomic_write "${INTENT_MARKER}" <<EOF_INTENT
state=fencing
request_id=${REQUEST_ID}
primary_host=${EXPECTED_PRIMARY_HOST}
new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}
started_at=${NOW}
EOF_INTENT
fi

# The unit's ExecStop owns normal stack shutdown. Direct container actions remain
# mandatory because ExecStop can fail partway through a Compose teardown.
if ! systemctl stop "${COMPOSE_UNIT}"; then
  echo "warning: ${COMPOSE_UNIT} stop failed; applying direct database fence" >&2
fi

# Disable autoheal before stopping PostgreSQL. The repeated restart-policy check
# also covers a partial Compose shutdown that recreated either container.
durably_stop_container "${AUTOHEAL_CONTAINER}" 15 || exit 1
durably_stop_container "${POSTGRES_CONTAINER}" 60 || exit 1

if [[ ! -r "${FENCED_MARKER}" ]]; then
  atomic_write "${FENCED_MARKER}" <<EOF_FENCED
state=fenced
request_id=${REQUEST_ID}
primary_host=${EXPECTED_PRIMARY_HOST}
new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}
fenced_at=${NOW}
EOF_FENCED
fi
rm -f -- "${INTENT_MARKER}"
sync -f "${STATE_DIR}"
printf 'FENCED|%s|%s:%s|%s\n' \
  "${EXPECTED_PRIMARY_HOST}" "${NEW_PRIMARY_HOST}" "${NEW_PRIMARY_PORT}" "${ack_token}"
