#!/usr/bin/env bash
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  printf '%s\n' "source-only helper: ${BASH_SOURCE[0]}" >&2
  exit 1
fi

cleanup_container() {
  local name="${1}" created="${2}"
  [[ "${created}" == 1 ]] || return 0
  [[ "$("${DOCKER_PATH}" inspect --format '{{index .Config.Labels "com.hololive.postgres-failover-integration"}}' "${name}" 2>/dev/null || true)" == "${LABEL_VALUE}" ]] || return 0
  "${DOCKER_PATH}" rm --force "${name}" >/dev/null 2>&1 || true
}
cleanup_volume() {
  local name="${1}" created="${2}"
  [[ "${created}" == 1 ]] || return 0
  [[ "$("${DOCKER_PATH}" volume inspect --format '{{index .Labels "com.hololive.postgres-failover-integration"}}' "${name}" 2>/dev/null || true)" == "${LABEL_VALUE}" ]] || return 0
  "${DOCKER_PATH}" volume rm "${name}" >/dev/null 2>&1 || true
}
cleanup_network() {
  [[ "${NETWORK_CREATED}" == 1 ]] || return 0
  [[ "$("${DOCKER_PATH}" network inspect --format '{{index .Labels "com.hololive.postgres-failover-integration"}}' "${NETWORK_NAME}" 2>/dev/null || true)" == "${LABEL_VALUE}" ]] || return 0
  "${DOCKER_PATH}" network rm "${NETWORK_NAME}" >/dev/null 2>&1 || true
}
cleanup() {
  local rc=$?
  set +e
  cleanup_container "${STANDBY_CONTAINER}" "${STANDBY_CONTAINER_CREATED}"
  cleanup_container "${PRIMARY_CONTAINER}" "${PRIMARY_CONTAINER_CREATED}"
  cleanup_volume "${STANDBY_TLS_VOLUME}" "${STANDBY_TLS_VOLUME_CREATED}"
  cleanup_volume "${PRIMARY_TLS_VOLUME}" "${PRIMARY_TLS_VOLUME_CREATED}"
  cleanup_volume "${STANDBY_VOLUME}" "${STANDBY_VOLUME_CREATED}"
  cleanup_volume "${PRIMARY_VOLUME}" "${PRIMARY_VOLUME_CREATED}"
  cleanup_network
  rm -rf -- "${TMP_DIR}"
  exit "${rc}"
}
fail() { printf '[FAIL] %s\n' "$*" >&2; exit 1; }
pass() { printf '[PASS] %s\n' "$*"; }
assert_equal() {
  local expected="${1}" actual="${2}" label="${3}"
  [[ "${actual}" == "${expected}" ]] || fail "${label}: expected ${expected}, got ${actual}"
}
assert_file_value() {
  local file="${1}" key="${2}" expected="${3}" actual
  actual="$(awk -F= -v key="${key}" '$1 == key {sub(/^[^=]*=/, ""); print; exit}' "${file}")"
  assert_equal "${expected}" "${actual}" "${key} in ${file}"
}
refuse_existing() {
  local kind="${1}" name="${2}"
  case "${kind}" in
    container) if "${DOCKER_PATH}" inspect "${name}" >/dev/null 2>&1; then fail "refusing pre-existing ${kind} name ${name}"; fi ;;
    volume) if "${DOCKER_PATH}" volume inspect "${name}" >/dev/null 2>&1; then fail "refusing pre-existing ${kind} name ${name}"; fi ;;
    network) if "${DOCKER_PATH}" network inspect "${name}" >/dev/null 2>&1; then fail "refusing pre-existing ${kind} name ${name}"; fi ;;
    *) fail "unknown Docker resource kind ${kind}" ;;
  esac
}
create_volume() {
  refuse_existing volume "${1}"
  "${DOCKER_PATH}" volume create --label "${LABEL_KEY}=${LABEL_VALUE}" "${1}" >/dev/null
}
create_network() {
  refuse_existing network "${NETWORK_NAME}"
  "${DOCKER_PATH}" network create --internal --label "${LABEL_KEY}=${LABEL_VALUE}" "${NETWORK_NAME}" >/dev/null
  NETWORK_CREATED=1
}
run_psql() {
  local host="${1}" port="${2}" sql="${3}"
  PGPASSFILE="${PGPASS_FILE}" PGSSLMODE=verify-full PGSSLROOTCERT="${CA_FILE}" \
    PGCONNECT_TIMEOUT=2 PGOPTIONS='-c statement_timeout=2000' \
    /usr/bin/timeout --foreground --kill-after=2 6s "${PSQL_PATH}" \
    -X -v ON_ERROR_STOP=1 -AtF '|' -h "${host}" -p "${port}" \
    -U "${REPLICATOR_USER}" -d "${DB_NAME}" -c "${sql}"
}
wait_for_psql() {
  local host="${1}" port="${2}" name="${3}"
  for _ in {1..90}; do
    if run_psql "${host}" "${port}" 'SELECT 1' >/dev/null 2>&1; then
      pass "${name} accepts TLS verify-full psql"
      return 0
    fi
    [[ "$("${DOCKER_PATH}" inspect --format '{{.State.Running}}' "${name}" 2>/dev/null || true)" == false ]] && break
    sleep 1
  done
  fail "${name} did not accept a host psql connection"
}
wait_for_streaming() {
  local local_state primary_state recovery read_only receive_lsn replay_lsn primary_lsn slot_active
  for _ in {1..90}; do
    local_state="$(run_psql standby-db "${STANDBY_PORT}" "SELECT pg_is_in_recovery(), current_setting('transaction_read_only'), COALESCE(pg_last_wal_receive_lsn()::text, '0/0'), COALESCE(pg_last_wal_replay_lsn()::text, '0/0')" 2>/dev/null || true)"
    primary_state="$(run_psql primary-db "${PRIMARY_PORT}" "SELECT pg_current_wal_lsn()::text, COALESCE((SELECT active::text FROM pg_replication_slots WHERE slot_name = '${REPLICATION_SLOT}'), 'false')" 2>/dev/null || true)"
    IFS='|' read -r recovery read_only receive_lsn replay_lsn <<<"${local_state}"
    IFS='|' read -r primary_lsn slot_active <<<"${primary_state}"
    if [[ "${recovery}" == t && "${read_only}" == on && "${slot_active}" == true \
      && -n "${primary_lsn}" && "${primary_lsn}" == "${receive_lsn}" && "${primary_lsn}" == "${replay_lsn}" ]]; then
      pass 'standby is a caught-up streaming replica on the physical slot'
      return 0
    fi
    sleep 1
  done
  fail 'standby did not become a caught-up streaming replica'
}

