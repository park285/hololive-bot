#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
POSTGRES_IMAGE="postgres:18.6-alpine@sha256:d3e1620b530c944afa6e887d22eb899824da68e19c52024bf98f5220c88a65b2"

# shellcheck source=scripts/ops/integration/lib/postgres-failover-integration-lib.sh
source "${SCRIPT_DIR}/lib/postgres-failover-integration-lib.sh"

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  printf '%s\n' "Usage: HOLOLIVE_POSTGRES_FAILOVER_INTEGRATION=1 $0"
  printf '%s\n' 'Runs an isolated PostgreSQL 18.4 physical-replication failover test.'
  exit 0
fi
if [[ "${HOLOLIVE_POSTGRES_FAILOVER_INTEGRATION:-0}" != "1" ]]; then
  printf '%s\n' 'refusing to run: set HOLOLIVE_POSTGRES_FAILOVER_INTEGRATION=1' >&2
  exit 2
fi
while IFS='=' read -r name _; do
  [[ "${name}" == POSTGRES_FAILOVER_* ]] || continue
  printf 'refusing inherited production failover variable: %s\n' "${name}" >&2
  exit 2
done < <(/usr/bin/env)
unset PGSERVICE PGHOST PGPORT PGUSER PGDATABASE PGSSLMODE PGSSLROOTCERT PGPASSFILE
if [[ -n "${DOCKER_HOST:-}" || -n "${DOCKER_CONTEXT:-}" || -n "${DOCKER_CONFIG:-}" ]]; then
  printf '%s\n' 'refusing Docker environment overrides; the harness requires the local default Docker context' >&2
  exit 2
fi
unset DOCKER_HOST DOCKER_CONTEXT DOCKER_CONFIG

DOCKER_PATH="$(command -v docker || true)"
if [[ -z "${DOCKER_PATH}" || ! -x "${DOCKER_PATH}" ]]; then
  printf '%s\n' '[SKIP] Docker is unavailable' >&2
  exit 77
