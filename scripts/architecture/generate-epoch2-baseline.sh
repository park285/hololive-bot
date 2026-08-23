#!/usr/bin/env bash
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
OUT="${ROOT}/hololive/hololive-api/scripts/migrations/001_schema_epoch2_baseline.sql"
CONTRACT="${ROOT}/scripts/architecture/epoch2_legacy_contract.sha256"
SUFFIX_CONTRACT="${ROOT}/scripts/architecture/epoch2_suffix_contract.txt"
ACL_TAIL="${ROOT}/hololive/hololive-api/scripts/migrations/manual/epoch2_acl_tail.sql"
NORMALIZER="${ROOT}/scripts/architecture/normalize-epoch2-baseline.py"
REPAIR_SOURCE_DIR="${ROOT}/hololive/hololive-api/scripts/migrations/manual/epoch1_message_contract_repair_sources"
RECOVERY_SOURCE_DIR="${ROOT}/hololive/hololive-api/scripts/migrations/manual/epoch1_recovery_sources"
INTEGRATION_SOURCE_DIR="${ROOT}/hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/testdata/epoch1_migrations"
OBSERVATION_SOURCE_DIR="${ROOT}/hololive/hololive-shared/pkg/service/youtube/tracking/observation/testdata/epoch1_migrations"
PG_IMAGE="${PG_IMAGE:-postgres:18.6-alpine@sha256:d3e1620b530c944afa6e887d22eb899824da68e19c52024bf98f5220c88a65b2}"
NAME="holobot-epoch2-baseline-$$"
READINESS_ATTEMPTS=60
TMP_DIR="$(mktemp -d)"
SOURCE_TREE="${TMP_DIR}/source"
DUMP="${TMP_DIR}/epoch2.sql"
BASELINE_TMP="${TMP_DIR}/001_schema_epoch2_baseline.sql"
CONTRACT_TMP="${TMP_DIR}/epoch2_legacy_contract.sha256"
SUFFIX_TMP="${TMP_DIR}/epoch2_suffix_contract.txt"
REPAIR_TMP="${TMP_DIR}/epoch1_message_contract_repair_sources"
RECOVERY_TMP="${TMP_DIR}/epoch1_recovery_sources"
INTEGRATION_TMP="${TMP_DIR}/epoch1_integration_sources"
OBSERVATION_TMP="${TMP_DIR}/epoch1_observation_sources"
REPAIR_FILES=(
  074_create_message_strings.sql
  076_seed_new_command_templates.sql
  077_seed_notification_celebration_templates.sql
  078_unify_outbox_header_body_templates.sql
  079_seed_error_strings.sql
  080_refresh_help_and_ambiguous.sql
  081_seed_canonical_alarm_templates.sql
  082_seed_calendar_image_strings.sql
)
RECOVERY_FILES=(
  114_drop_unused_indexes.sql
)
INTEGRATION_FILES=(
  058_create_alarm_dispatch_outbox.sql
  059_harden_alarm_dispatch_outbox.sql
  065_record_alarm_dispatch_event_collisions.sql
  118_alarm_dispatch_state_shape_check.sql
  122_alarm_dispatch_last_error_size_check.sql
)
OBSERVATION_FILES=(
  070_repoint_youtube_content_alarm_tracking_pk_to_canonical.sql
)

SOURCE_COMMIT="${EPOCH2_SOURCE_COMMIT:-}"
if [[ -z "${SOURCE_COMMIT}" && -f "${OUT}" ]]; then
  SOURCE_COMMIT="$(sed -n 's/^-- Source commit: //p' "${OUT}" | head -n 1)"
fi
if [[ -z "${SOURCE_COMMIT}" ]]; then
  SOURCE_COMMIT="$(git rev-parse HEAD)"
fi

