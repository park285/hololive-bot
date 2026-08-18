#!/usr/bin/env bash
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
OUT="${ROOT}/hololive/hololive-api/scripts/migrations/001_schema_epoch2_baseline.sql"
CONTRACT="${ROOT}/hololive/hololive-api/internal/migrationrunner/epoch2_legacy_contract.sha256"
SUFFIX_CONTRACT="${ROOT}/scripts/architecture/epoch2_suffix_contract.txt"
ACL_TAIL="${ROOT}/scripts/architecture/epoch2_acl_tail.sql"
NORMALIZER="${ROOT}/scripts/architecture/normalize-epoch2-baseline.py"
PG_IMAGE="${PG_IMAGE:-postgres:18-alpine@sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15}"
NAME="holobot-epoch2-baseline-$$"
READINESS_ATTEMPTS=60
TMP_DIR="$(mktemp -d)"
SOURCE_TREE="${TMP_DIR}/source"
DUMP="${TMP_DIR}/epoch2.sql"
BASELINE_TMP="${TMP_DIR}/001_schema_epoch2_baseline.sql"
CONTRACT_TMP="${TMP_DIR}/epoch2_legacy_contract.sha256"
SUFFIX_TMP="${TMP_DIR}/epoch2_suffix_contract.txt"

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

if [[ ! "${PG_IMAGE}" =~ @sha256:[0-9a-f]{64}$ ]]; then
  echo "PG_IMAGE must be pinned by sha256 digest: ${PG_IMAGE}" >&2
  exit 1
fi

docker run -d --rm --name "${NAME}" \
  -e POSTGRES_USER=hololive \
  -e POSTGRES_PASSWORD=hololive \
  -e POSTGRES_DB=hololive \
  "${PG_IMAGE}" >/dev/null

ready=false
for ((attempt = 1; attempt <= READINESS_ATTEMPTS; attempt++)); do
  if docker exec "${NAME}" pg_isready -U hololive -d hololive >/dev/null 2>&1; then
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

echo "generated: ${OUT}"
echo "generated: ${CONTRACT}"
echo "generated: ${SUFFIX_CONTRACT}"