fi
DOCKER_PATH="$(realpath -e -- "${DOCKER_PATH}")"
[[ "$("${DOCKER_PATH}" context show 2>/dev/null || true)" == default ]] || {
  printf '%s\n' 'refusing a non-default Docker context' >&2
  exit 2
}
docker_endpoint="$("${DOCKER_PATH}" context inspect default --format '{{(index .Endpoints "docker").Host}}' 2>/dev/null || true)"
[[ "${docker_endpoint}" == unix://* ]] || {
  printf '%s\n' 'refusing a non-local Docker endpoint' >&2
  exit 2
}
OPENSSL_PATH="$(command -v openssl || true)"
if [[ -z "${OPENSSL_PATH}" || ! -x "${OPENSSL_PATH}" ]]; then
  printf '%s\n' '[SKIP] OpenSSL is unavailable' >&2
  exit 77
fi
OPENSSL_PATH="$(realpath -e -- "${OPENSSL_PATH}")"
"${DOCKER_PATH}" info >/dev/null 2>&1 || { printf '%s\n' '[SKIP] Docker daemon is unavailable' >&2; exit 77; }

RUN_ID="$(date +%s)-${BASHPID}-${RANDOM}"
BASE_NAME="hololive-pg-failover-it-${RUN_ID}"
TMP_DIR="$(mktemp -d "/tmp/${BASE_NAME}.XXXXXX")"
chmod 0700 "${TMP_DIR}"
PSQL_PATH="${TMP_DIR}/psql"
cat >"${PSQL_PATH}" <<'EOF_PSQL_CLIENT'
#!/usr/bin/env bash
set -euo pipefail
: "${HOLOLIVE_FAILOVER_IT_DOCKER_PATH:?}"
: "${HOLOLIVE_FAILOVER_IT_POSTGRES_IMAGE:?}"
: "${PGPASSFILE:?}"
: "${PGSSLROOTCERT:?}"
exec "${HOLOLIVE_FAILOVER_IT_DOCKER_PATH}" run --rm --network "${HOLOLIVE_FAILOVER_IT_NETWORK:?}" \
  --user "$(/usr/bin/id -u):$(/usr/bin/id -g)" \
  --volume "${PGPASSFILE}:/run/hololive-failover/pgpass:ro" \
  --volume "${PGSSLROOTCERT}:/run/hololive-failover/ca.pem:ro" \
  --env PGPASSFILE=/run/hololive-failover/pgpass \
  --env PGSSLROOTCERT=/run/hololive-failover/ca.pem \
  --env "PGSSLMODE=${PGSSLMODE:-verify-full}" \
  --env "PGCONNECT_TIMEOUT=${PGCONNECT_TIMEOUT:-2}" \
  --env "PGOPTIONS=${PGOPTIONS:-}" \
  --entrypoint /usr/local/bin/psql \
  "${HOLOLIVE_FAILOVER_IT_POSTGRES_IMAGE}" "$@"
EOF_PSQL_CLIENT
chmod 0700 "${PSQL_PATH}"
export HOLOLIVE_FAILOVER_IT_DOCKER_PATH="${DOCKER_PATH}"
export HOLOLIVE_FAILOVER_IT_POSTGRES_IMAGE="${POSTGRES_IMAGE}"
NETWORK_NAME="${BASE_NAME}-net"
export HOLOLIVE_FAILOVER_IT_NETWORK="${NETWORK_NAME}"
PRIMARY_CONTAINER="${BASE_NAME}-primary"
STANDBY_CONTAINER="${BASE_NAME}-standby"
PRIMARY_VOLUME="${BASE_NAME}-primary-data"
STANDBY_VOLUME="${BASE_NAME}-standby-data"
PRIMARY_TLS_VOLUME="${BASE_NAME}-primary-tls"
STANDBY_TLS_VOLUME="${BASE_NAME}-standby-tls"
LABEL_KEY='com.hololive.postgres-failover-integration'
LABEL_VALUE="${RUN_ID}"
NETWORK_CREATED=0
PRIMARY_VOLUME_CREATED=0
STANDBY_VOLUME_CREATED=0
PRIMARY_TLS_VOLUME_CREATED=0
STANDBY_TLS_VOLUME_CREATED=0
PRIMARY_CONTAINER_CREATED=0
STANDBY_CONTAINER_CREATED=0
trap cleanup EXIT
trap 'exit 130' INT TERM


start_primary() {
  refuse_existing container "${PRIMARY_CONTAINER}"
  "${DOCKER_PATH}" run --detach --user root --name "${PRIMARY_CONTAINER}" --restart=no \
    --label "${LABEL_KEY}=${LABEL_VALUE}" --network "${NETWORK_NAME}" --network-alias primary-db \
    --shm-size 128m \
    --env POSTGRES_DB="${DB_NAME}" --env POSTGRES_USER="${ADMIN_USER}" \
    --env POSTGRES_PASSWORD="${ADMIN_PASSWORD}" --env PGDATA=/var/lib/postgresql/pgdata \
    --env POSTGRES_INITDB_ARGS="--locale-provider=builtin --builtin-locale=C.UTF-8 --encoding=UTF8" \
    -v "${PRIMARY_VOLUME}:/var/lib/postgresql" \
    -v "${TMP_DIR}/postgres/pg_hba.conf:/etc/postgres-config/pg_hba.conf:ro" \
    -v "${TMP_DIR}/init:/docker-entrypoint-initdb.d:ro" -v "${PRIMARY_TLS_VOLUME}:/etc/postgres-tls:ro" \
    "${POSTGRES_IMAGE}" postgres -c wal_level=replica -c max_wal_senders=10 \
    -c max_replication_slots=10 -c hba_file=/etc/postgres-config/pg_hba.conf \
    -c ssl=on -c ssl_cert_file=/etc/postgres-tls/server.crt -c ssl_key_file=/etc/postgres-tls/server.key >/dev/null
  PRIMARY_CONTAINER_CREATED=1
}
prepare_standby() {
  "${DOCKER_PATH}" run --rm --user 0 -v "${STANDBY_VOLUME}:/var/lib/postgresql" \
    -v "${PGPASS_FILE}:/input/pgpass:ro" "${POSTGRES_IMAGE}" sh -ec \
    'mkdir -p /var/lib/postgresql/pgdata; cp /input/pgpass /var/lib/postgresql/pgpass-basebackup; chown -R postgres:postgres /var/lib/postgresql; chmod 0600 /var/lib/postgresql/pgpass-basebackup'
  "${DOCKER_PATH}" run --rm --user postgres --network "${NETWORK_NAME}" \
    -v "${STANDBY_VOLUME}:/var/lib/postgresql" -v "${PRIMARY_TLS_VOLUME}:/etc/postgres-tls:ro" \
    --env PGDATA=/var/lib/postgresql/pgdata --env PGPASSFILE=/var/lib/postgresql/pgpass-basebackup \
    --env PGSSLMODE=verify-full --env PGSSLROOTCERT=/etc/postgres-tls/ca.crt \
    "${POSTGRES_IMAGE}" pg_basebackup -h primary-db -p 5432 -U "${REPLICATOR_USER}" \
    -D /var/lib/postgresql/pgdata -X stream -S "${REPLICATION_SLOT}" -R -P -v \
    >"${TMP_DIR}/pg_basebackup.log" 2>&1
  "${DOCKER_PATH}" run --rm --user 0 -v "${STANDBY_VOLUME}:/var/lib/postgresql" \
    -v "${PGPASS_FILE}:/input/pgpass:ro" -v "${TMP_DIR}/standby.auto.conf:/input/standby.auto.conf:ro" \
    "${POSTGRES_IMAGE}" sh -ec \
    'test -f /var/lib/postgresql/pgdata/standby.signal; cp /input/pgpass /var/lib/postgresql/pgdata/pgpass; cp /input/standby.auto.conf /var/lib/postgresql/pgdata/postgresql.auto.conf; rm -f /var/lib/postgresql/pgpass-basebackup; chmod 0600 /var/lib/postgresql/pgdata/pgpass; chown postgres:postgres /var/lib/postgresql/pgdata/pgpass /var/lib/postgresql/pgdata/postgresql.auto.conf /var/lib/postgresql/pgdata/standby.signal'
}
start_standby() {
  refuse_existing container "${STANDBY_CONTAINER}"
  "${DOCKER_PATH}" run --detach --user root --name "${STANDBY_CONTAINER}" --restart=no \
    --label "${LABEL_KEY}=${LABEL_VALUE}" --network "${NETWORK_NAME}" --network-alias standby-db \
    --shm-size 128m --env PGDATA=/var/lib/postgresql/pgdata \
    -v "${STANDBY_VOLUME}:/var/lib/postgresql" \
    -v "${TMP_DIR}/postgres/pg_hba.conf:/etc/postgres-config/pg_hba.conf:ro" \
    -v "${STANDBY_TLS_VOLUME}:/etc/postgres-tls:ro" "${POSTGRES_IMAGE}" postgres \
    -c hba_file=/etc/postgres-config/pg_hba.conf -c hot_standby=on -c ssl=on \
    -c ssl_cert_file=/etc/postgres-tls/server.crt -c ssl_key_file=/etc/postgres-tls/server.key >/dev/null
  STANDBY_CONTAINER_CREATED=1
}

write_hooks() {
  mkdir -p "${TMP_DIR}/hooks" && chmod 0700 "${TMP_DIR}/hooks"
  FENCE_HOOK="${TMP_DIR}/hooks/fence.sh"
  cat >"${FENCE_HOOK}" <<'EOF_FENCE'
#!/usr/bin/env bash
set -euo pipefail
: "${POSTGRES_FAILOVER_REQUEST_ID:?}"
: "${POSTGRES_FAILOVER_PRIMARY_HOST:?}"
: "${POSTGRES_FAILOVER_NEW_PRIMARY_HOST:?}"
: "${POSTGRES_FAILOVER_NEW_PRIMARY_PORT:?}"
: "${HARNESS_DOCKER:?}"
: "${HARNESS_PRIMARY_CONTAINER:?}"
: "${HARNESS_FENCE_MARKER:?}"
: "${HARNESS_FENCE_LABEL:?}"
[[ "${HARNESS_PRIMARY_CONTAINER}" == hololive-pg-failover-it-*-primary ]] || exit 1
label="$("${HARNESS_DOCKER}" inspect --format '{{index .Config.Labels "com.hololive.postgres-failover-integration"}}' "${HARNESS_PRIMARY_CONTAINER}")"
[[ "${label}" == "${HARNESS_FENCE_LABEL}" ]] || exit 1
fence_token=''
if [[ -r "${HARNESS_FENCE_MARKER}" ]]; then
  [[ "$(awk -F= '$1 == "state" {sub(/^[^=]*=/, ""); print; exit}' "${HARNESS_FENCE_MARKER}")" == fenced ]] || exit 1
  [[ "$(awk -F= '$1 == "primary_host" {sub(/^[^=]*=/, ""); print; exit}' "${HARNESS_FENCE_MARKER}")" == "${POSTGRES_FAILOVER_PRIMARY_HOST}" ]] || exit 1
  [[ "$(awk -F= '$1 == "new_primary" {sub(/^[^=]*=/, ""); print; exit}' "${HARNESS_FENCE_MARKER}")" == "${POSTGRES_FAILOVER_NEW_PRIMARY_HOST}:${POSTGRES_FAILOVER_NEW_PRIMARY_PORT}" ]] || exit 1
  fence_token="$(awk -F= '$1 == "fence_token" {sub(/^[^=]*=/, ""); print; exit}' "${HARNESS_FENCE_MARKER}")"
fi
if [[ -z "${fence_token}" ]]; then
  fence_token="fence-${POSTGRES_FAILOVER_NEW_PRIMARY_PORT}-${RANDOM}-${RANDOM}"
fi
paused="$("${HARNESS_DOCKER}" inspect --format '{{.State.Paused}}' "${HARNESS_PRIMARY_CONTAINER}")"
[[ "${paused}" != true ]] || "${HARNESS_DOCKER}" unpause "${HARNESS_PRIMARY_CONTAINER}" >/dev/null
running="$("${HARNESS_DOCKER}" inspect --format '{{.State.Running}}' "${HARNESS_PRIMARY_CONTAINER}")"
if [[ "${running}" == true ]]; then
  "${HARNESS_DOCKER}" update --restart=no "${HARNESS_PRIMARY_CONTAINER}" >/dev/null
  "${HARNESS_DOCKER}" stop --time=5 "${HARNESS_PRIMARY_CONTAINER}" >/dev/null
fi
running="$("${HARNESS_DOCKER}" inspect --format '{{.State.Running}}' "${HARNESS_PRIMARY_CONTAINER}")"
[[ "${running}" == false ]] || exit 1
umask 077
tmp="${HARNESS_FENCE_MARKER}.tmp.$$"
printf 'state=fenced\nprimary_host=%s\nnew_primary=%s:%s\nfence_token=%s\n' \
  "${POSTGRES_FAILOVER_PRIMARY_HOST}" "${POSTGRES_FAILOVER_NEW_PRIMARY_HOST}" \
  "${POSTGRES_FAILOVER_NEW_PRIMARY_PORT}" "${fence_token}" >"${tmp}"
chmod 0600 "${tmp}"
sync -f "${tmp}"
mv -f -- "${tmp}" "${HARNESS_FENCE_MARKER}"
sync -f "$(dirname -- "${HARNESS_FENCE_MARKER}")"
printf 'FENCED|%s|%s:%s|%s|%s\n' "${POSTGRES_FAILOVER_PRIMARY_HOST}" \
  "${POSTGRES_FAILOVER_NEW_PRIMARY_HOST}" "${POSTGRES_FAILOVER_NEW_PRIMARY_PORT}" \
  "${POSTGRES_FAILOVER_REQUEST_ID}" "${fence_token}"
EOF_FENCE
  chmod 0700 "${FENCE_HOOK}"
  ROUTE_HOOK="${TMP_DIR}/hooks/route.sh"
  cat >"${ROUTE_HOOK}" <<'EOF_ROUTE'
#!/usr/bin/env bash
set -euo pipefail
: "${POSTGRES_FAILOVER_NEW_PRIMARY_HOST:?}"
: "${POSTGRES_FAILOVER_NEW_PRIMARY_PORT:?}"
: "${POSTGRES_FAILOVER_FENCE_TOKEN:?}"
: "${HARNESS_PSQL:?}"
: "${HARNESS_PGPASS:?}"
: "${HARNESS_CA:?}"
: "${HARNESS_DB:?}"
: "${HARNESS_USER:?}"
: "${HARNESS_ROUTE_MARKER:?}"
[[ "${POSTGRES_FAILOVER_NEW_PRIMARY_HOST}" == standby-db ]] || exit 1
[[ "${POSTGRES_FAILOVER_NEW_PRIMARY_PORT}" =~ ^[0-9]+$ ]] || exit 1
probe="$(PGPASSFILE="${HARNESS_PGPASS}" PGSSLMODE=verify-full PGSSLROOTCERT="${HARNESS_CA}" \
  PGCONNECT_TIMEOUT=2 PGOPTIONS='-c statement_timeout=2000' \
  /usr/bin/timeout --foreground --kill-after=2 6s "${HARNESS_PSQL}" \
  -X -v ON_ERROR_STOP=1 -AtF '|' -h "${POSTGRES_FAILOVER_NEW_PRIMARY_HOST}" \
  -p "${POSTGRES_FAILOVER_NEW_PRIMARY_PORT}" -U "${HARNESS_USER}" -d "${HARNESS_DB}" \
  -c "SELECT pg_is_in_recovery(), current_setting('transaction_read_only')")"
[[ "${probe}" == 'f|off' ]] || exit 1
count=0
if [[ -r "${HARNESS_ROUTE_MARKER}" ]]; then
  [[ "$(awk -F= '$1 == "state" {sub(/^[^=]*=/, ""); print; exit}' "${HARNESS_ROUTE_MARKER}")" == complete ]] || exit 1
  [[ "$(awk -F= '$1 == "endpoint" {sub(/^[^=]*=/, ""); print; exit}' "${HARNESS_ROUTE_MARKER}")" == "${POSTGRES_FAILOVER_NEW_PRIMARY_HOST}:${POSTGRES_FAILOVER_NEW_PRIMARY_PORT}" ]] || exit 1
  [[ "$(awk -F= '$1 == "fence_token" {sub(/^[^=]*=/, ""); print; exit}' "${HARNESS_ROUTE_MARKER}")" == "${POSTGRES_FAILOVER_FENCE_TOKEN}" ]] || exit 1
  count="$(awk -F= '$1 == "invocation_count" {sub(/^[^=]*=/, ""); print; exit}' "${HARNESS_ROUTE_MARKER}")"
  [[ "${count}" =~ ^[0-9]+$ ]] || exit 1
fi
count=$((count + 1))
umask 077
tmp="${HARNESS_ROUTE_MARKER}.tmp.$$"
printf 'state=complete\nendpoint=%s:%s\nfence_token=%s\ninvocation_count=%s\n' \
  "${POSTGRES_FAILOVER_NEW_PRIMARY_HOST}" "${POSTGRES_FAILOVER_NEW_PRIMARY_PORT}" \
  "${POSTGRES_FAILOVER_FENCE_TOKEN}" "${count}" >"${tmp}"
chmod 0600 "${tmp}"
sync -f "${tmp}"
mv -f -- "${tmp}" "${HARNESS_ROUTE_MARKER}"
sync -f "$(dirname -- "${HARNESS_ROUTE_MARKER}")"
printf 'ROUTED|%s:%s|%s\n' "${POSTGRES_FAILOVER_NEW_PRIMARY_HOST}" \
  "${POSTGRES_FAILOVER_NEW_PRIMARY_PORT}" "${POSTGRES_FAILOVER_FENCE_TOKEN}"
EOF_ROUTE
  chmod 0700 "${ROUTE_HOOK}"
}
run_controller() {
  local now="${1}" out="${2}" err="${3}"
  env PATH=/usr/bin:/bin \
    POSTGRES_FAILOVER_ALLOW_NON_ROOT_FOR_TEST=1 POSTGRES_FAILOVER_SERVICE_USER="$(id -un)" \
    POSTGRES_FAILOVER_PSQL_PATH="${PSQL_PATH}" POSTGRES_FAILOVER_PGPASS_FILE="${PGPASS_FILE}" \
    POSTGRES_FAILOVER_CA_FILE="${CA_FILE}" POSTGRES_FAILOVER_RUNTIME_DIR="${RUNTIME_DIR}" \
    POSTGRES_FAILOVER_STATE_DIR="${STATE_DIR}" POSTGRES_FAILOVER_PRIMARY_HOST=primary-db \
    POSTGRES_FAILOVER_PRIMARY_PORT="${PRIMARY_PORT}" POSTGRES_FAILOVER_NEW_PRIMARY_HOST=standby-db \
    POSTGRES_FAILOVER_NEW_PRIMARY_PORT="${STANDBY_PORT}" POSTGRES_FAILOVER_LOCAL_HOST=standby-db \
    POSTGRES_FAILOVER_LOCAL_PORT="${STANDBY_PORT}" POSTGRES_FAILOVER_DB_NAME="${DB_NAME}" \
    POSTGRES_FAILOVER_PROBE_USER="${REPLICATOR_USER}" POSTGRES_FAILOVER_FAILURE_THRESHOLD=1 \
    POSTGRES_FAILOVER_MIN_OUTAGE_SEC=0 POSTGRES_FAILOVER_MAX_LAST_HEALTHY_AGE_SEC=120 \
    POSTGRES_FAILOVER_MAX_KNOWN_LAG_BYTES=0 POSTGRES_FAILOVER_PROBE_TIMEOUT_SEC=2 \
    POSTGRES_FAILOVER_PROMOTE_TIMEOUT_SEC=30 POSTGRES_FAILOVER_FENCE_HOOK_TIMEOUT_SEC=30 \
    POSTGRES_FAILOVER_ROUTE_HOOK_TIMEOUT_SEC=30 POSTGRES_FAILOVER_REQUIRE_ROUTE_HOOK=1 \
    POSTGRES_FAILOVER_FENCE_COMMAND="${FENCE_HOOK}" POSTGRES_FAILOVER_ROUTE_COMMAND="${ROUTE_HOOK}" \
    POSTGRES_FAILOVER_NOW="${now}" POSTGRES_FAILOVER_POST_FENCE_NOW="${now}" \
    HARNESS_DOCKER="${DOCKER_PATH}" HARNESS_PRIMARY_CONTAINER="${PRIMARY_CONTAINER}" \
    HARNESS_FENCE_MARKER="${FENCE_MARKER}" HARNESS_FENCE_LABEL="${LABEL_VALUE}" \
    HARNESS_PSQL="${PSQL_PATH}" HARNESS_PGPASS="${PGPASS_FILE}" HARNESS_CA="${CA_FILE}" \
    HARNESS_DB="${DB_NAME}" HARNESS_USER="${REPLICATOR_USER}" HARNESS_ROUTE_MARKER="${ROUTE_MARKER}" \
    /usr/bin/env bash "${CONTROLLER}" --apply >"${out}" 2>"${err}"
}

ADMIN_USER=postgres_admin
REPLICATOR_USER=hololive_replicator
DB_NAME=hololive
REPLICATION_SLOT=failover_slot
ADMIN_PASSWORD="$("${OPENSSL_PATH}" rand -hex 24)"
REPLICATOR_PASSWORD="$("${OPENSSL_PATH}" rand -hex 24)"
PRIMARY_PORT=5432
STANDBY_PORT=5432
CA_FILE="${TMP_DIR}/certs/ca.crt"
PGPASS_FILE="${TMP_DIR}/pgpass"
STATE_DIR="${TMP_DIR}/state"
RUNTIME_DIR="${TMP_DIR}/runtime"
FENCE_MARKER="${STATE_DIR}/fenced"
ROUTE_MARKER="${STATE_DIR}/route"
mkdir -p "${TMP_DIR}/certs" "${STATE_DIR}" "${RUNTIME_DIR}"
chmod 0700 "${TMP_DIR}/certs" "${STATE_DIR}" "${RUNTIME_DIR}"
"${OPENSSL_PATH}" req -x509 -newkey rsa:2048 -nodes -keyout "${TMP_DIR}/certs/ca.key" \
  -out "${CA_FILE}" -days 1 -subj '/CN=hololive-failover-integration-ca' >/dev/null 2>&1
chmod 0600 "${TMP_DIR}/certs/ca.key"
chmod 0644 "${CA_FILE}"
write_tls_material primary
write_tls_material standby
printf 'primary-db:5432:*:%s:%s\nstandby-db:5432:*:%s:%s\n' \
  "${REPLICATOR_USER}" "${REPLICATOR_PASSWORD}" "${REPLICATOR_USER}" "${REPLICATOR_PASSWORD}" >"${PGPASS_FILE}"
chmod 0600 "${PGPASS_FILE}"
write_cluster_files
write_hooks
mkdir -p "${TMP_DIR}/controller"
cp --parents "${ROOT_DIR}/scripts/ops/postgres-failover.sh" \
  "${ROOT_DIR}/scripts/ops/lib/postgres-failover-lib.sh" \
  "${ROOT_DIR}/scripts/ops/lib/postgres-failover-transition-lib.sh" "${TMP_DIR}/controller"
chmod -R go-w "${TMP_DIR}/controller"
CONTROLLER="${TMP_DIR}/controller${ROOT_DIR}/scripts/ops/postgres-failover.sh"

create_network
create_volume "${PRIMARY_VOLUME}"; PRIMARY_VOLUME_CREATED=1
create_volume "${STANDBY_VOLUME}"; STANDBY_VOLUME_CREATED=1
create_volume "${PRIMARY_TLS_VOLUME}"; PRIMARY_TLS_VOLUME_CREATED=1
create_volume "${STANDBY_TLS_VOLUME}"; STANDBY_TLS_VOLUME_CREATED=1
stage_tls_volume "${PRIMARY_TLS_VOLUME}" primary
stage_tls_volume "${STANDBY_TLS_VOLUME}" standby
start_primary
wait_for_psql primary-db "${PRIMARY_PORT}" "${PRIMARY_CONTAINER}"
slot_state="$(run_psql primary-db "${PRIMARY_PORT}" \
  "SELECT slot_type || '|' || active::text FROM pg_replication_slots WHERE slot_name = '${REPLICATION_SLOT}'")"
assert_equal 'physical|false' "${slot_state}" 'primary physical slot'
pass 'primary created a physical replication slot'
prepare_standby
pass 'physical pg_basebackup completed with the replication slot'
start_standby
wait_for_psql standby-db "${STANDBY_PORT}" "${STANDBY_CONTAINER}"
wait_for_streaming
role_state="$(run_psql primary-db "${PRIMARY_PORT}" \
  "SELECT rolsuper::text || '|' || rolcreaterole::text || '|' || rolcreatedb::text || '|' || rolreplication::text || '|' || has_database_privilege(current_user, '${DB_NAME}', 'CONNECT')::text || '|' || has_function_privilege(current_user, 'pg_catalog.pg_promote(boolean, integer)', 'EXECUTE')::text || '|' || has_schema_privilege(current_user, 'public', 'CREATE')::text FROM pg_roles WHERE rolname = current_user")"
assert_equal 'false|false|false|true|true|true|false' "${role_state}" 'least-privilege promotion role'
pass 'least-privilege promotion role is usable for probes and pg_promote'

NOW="$(date +%s)"
run_controller "${NOW}" "${TMP_DIR}/controller-first.out" "${TMP_DIR}/controller-first.err" || {
  sed -n '1,160p' "${TMP_DIR}/controller-first.err" >&2
  fail 'controller did not record a healthy primary observation'
}
state_file="${STATE_DIR}/state.tsv"
[[ -r "${state_file}" ]] || fail 'controller did not write state.tsv'
last_healthy="$(awk -F '\t' '{print $4}' "${state_file}")"
[[ "${last_healthy}" =~ ^[0-9]+$ && "${last_healthy}" != 0 ]] || fail 'controller did not record a fresh healthy observation'
pass 'real host controller recorded a zero-known-lag healthy observation'

"${DOCKER_PATH}" pause "${PRIMARY_CONTAINER}" >/dev/null
sleep 1
FAILOVER_NOW=$((NOW + 1))
if ! run_controller "${FAILOVER_NOW}" "${TMP_DIR}/controller-failover.out" "${TMP_DIR}/controller-failover.err"; then
  sed -n '1,240p' "${TMP_DIR}/controller-failover.err" >&2
  fail 'controller did not complete fenced promotion and route'
fi

primary_running="$("${DOCKER_PATH}" inspect --format '{{.State.Running}}' "${PRIMARY_CONTAINER}")"
primary_restarting="$("${DOCKER_PATH}" inspect --format '{{.State.Restarting}}' "${PRIMARY_CONTAINER}")"
assert_equal false "${primary_running}" 'old primary stopped by fence hook'
assert_equal false "${primary_restarting}" 'old primary remains stopped'
if run_psql primary-db "${PRIMARY_PORT}" 'SELECT 1' >/dev/null 2>&1; then
  fail 'old primary remained writable after fencing'
fi
pass 'old primary is non-writable after durable fencing'
assert_file_value "${FENCE_MARKER}" state fenced
assert_file_value "${FENCE_MARKER}" primary_host primary-db
assert_file_value "${FENCE_MARKER}" new_primary "standby-db:${STANDBY_PORT}"
new_role="$(run_psql standby-db "${STANDBY_PORT}" \
  "SELECT pg_is_in_recovery()::text || '|' || current_setting('transaction_read_only')")"
assert_equal 'false|off' "${new_role}" 'promoted primary role'
pass 'new primary is f|off over TLS verify-full'
assert_file_value "${ROUTE_MARKER}" state complete
assert_file_value "${ROUTE_MARKER}" endpoint "standby-db:${STANDBY_PORT}"
assert_file_value "${ROUTE_MARKER}" invocation_count 1
assert_file_value "${STATE_DIR}/promoted" route_state complete
assert_equal promoted "$(awk -F '\t' '{print $7}' "${STATE_DIR}/state.tsv")" 'controller promotion state'
grep -qx 'role=primary' "${STATE_DIR}/health.signal" || fail 'controller promotion signal is missing'
grep -Fq 'event=route_complete' "${TMP_DIR}/controller-failover.err" || fail 'controller route completion is missing'
grep -Fq 'event=promotion_complete' "${TMP_DIR}/controller-failover.err" || fail 'controller promotion completion is missing'
pass 'controller persisted complete promotion and route state'
fence_token="$(awk -F= '$1 == "fence_token" {sub(/^[^=]*=/, ""); print; exit}' "${STATE_DIR}/promoted")"
route_ack="$(POSTGRES_FAILOVER_NEW_PRIMARY_HOST=standby-db POSTGRES_FAILOVER_NEW_PRIMARY_PORT="${STANDBY_PORT}" \
  POSTGRES_FAILOVER_FENCE_TOKEN="${fence_token}" HARNESS_PSQL="${PSQL_PATH}" \
  HARNESS_PGPASS="${PGPASS_FILE}" HARNESS_CA="${CA_FILE}" HARNESS_DB="${DB_NAME}" \
  HARNESS_USER="${REPLICATOR_USER}" HARNESS_ROUTE_MARKER="${ROUTE_MARKER}" \
  /usr/bin/env bash "${ROUTE_HOOK}")"
assert_equal "ROUTED|standby-db:${STANDBY_PORT}|${fence_token}" "${route_ack}" 'idempotent route hook acknowledgement'
assert_file_value "${ROUTE_MARKER}" invocation_count 2
pass 'test-only route hook is idempotent and re-probes the promoted primary'
pass 'isolated PostgreSQL 18.4 two-node failover integration passed'
