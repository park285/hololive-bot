#!/bin/sh
# Copyright (c) 2025 Kapu
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in
# all copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
# SOFTWARE.

set -eu

PGHOST="${PGHOST:-postgres}"
PGPORT="${PGPORT:-5432}"
PGUSER="${PGUSER:-hololive_migrator}"
PGPASSWORD="${PGPASSWORD:-}"
PGDATABASE="${PGDATABASE:-hololive}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-/migrations}"
MIGRATION_GLOB="${MIGRATION_GLOB:-[0-9]*.sql}"
MIGRATION_MANIFEST="${MIGRATION_MANIFEST:-${MIGRATIONS_DIR}/manifest.txt}"
MANIFEST_ALL_ORDERED="$(mktemp)"
MANIFEST_ALL_SORTED="$(mktemp)"
MANIFEST_SELECTED_ORDERED="$(mktemp)"
MANIFEST_SELECTED_SORTED="$(mktemp)"
ACTUAL_ALL_SQL_SORTED="$(mktemp)"

cleanup() {
  rm -f "${MANIFEST_ALL_ORDERED}" "${MANIFEST_ALL_SORTED}" "${MANIFEST_SELECTED_ORDERED}" "${MANIFEST_SELECTED_SORTED}" "${ACTUAL_ALL_SQL_SORTED}"
}
trap cleanup EXIT INT TERM HUP

run_psql() {
  PGPASSWORD="${PGPASSWORD}" psql \
    -v ON_ERROR_STOP=1 \
    -h "${PGHOST}" \
    -p "${PGPORT}" \
    -U "${PGUSER}" \
    -d "${PGDATABASE}" \
    "$@"
}

if [ -z "${PGPASSWORD}" ]; then
  echo "PGPASSWORD is required" >&2
  exit 1
fi

if [ ! -f "${MIGRATION_MANIFEST}" ]; then
  echo "migration manifest not found: ${MIGRATION_MANIFEST}" >&2
  exit 1
fi

for path in "${MIGRATIONS_DIR}"/[0-9]*.sql; do
  [ -f "${path}" ] || continue
  basename "${path}" >> "${ACTUAL_ALL_SQL_SORTED}"
done
sort -u "${ACTUAL_ALL_SQL_SORTED}" -o "${ACTUAL_ALL_SQL_SORTED}"

echo "==> applying migrations from manifest ${MIGRATION_MANIFEST} to ${PGDATABASE}@${PGHOST}:${PGPORT}"

expected_order=1

while IFS= read -r entry || [ -n "${entry}" ]; do
  trimmed_entry=$(printf '%s' "${entry}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
  case "${trimmed_entry}" in
    ''|'#'*)
      continue
      ;;
  esac

  set -- ${trimmed_entry}
  order="${1:-}"
  filename="${2:-}"
  extra="${3:-}"
  if [ -z "${order}" ] || [ -z "${filename}" ] || [ -n "${extra}" ]; then
    echo "invalid manifest entry: ${trimmed_entry}" >&2
    exit 1
  fi

  expected_label=$(printf '%03d' "${expected_order}")
  if [ "${order}" != "${expected_label}" ]; then
    echo "migration manifest order drift: expected ${expected_label}, got ${order}" >&2
    exit 1
  fi
  expected_order=$((expected_order + 1))

  case "${filename}" in
    [0-9]*.sql)
      ;;
    *)
      echo "invalid migration manifest SQL filename: ${filename}" >&2
      exit 1
      ;;
  esac

  printf '%s\n' "${filename}" >> "${MANIFEST_ALL_ORDERED}"

  case "${filename}" in
    ${MIGRATION_GLOB})
      printf '%s\n' "${filename}" >> "${MANIFEST_SELECTED_ORDERED}"
      ;;
  esac
done < "${MIGRATION_MANIFEST}"

sort -u "${MANIFEST_ALL_ORDERED}" -o "${MANIFEST_ALL_SORTED}"
sort -u "${MANIFEST_SELECTED_ORDERED}" -o "${MANIFEST_SELECTED_SORTED}"

manifest_all_count="$(wc -l < "${MANIFEST_ALL_ORDERED}" | tr -d '[:space:]')"
manifest_all_unique_count="$(wc -l < "${MANIFEST_ALL_SORTED}" | tr -d '[:space:]')"
if [ "${manifest_all_count}" = "0" ]; then
  echo "migration manifest is empty" >&2
  exit 1
fi

if [ "${manifest_all_count}" != "${manifest_all_unique_count}" ]; then
  echo "duplicate migration filenames in manifest" >&2
  exit 1
fi

if ! cmp -s "${MANIFEST_ALL_SORTED}" "${ACTUAL_ALL_SQL_SORTED}"; then
  echo "migration manifest and actual SQL files differ" >&2
  echo "--- manifest files" >&2
  cat "${MANIFEST_ALL_SORTED}" >&2
  echo "--- actual files" >&2
  cat "${ACTUAL_ALL_SQL_SORTED}" >&2
  exit 1
fi

echo "==> ensuring schema_migrations ledger"
run_psql -q -c "CREATE TABLE IF NOT EXISTS schema_migrations (filename TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now());"

ledger_count="$(run_psql -tAc "SELECT count(*) FROM schema_migrations;" | tr -d '[:space:]')"
if [ "${ledger_count}" = "0" ]; then
  existing_schema="$(run_psql -tAc "SELECT (to_regclass('public.members') IS NOT NULL AND to_regclass('public.alarms') IS NOT NULL);" | tr -d '[:space:]')"
  if [ "${existing_schema}" = "t" ]; then
    # ledger는 비었지만 핵심 테이블이 이미 있으면 schema_migrations 도입 이전부터 살아온 DB다.
    # 전량 재적용(특히 012의 DELETE+reseed가 운영자 편집을 날림)을 피하려 SQL 재실행 없이 applied로만 마킹한다.
    echo "==> existing schema detected with empty ledger; baselining manifest as already-applied (no SQL re-run)"
    while IFS= read -r baseline_file || [ -n "${baseline_file}" ]; do
      [ -n "${baseline_file}" ] || continue
      run_psql -q -c "INSERT INTO schema_migrations(filename) VALUES ('${baseline_file}') ON CONFLICT (filename) DO NOTHING;"
    done < "${MANIFEST_SELECTED_ORDERED}"
  fi
fi

while IFS= read -r filename || [ -n "${filename}" ]; do
  [ -n "${filename}" ] || continue
  file="${MIGRATIONS_DIR}/${filename}"
  if [ ! -f "${file}" ]; then
    echo "manifest entry not found: ${file}" >&2
    exit 1
  fi

  if [ "$(run_psql -tAc "SELECT 1 FROM schema_migrations WHERE filename = '${filename}';" | tr -d '[:space:]')" = "1" ]; then
    echo "==> skip ${filename} (already applied)"
    continue
  fi

  echo "==> apply ${file}"
  run_psql -f "${file}"
  run_psql -q -c "INSERT INTO schema_migrations(filename) VALUES ('${filename}') ON CONFLICT (filename) DO NOTHING;"
done < "${MANIFEST_SELECTED_ORDERED}"

echo "==> hololive migrations applied"
