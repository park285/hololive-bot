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

list_invalid_indexes() {
  run_psql -tAc "SELECT format('%I.%I', n.nspname, c.relname) FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid JOIN pg_namespace n ON n.oid = c.relnamespace WHERE NOT i.indisvalid AND NOT EXISTS (SELECT 1 FROM pg_stat_progress_create_index p WHERE p.index_relid = i.indexrelid) ORDER BY 1;"
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

  # MIGRATION_GLOB is intentionally a case pattern selected by the operator.
  # shellcheck disable=SC2254
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

ledger_count="$(run_psql -tAc "SELECT count(filename) FROM schema_migrations;" | tr -d '[:space:]')"
if [ "${ledger_count}" = "0" ]; then
  existing_schema="$(run_psql -tAc "SELECT (to_regclass('public.members') IS NOT NULL AND to_regclass('public.alarms') IS NOT NULL);" | tr -d '[:space:]')"
  if [ "${existing_schema}" = "t" ]; then
    # 매니페스트 전체를 baseline 하면 아직 적용 안 된 신규 마이그레이션까지 SQL 없이 applied로 찍혀
    # 스키마가 조용히 뒤처진다(073 DB에 074-082가 skip 된 실제 사고). watermark 이하만 baseline 한다.
    baseline_through="${MIGRATION_BASELINE_THROUGH:-}"
    if [ -z "${baseline_through}" ]; then
      echo "ERROR: existing schema detected with empty schema_migrations ledger." >&2
      echo "Refusing to baseline the whole manifest as applied — that would silently skip genuinely-pending migrations." >&2
      echo "Set MIGRATION_BASELINE_THROUGH to the last manifest filename already applied to this DB" >&2
      echo "(baseline through it, apply the remainder). If the ledger is unexpectedly empty, investigate before retrying." >&2
      exit 1
    fi
    if ! grep -qxF "${baseline_through}" "${MANIFEST_SELECTED_ORDERED}"; then
      echo "ERROR: MIGRATION_BASELINE_THROUGH='${baseline_through}' is not a selected manifest migration filename" >&2
      exit 1
    fi
    echo "==> existing schema with empty ledger; baselining through ${baseline_through} (no SQL re-run), applying the remainder"
    while IFS= read -r baseline_file || [ -n "${baseline_file}" ]; do
      [ -n "${baseline_file}" ] || continue
      run_psql -q -c "INSERT INTO schema_migrations(filename) VALUES ('${baseline_file}') ON CONFLICT (filename) DO NOTHING;"
      if [ "${baseline_file}" = "${baseline_through}" ]; then
        break
      fi
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
  # CONCURRENTLY 실패 잔재(invalid index)는 이름을 점유해 다음 실행의 IF NOT EXISTS를 no-op으로
  # 만들고, 그 no-op이 ledger에 applied로 기록되면 잔재를 DROP해도 재빌드 경로가 사라진다.
  # 그래서 ledger 기록 전에 감지·DROP하고 실패시켜, 재실행이 같은 파일을 다시 적용하게 한다.
  if grep -qiE 'CREATE[[:space:]]+(UNIQUE[[:space:]]+)?INDEX[[:space:]]+CONCURRENTLY' "${file}"; then
    invalid_after_file="$(list_invalid_indexes)"
    if [ -n "${invalid_after_file}" ]; then
      echo "ERROR: invalid index(es) present after applying ${filename}; dropping them so a rerun rebuilds:" >&2
      printf '%s\n' "${invalid_after_file}" >&2
      printf '%s\n' "${invalid_after_file}" | while IFS= read -r bad_index; do
        [ -n "${bad_index}" ] || continue
        run_psql -q -c "DROP INDEX CONCURRENTLY IF EXISTS ${bad_index};"
      done
      echo "${filename} was NOT recorded in schema_migrations; rerun apply-all.sh to rebuild." >&2
      exit 1
    fi
  fi
  run_psql -q -c "INSERT INTO schema_migrations(filename) VALUES ('${filename}') ON CONFLICT (filename) DO NOTHING;"
done < "${MANIFEST_SELECTED_ORDERED}"

echo "==> checking for invalid indexes"
invalid_indexes="$(list_invalid_indexes)"
if [ -n "${invalid_indexes}" ]; then
  echo "ERROR: invalid PostgreSQL indexes remain after migrations (left by something outside this runner):" >&2
  printf '%s\n' "${invalid_indexes}" >&2
  echo "Drop the invalid indexes (DROP INDEX IF EXISTS <name>) and recreate them from their owning DDL, then rerun." >&2
  exit 1
fi

echo "==> hololive migrations applied"
