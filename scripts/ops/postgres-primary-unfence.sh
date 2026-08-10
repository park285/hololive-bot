#!/usr/bin/env bash

set -euo pipefail

FENCE_TOKEN="${1:-}"
EXPECTED_PRIMARY_HOST="${2:-}"
CURRENT_PRIMARY_HOST="${3:-}"
CURRENT_PRIMARY_PORT="${4:-}"
STATE_DIR="${POSTGRES_PRIMARY_UNFENCE_STATE_DIR:-/var/lib/hololive-postgres-fence}"
POSTGRES_CONTAINER="${POSTGRES_PRIMARY_UNFENCE_CONTAINER:-holo-postgres}"
COMPOSE_UNIT="${POSTGRES_PRIMARY_UNFENCE_COMPOSE_UNIT:-hololive-compose.service}"
DB_NAME="${POSTGRES_PRIMARY_UNFENCE_DB_NAME:-hololive}"
DB_USER="${POSTGRES_PRIMARY_UNFENCE_DB_USER:-postgres_admin}"
PROBE_USER="${POSTGRES_PRIMARY_UNFENCE_PROBE_USER:-hololive_replicator}"
PGPASS_FILE="${POSTGRES_PRIMARY_UNFENCE_CONTAINER_PGPASS_FILE:-/run/hololive-bot/postgres/pgpass}"
CA_FILE="${POSTGRES_PRIMARY_UNFENCE_CONTAINER_CA_FILE:-/run/hololive-bot/certs/postgres-ca.pem}"
PROBE_TIMEOUT_SEC="${POSTGRES_PRIMARY_UNFENCE_PROBE_TIMEOUT_SEC:-5}"
ALLOW_TEST_STATE_DIR="${POSTGRES_PRIMARY_UNFENCE_ALLOW_TEST_STATE_DIR:-0}"
ALLOW_NON_ROOT_TEST="${POSTGRES_PRIMARY_UNFENCE_ALLOW_NON_ROOT_FOR_TEST:-0}"
FENCED_MARKER="${STATE_DIR}/fenced"
INTENT_MARKER="${STATE_DIR}/fence.intent"
LOCK_FILE="${STATE_DIR}/transition.lock"

is_token() { [[ "$1" =~ ^[A-Za-z0-9._:-]{8,128}$ ]]; }
is_host() { [[ "$1" =~ ^[A-Za-z0-9._:-]+$ ]]; }
is_name() { [[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]]; }

