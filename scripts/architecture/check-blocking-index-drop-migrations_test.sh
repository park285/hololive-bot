#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

cat >"${TMP_DIR}/114_legacy.sql" <<'SQL'
DROP INDEX IF EXISTS legacy_index;
SQL
cat >"${TMP_DIR}/115_concurrent.sql" <<'SQL'
DROP /* safe comment */ INDEX
CONCURRENTLY IF EXISTS safe_index;
SQL

bash "${SCRIPT_DIR}/check-blocking-index-drop-migrations.sh" "${TMP_DIR}" >/dev/null

cat >"${TMP_DIR}/115_concurrent.sql" <<'SQL'
DROP
INDEX IF EXISTS unsafe_index;
SQL
if bash "${SCRIPT_DIR}/check-blocking-index-drop-migrations.sh" "${TMP_DIR}" >/dev/null 2>&1; then
  echo "FAIL: multiline blocking DROP INDEX in migration 115 was accepted" >&2
  exit 1
fi

cat >"${TMP_DIR}/115_concurrent.sql" <<'SQL'
DO $body$
BEGIN
  RAISE NOTICE 'DROP INDEX inside a quoted body is not direct DDL';
END
$body$;
SQL
bash "${SCRIPT_DIR}/check-blocking-index-drop-migrations.sh" "${TMP_DIR}" >/dev/null

echo "OK: blocking index-drop migration gate rejects new unsafe DDL"