write_tls_material() {
  local node="${1}" dir="${TMP_DIR}/certs/${1}"
  mkdir -p "${dir}" && chmod 0700 "${dir}"
  printf 'subjectAltName=DNS:%s-db,DNS:%s,IP:127.0.0.1\n' "${node}" "${node}" >"${TMP_DIR}/certs/${node}.ext"
  "${OPENSSL_PATH}" req -new -newkey rsa:2048 -nodes -keyout "${dir}/server.key" \
    -out "${dir}/server.csr" -subj "/CN=${node}-db" >/dev/null 2>&1
  "${OPENSSL_PATH}" x509 -req -in "${dir}/server.csr" -CA "${CA_FILE}" -CAkey "${TMP_DIR}/certs/ca.key" \
    -CAcreateserial -out "${dir}/server.crt" -days 1 -sha256 \
    -extfile "${TMP_DIR}/certs/${node}.ext" >/dev/null 2>&1
  cp -- "${CA_FILE}" "${dir}/ca.crt"
  chmod 0600 "${dir}/server.key"
  chmod 0644 "${dir}/server.crt" "${dir}/ca.crt"
}
stage_tls_volume() {
  "${DOCKER_PATH}" run --rm --user 0 -v "${1}:/tls" -v "${TMP_DIR}/certs/${2}:/input:ro" \
    "${POSTGRES_IMAGE}" sh -ec \
    'cp /input/server.crt /tls/server.crt; cp /input/server.key /tls/server.key; cp /input/ca.crt /tls/ca.crt; chown postgres:postgres /tls/server.crt /tls/server.key /tls/ca.crt; chmod 0644 /tls/server.crt /tls/ca.crt; chmod 0600 /tls/server.key'
}
write_cluster_files() {
  mkdir -p "${TMP_DIR}/postgres" "${TMP_DIR}/init"
  chmod 0700 "${TMP_DIR}/postgres"
  chmod 0755 "${TMP_DIR}/init"
  cat >"${TMP_DIR}/postgres/pg_hba.conf" <<'EOF_HBA'
local   all             all                                     trust
hostssl replication     hololive_replicator 0.0.0.0/0           scram-sha-256
hostssl hololive        hololive_replicator 0.0.0.0/0           scram-sha-256
EOF_HBA
  chmod 0644 "${TMP_DIR}/postgres/pg_hba.conf"
  cat >"${TMP_DIR}/init/01-failover-role.sql" <<EOF_SQL
CREATE ROLE ${REPLICATOR_USER} WITH LOGIN REPLICATION PASSWORD '${REPLICATOR_PASSWORD}';
GRANT CONNECT ON DATABASE ${DB_NAME} TO ${REPLICATOR_USER};
GRANT EXECUTE ON FUNCTION pg_catalog.pg_promote(boolean, integer) TO ${REPLICATOR_USER};
SELECT pg_create_physical_replication_slot('${REPLICATION_SLOT}');
EOF_SQL
  chmod 0644 "${TMP_DIR}/init/01-failover-role.sql"
  printf "primary_conninfo = 'host=primary-db port=5432 user=%s dbname=replication sslmode=verify-full sslrootcert=/etc/postgres-tls/ca.crt passfile=/var/lib/postgresql/pgdata/pgpass application_name=standby'\nprimary_slot_name = '%s'\n" \
    "${REPLICATOR_USER}" "${REPLICATION_SLOT}" >"${TMP_DIR}/standby.auto.conf"
  chmod 0644 "${TMP_DIR}/standby.auto.conf"
}
