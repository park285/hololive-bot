#!/bin/sh
set -eu

PGHOST="${PGHOST:-postgres}"
PGPORT="${PGPORT:-5432}"
PGUSER="${PGUSER:-hololive_migrator}"
PGPASSWORD="${PGPASSWORD:-}"
PGDATABASE="${PGDATABASE:-hololive}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-/migrations}"
MIGRATION_GLOB="${MIGRATION_GLOB:-[0-9]*.sql}"

if [ -z "${PGPASSWORD}" ]; then
  echo "PGPASSWORD is required" >&2
  exit 1
fi

echo "==> applying migrations in ${MIGRATIONS_DIR}/${MIGRATION_GLOB} to ${PGDATABASE}@${PGHOST}:${PGPORT}"

for file in $(ls -1 "${MIGRATIONS_DIR}"/${MIGRATION_GLOB} 2>/dev/null | sort); do
  echo "==> apply ${file}"
  PGPASSWORD="${PGPASSWORD}" psql \
    -v ON_ERROR_STOP=1 \
    -h "${PGHOST}" \
    -p "${PGPORT}" \
    -U "${PGUSER}" \
    -d "${PGDATABASE}" \
    -f "${file}"
done

echo "==> hololive migrations applied"