cleanup() {
  docker rm -f "${NAME}" >/dev/null 2>&1 || true
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

git cat-file -e "${SOURCE_COMMIT}^{commit}"
mkdir -p "${SOURCE_TREE}"
git archive "${SOURCE_COMMIT}" hololive/hololive-api/scripts/migrations | tar -x -C "${SOURCE_TREE}"

MIG_DIR="${SOURCE_TREE}/hololive/hololive-api/scripts/migrations"
MANIFEST="${MIG_DIR}/manifest.txt"
mapfile -t LEGACY < <(
  awk '
    /^[[:space:]]*#/ || NF == 0 { next }
    {
      file=$NF
      if (file == "140_epoch2_checkpoint.sql") exit
      print file
    }
  ' "${MANIFEST}"
)
mapfile -t CONTRACT_FILES < <(
  awk '
    /^[[:space:]]*#/ || NF == 0 { next }
    {
      file=$NF
      print file
      if (file == "140_epoch2_checkpoint.sql") exit
    }
  ' "${MANIFEST}"
)
mapfile -t SUFFIX < <(
  awk '
    /^[[:space:]]*#/ || NF == 0 { next }
    found { print $NF }
    $NF == "140_epoch2_checkpoint.sql" { found=1 }
  ' "${MANIFEST}"
)

if [[ ${#LEGACY[@]} -eq 0 || "${LEGACY[0]}" != "006-base-runtime-tables.sql" || "${LEGACY[-1]}" != "139_trust_alarm_short_links.sql" ]]; then
  echo "epoch-2 legacy source must span 006-base-runtime-tables.sql through 139_trust_alarm_short_links.sql" >&2
  exit 1
fi
if ! grep -qE '^[[:space:]]*[0-9]+[[:space:]]+140_epoch2_checkpoint\.sql[[:space:]]*$' "${MANIFEST}"; then
  echo "epoch-2 compatibility checkpoint is missing from source manifest" >&2
  exit 1
fi
if [[ "${CONTRACT_FILES[-1]}" != "140_epoch2_checkpoint.sql" ]]; then
  echo "epoch-2 legacy contract must end at 140_epoch2_checkpoint.sql" >&2
  exit 1
fi
if [[ ${#SUFFIX[@]} -eq 0 || "${SUFFIX[0]}" != "141_alarm_dispatch_send_units.sql" ]]; then
  echo "epoch-2 retained suffix must begin at 141_alarm_dispatch_send_units.sql" >&2
  exit 1
fi

for file in "${CONTRACT_FILES[@]}"; do
  checksum="$(sha256sum "${MIG_DIR}/${file}" | awk '{print $1}')"
  printf '%s  %s\n' "${checksum}" "${file}" >> "${CONTRACT_TMP}"
done
printf '%s\n' "${SUFFIX[@]}" > "${SUFFIX_TMP}"
install -d -m 0755 "${REPAIR_TMP}"
for file in "${REPAIR_FILES[@]}"; do
  install -m 0644 "${MIG_DIR}/${file}" "${REPAIR_TMP}/${file}"
done
install -d -m 0755 "${RECOVERY_TMP}"
for file in "${RECOVERY_FILES[@]}"; do
  install -m 0644 "${MIG_DIR}/${file}" "${RECOVERY_TMP}/${file}"
done
install -d -m 0755 "${INTEGRATION_TMP}"
for file in "${INTEGRATION_FILES[@]}"; do
  install -m 0644 "${MIG_DIR}/${file}" "${INTEGRATION_TMP}/${file}"
done
install -d -m 0755 "${OBSERVATION_TMP}"
for file in "${OBSERVATION_FILES[@]}"; do
  install -m 0644 "${MIG_DIR}/${file}" "${OBSERVATION_TMP}/${file}"
done

if [[ ! "${PG_IMAGE}" =~ @sha256:[0-9a-f]{64}$ ]]; then
  echo "PG_IMAGE must be pinned by sha256 digest: ${PG_IMAGE}" >&2
  exit 1
fi

docker run -d --rm --name "${NAME}" \
  -e POSTGRES_USER=hololive \
  -e POSTGRES_PASSWORD=hololive \
  -e POSTGRES_DB=hololive \
  -e POSTGRES_INITDB_ARGS="--locale-provider=builtin --builtin-locale=C.UTF-8 --encoding=UTF8" \
  "${PG_IMAGE}" >/dev/null

ready=false
for ((attempt = 1; attempt <= READINESS_ATTEMPTS; attempt++)); do
  if ready_output="$(docker exec "${NAME}" psql -X -At -U hololive -d hololive -c 'SELECT 1' 2>/dev/null)" &&
     [[ "${ready_output}" == "1" ]]; then
    ready=true
    break
  fi
  if ! running="$(docker inspect --format '{{.State.Running}}' "${NAME}")"; then
    echo "failed to inspect PostgreSQL container readiness" >&2
    exit 1
  fi
  if [[ "${running}" != "true" ]]; then
    echo "PostgreSQL container exited before becoming ready" >&2
    exit 1
  fi
  sleep 1
done
if [[ "${ready}" != "true" ]]; then
  echo "PostgreSQL did not become ready after ${READINESS_ATTEMPTS} seconds" >&2
  exit 1
fi

SERVER_VERSION_NUM="$(docker exec "${NAME}" psql -X -At -U hololive -d hololive -c 'SHOW server_version_num')"
if [[ ! "${SERVER_VERSION_NUM}" =~ ^[0-9]+$ ]] || (( SERVER_VERSION_NUM < 180000 || SERVER_VERSION_NUM >= 190000 )); then
  echo "epoch-2 baseline generation requires PostgreSQL 18, got server_version_num=${SERVER_VERSION_NUM}" >&2
  exit 1
fi

docker exec -i "${NAME}" psql -X -v ON_ERROR_STOP=1 -U hololive -d hololive >/dev/null <<'SQL'
CREATE ROLE hololive_runtime NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT;
CREATE ROLE hololive_scraper NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO hololive_runtime, hololive_scraper;
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON TABLES FROM PUBLIC;
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON SEQUENCES FROM PUBLIC;
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON FUNCTIONS FROM PUBLIC;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO hololive_runtime;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO hololive_runtime;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO hololive_runtime;
SQL

for file in "${LEGACY[@]}"; do
  echo "apply ${file}"
  docker exec -i "${NAME}" \
    psql -X -v ON_ERROR_STOP=1 -U hololive -d hololive \
    < "${MIG_DIR}/${file}"
done

docker exec -i "${NAME}" psql -X -v ON_ERROR_STOP=1 -U hololive -d hololive >/dev/null <<'SQL'
SET session_replication_role = replica;
DO $normalize_migration_time$
DECLARE
  target record;
BEGIN
  FOR target IN
    SELECT n.nspname,
           c.relname,
           string_agg(
             format(
               '%1$I = CASE WHEN %1$I IS NULL THEN NULL ELSE %2$L::%3$s END',
               a.attname,
               '2000-01-01 00:00:00+00',
               CASE WHEN a.atttypid = 'timestamptz'::regtype THEN 'timestamptz' ELSE 'timestamp' END
             ),
             ', ' ORDER BY a.attname
           ) AS assignments
    FROM pg_attribute a
    JOIN pg_class c ON c.oid = a.attrelid
    JOIN pg_namespace n ON n.oid = c.relnamespace
    JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
    WHERE n.nspname = 'public'
      AND c.relkind IN ('r', 'p')
      AND a.atttypid IN ('timestamp'::regtype, 'timestamptz'::regtype)
      AND pg_get_expr(d.adbin, d.adrelid) ~* '(now\(\)|CURRENT_TIMESTAMP|clock_timestamp\(\))'
    GROUP BY n.nspname, c.relname
  LOOP
    EXECUTE format('UPDATE %I.%I SET %s', target.nspname, target.relname, target.assignments);
  END LOOP;
END
$normalize_migration_time$;
SET session_replication_role = origin;
SQL

docker exec "${NAME}" pg_dump \
  -U hololive -d hololive \
  --format=plain \
  --schema=public \
  --no-owner \
  --no-privileges \
  --no-comments \
  --inserts \
  --column-inserts \
  > "${DUMP}"

python3 "${NORMALIZER}" \
  --input "${DUMP}" \
  --output "${BASELINE_TMP}" \
  --acl-tail "${ACL_TAIL}" \
  --source-commit "${SOURCE_COMMIT}"

install -m 0644 "${BASELINE_TMP}" "${OUT}"
install -m 0644 "${CONTRACT_TMP}" "${CONTRACT}"
install -m 0644 "${SUFFIX_TMP}" "${SUFFIX_CONTRACT}"
install -d -m 0755 "${REPAIR_SOURCE_DIR}"
for file in "${REPAIR_FILES[@]}"; do
  install -m 0644 "${REPAIR_TMP}/${file}" "${REPAIR_SOURCE_DIR}/${file}"
done
install -d -m 0755 "${RECOVERY_SOURCE_DIR}"
for file in "${RECOVERY_FILES[@]}"; do
  install -m 0644 "${RECOVERY_TMP}/${file}" "${RECOVERY_SOURCE_DIR}/${file}"
done
install -d -m 0755 "${INTEGRATION_SOURCE_DIR}"
for file in "${INTEGRATION_FILES[@]}"; do
  install -m 0644 "${INTEGRATION_TMP}/${file}" "${INTEGRATION_SOURCE_DIR}/${file}"
done
install -d -m 0755 "${OBSERVATION_SOURCE_DIR}"
for file in "${OBSERVATION_FILES[@]}"; do
  install -m 0644 "${OBSERVATION_TMP}/${file}" "${OBSERVATION_SOURCE_DIR}/${file}"
done

echo "generated: ${OUT}"
echo "generated: ${CONTRACT}"
echo "generated: ${SUFFIX_CONTRACT}"
echo "generated: ${REPAIR_SOURCE_DIR}"
echo "generated: ${RECOVERY_SOURCE_DIR}"
echo "generated: ${INTEGRATION_SOURCE_DIR}"
echo "generated: ${OBSERVATION_SOURCE_DIR}"