CURRENT_UID="$(/usr/bin/id -u)"
[[ "${ALLOW_NON_ROOT_TEST}" == "0" || "${ALLOW_NON_ROOT_TEST}" == "1" ]] || { printf 'invalid non-root test flag\n' >&2; exit 2; }
if [[ "${ALLOW_NON_ROOT_TEST}" == "1" && ( "${CURRENT_UID}" == "0" || "${STATE_DIR}" != /tmp/* ) ]]; then
  printf 'non-root test mode requires a non-root uid and /tmp state\n' >&2
  exit 2
fi
[[ "${CURRENT_UID}" == "0" || "${ALLOW_NON_ROOT_TEST}" == "1" ]] || { printf 'primary unfence requires root\n' >&2; exit 1; }
is_token "${FENCE_TOKEN}" || { printf 'invalid fence token\n' >&2; exit 2; }
is_host "${EXPECTED_PRIMARY_HOST}" || { printf 'invalid expected host\n' >&2; exit 2; }
is_host "${CURRENT_PRIMARY_HOST}" || { printf 'invalid current primary host\n' >&2; exit 2; }
[[ "${CURRENT_PRIMARY_PORT}" =~ ^[0-9]+$ && ${#CURRENT_PRIMARY_PORT} -le 5 ]] \
  && (( 10#${CURRENT_PRIMARY_PORT} > 0 && 10#${CURRENT_PRIMARY_PORT} <= 65535 )) \
  || { printf 'invalid current primary port\n' >&2; exit 2; }
CURRENT_PRIMARY_PORT=$((10#${CURRENT_PRIMARY_PORT}))
is_name "${POSTGRES_CONTAINER}" || { printf 'invalid postgres container\n' >&2; exit 2; }
is_name "${DB_NAME}" || { printf 'invalid database name\n' >&2; exit 2; }
is_name "${DB_USER}" || { printf 'invalid database user\n' >&2; exit 2; }
is_name "${PROBE_USER}" || { printf 'invalid probe user\n' >&2; exit 2; }
[[ "${COMPOSE_UNIT}" =~ ^[A-Za-z0-9_.@:-]+\.service$ ]] || { printf 'invalid compose unit\n' >&2; exit 2; }
[[ "${PROBE_TIMEOUT_SEC}" =~ ^[1-9][0-9]*$ && ${#PROBE_TIMEOUT_SEC} -le 3 ]] || { printf 'invalid probe timeout\n' >&2; exit 2; }
[[ "${ALLOW_TEST_STATE_DIR}" == "0" || "${ALLOW_TEST_STATE_DIR}" == "1" ]] || { printf 'invalid test state flag\n' >&2; exit 2; }
if [[ "${ALLOW_TEST_STATE_DIR}" == "0" ]]; then
  PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
  export PATH
fi
[[ "${STATE_DIR}" == "/var/lib/hololive-postgres-fence" || ( "${ALLOW_TEST_STATE_DIR}" == "1" && "${STATE_DIR}" == /tmp/* ) ]] || {
  printf 'refusing noncanonical fence state directory\n' >&2
  exit 2
}

marker_value() {
  local file="$1" key="$2"
  awk -F= -v key="${key}" '$1 == key {sub(/^[^=]*=/, ""); print; exit}' "${file}" 2>/dev/null || true
}

[[ -d "${STATE_DIR}" && ! -L "${STATE_DIR}" ]] || { printf 'fence state directory is missing\n' >&2; exit 1; }
state_real="$(realpath -e -- "${STATE_DIR}")"
state_owner="$(stat -c '%u' -- "${STATE_DIR}")"
state_mode_hex="$(stat -c '%f' -- "${STATE_DIR}")"
[[ "${state_real}" == "${STATE_DIR}" ]] || { printf 'fence state directory must be canonical\n' >&2; exit 1; }
[[ "${state_owner}" == "0" || ( "${ALLOW_NON_ROOT_TEST}" == "1" && "${state_owner}" == "${CURRENT_UID}" ) ]] || { printf 'fence state directory has an invalid owner\n' >&2; exit 1; }
(( (0x${state_mode_hex} & 0x0012) == 0 )) || { printf 'fence state directory is writable by group or other\n' >&2; exit 1; }

exec 9>"${LOCK_FILE}"
flock -x 9

[[ -r "${FENCED_MARKER}" && ! -L "${FENCED_MARKER}" ]] || { printf 'durable fence marker is missing\n' >&2; exit 1; }
[[ ! -e "${INTENT_MARKER}" ]] || { printf 'incomplete fence intent is still present\n' >&2; exit 1; }
marker_state="$(marker_value "${FENCED_MARKER}" state)"
marker_host="$(marker_value "${FENCED_MARKER}" primary_host)"
marker_primary="$(marker_value "${FENCED_MARKER}" new_primary)"
marker_token="$(marker_value "${FENCED_MARKER}" fence_token)"
[[ -n "${marker_token}" ]] || marker_token="$(marker_value "${FENCED_MARKER}" request_id)"
[[ "${marker_state}" == "fenced" \
  && "${marker_host}" == "${EXPECTED_PRIMARY_HOST}" \
  && "${marker_primary}" == "${CURRENT_PRIMARY_HOST}:${CURRENT_PRIMARY_PORT}" \
  && "${marker_token}" == "${FENCE_TOKEN}" ]] || { printf 'fence marker does not match this generation\n' >&2; exit 1; }

ip -o addr show scope global | awk '{print $4}' | cut -d/ -f1 | grep -Fxq -- "${EXPECTED_PRIMARY_HOST}" || {
  printf 'expected old-primary address is not configured on this host\n' >&2
  exit 1
}
if systemctl is-active --quiet "${COMPOSE_UNIT}"; then
  printf 'compose unit must remain stopped while unfencing\n' >&2
  exit 1
fi

running="$(docker inspect -f '{{.State.Running}}' "${POSTGRES_CONTAINER}" 2>/dev/null || printf 'unknown')"
restart_policy="$(docker inspect -f '{{.HostConfig.RestartPolicy.Name}}' "${POSTGRES_CONTAINER}" 2>/dev/null || printf 'unknown')"
[[ "${running}" == "true" && "${restart_policy}" == "no" ]] || {
  printf 'reseeded standby must be running with restart policy no\n' >&2
  exit 1
}

local_status() {
  /usr/bin/timeout --foreground --kill-after=2 "$((PROBE_TIMEOUT_SEC + 2))s" \
    docker exec "${POSTGRES_CONTAINER}" psql -X -v ON_ERROR_STOP=1 -AtF '|' \
    -U "${DB_USER}" -d "${DB_NAME}" \
    -c "SELECT pg_is_in_recovery(), current_setting('transaction_read_only'), pg_is_wal_replay_paused(), COALESCE((SELECT status FROM pg_stat_wal_receiver LIMIT 1),''), COALESCE((SELECT sender_host FROM pg_stat_wal_receiver LIMIT 1),''), COALESCE((SELECT sender_port::text FROM pg_stat_wal_receiver LIMIT 1),'')"
}

verify_local_standby() {
  local raw recovery read_only replay_paused receiver_status sender_host sender_port extra
  raw="$(local_status)" || return 1
  [[ "${raw}" != *$'\n'* ]] || return 1
  IFS='|' read -r recovery read_only replay_paused receiver_status sender_host sender_port extra <<<"${raw}"
  [[ -z "${extra:-}" \
    && "${recovery}" == "t" \
    && "${read_only}" == "on" \
    && "${replay_paused}" == "f" \
    && "${receiver_status}" == "streaming" \
    && "${sender_host}" == "${CURRENT_PRIMARY_HOST}" \
    && "${sender_port}" == "${CURRENT_PRIMARY_PORT}" ]]
}

verify_local_standby || { printf 'local database is not a streaming read-only standby of the current primary\n' >&2; exit 1; }
remote_status="$(PGPASSFILE="${PGPASS_FILE}" PGSSLMODE=verify-full PGSSLROOTCERT="${CA_FILE}" \
  PGCONNECT_TIMEOUT="${PROBE_TIMEOUT_SEC}" /usr/bin/timeout --foreground --kill-after=2 "$((PROBE_TIMEOUT_SEC + 2))s" \
  docker exec -e "PGPASSFILE=${PGPASS_FILE}" -e PGSSLMODE=verify-full -e "PGSSLROOTCERT=${CA_FILE}" \
  -e "PGCONNECT_TIMEOUT=${PROBE_TIMEOUT_SEC}" "${POSTGRES_CONTAINER}" psql -X -v ON_ERROR_STOP=1 -AtF '|' \
  -h "${CURRENT_PRIMARY_HOST}" -p "${CURRENT_PRIMARY_PORT}" -U "${PROBE_USER}" -d "${DB_NAME}" \
  -c "SELECT pg_is_in_recovery(), current_setting('transaction_read_only')")" || {
    printf 'current primary probe failed\n' >&2
    exit 1
  }
[[ "${remote_status}" == "f|off" ]] || { printf 'current endpoint is not a read/write primary\n' >&2; exit 1; }
verify_local_standby || { printf 'local standby changed state during verification\n' >&2; exit 1; }

rm -f -- "${FENCED_MARKER}"
sync -f "${STATE_DIR}"
printf 'UNFENCED|%s|%s:%s|%s\n' "${EXPECTED_PRIMARY_HOST}" "${CURRENT_PRIMARY_HOST}" "${CURRENT_PRIMARY_PORT}" "${FENCE_TOKEN}"
