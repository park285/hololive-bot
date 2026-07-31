#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
drop_count="$(sed -n 's/^DROP INDEX IF EXISTS \([^;]*\);/\1/p' "${root}/114_drop_unused_indexes.sql" | sort -u | wc -l)"
inventory_count="$(sed -n '/mapfile -t indexes/,/(( .*indexes/p' "${root}/preflight-114-restore.sh" | grep -c '114_drop_unused_indexes.sql')"

[[ "${drop_count}" -gt 0 ]] || { echo "migration 114 drop inventory is empty" >&2; exit 1; }
[[ "${inventory_count}" -eq 1 ]] || { echo "preflight must derive its inventory from migration 114" >&2; exit 1; }
grep -q "pg_get_indexdef" "${root}/preflight-114-restore.sh"
grep -q "pg_get_constraintdef" "${root}/preflight-114-restore.sh"
grep -q "n.nspname = 'public'" "${root}/preflight-114-restore.sh"
grep -q "'public.members'::regclass" "${root}/preflight-114-restore.sh"
grep -q "quiescing all database writers" "${root}/preflight-114-restore.sh"
grep -q "lock_timeout" "${root}/preflight-114-restore.sh"
grep -q "^if grep -q '\^MISSING '" "${root}/preflight-114-restore.sh"
grep -q "INDEX IF NOT EXISTS" "${root}/preflight-114-restore.sh"
if grep -q 'psql .*DATABASE_URL' "${root}/preflight-114-restore.sh"; then
  echo "preflight passes DATABASE_URL through process argv" >&2
  exit 1
fi

touch "${tmp}/pgpass"
chmod 600 "${tmp}/pgpass"
cat >"${tmp}/psql" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >"${FAKE_PSQL_ARGS_FILE}"
cat <<'SQL'
CREATE INDEX IF NOT EXISTS idx_restore_one ON public.restore_target USING btree (id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_restore_two ON public.restore_target USING btree (slug);
DO $restore$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'members_slug_key' AND conrelid = 'public.members'::regclass) THEN ALTER TABLE public.members ADD CONSTRAINT members_slug_key UNIQUE (slug); END IF; END $restore$;
SQL
SH
chmod +x "${tmp}/psql"

if PATH="${tmp}:${PATH}" "${root}/preflight-114-restore.sh" "${tmp}/restore.sql" >"${tmp}/out" 2>&1; then
  echo "preflight accepted missing PGSERVICE" >&2
  exit 1
fi
grep -q 'PGSERVICE is required' "${tmp}/out"

if PATH="${tmp}:${PATH}" PGSERVICE=test-service \
  "${root}/preflight-114-restore.sh" "${tmp}/restore.sql" >"${tmp}/out" 2>&1; then
  echo "preflight accepted missing PGPASSFILE" >&2
  exit 1
fi
grep -q 'PGPASSFILE is required' "${tmp}/out"

touch "${tmp}/unreadable-pgpass"
chmod 000 "${tmp}/unreadable-pgpass"
if PATH="${tmp}:${PATH}" PGSERVICE=test-service PGPASSFILE="${tmp}/unreadable-pgpass" \
  "${root}/preflight-114-restore.sh" "${tmp}/restore.sql" >"${tmp}/out" 2>&1; then
  echo "preflight accepted unreadable PGPASSFILE" >&2
  exit 1
fi
grep -q 'PGPASSFILE is not readable' "${tmp}/out"

ln -s "${tmp}/pgpass" "${tmp}/pgpass-link"
if PATH="${tmp}:${PATH}" PGSERVICE=test-service PGPASSFILE="${tmp}/pgpass-link" \
  "${root}/preflight-114-restore.sh" "${tmp}/restore.sql" >"${tmp}/out" 2>&1; then
  echo "preflight accepted symlink PGPASSFILE" >&2
  exit 1
fi
grep -q 'PGPASSFILE must not be a symlink' "${tmp}/out"

if PATH="${tmp}:${PATH}" PGSERVICE=test-service PGPASSFILE="${tmp}/pgpass" PGPASSWORD=raw-test-secret \
  "${root}/preflight-114-restore.sh" "${tmp}/restore.sql" >"${tmp}/out" 2>&1; then
  echo "preflight accepted PGPASSWORD" >&2
  exit 1
fi
grep -q 'PGPASSWORD is forbidden' "${tmp}/out"
if grep -q 'raw-test-secret' "${tmp}/out"; then
  echo "preflight printed PGPASSWORD" >&2
  exit 1
fi

env -u PGPASSWORD PATH="${tmp}:${PATH}" PGSERVICE=test-service PGPASSFILE="${tmp}/pgpass" \
  FAKE_PSQL_ARGS_FILE="${tmp}/args" "${root}/preflight-114-restore.sh" "${tmp}/restore.sql" >/dev/null
[[ "$(cat "${tmp}/args")" == '-w -X -v ON_ERROR_STOP=1 -At' ]]
grep -q '^BEGIN;$' "${tmp}/restore.sql"
grep -q '^COMMIT;$' "${tmp}/restore.sql"
grep -q '^CREATE INDEX IF NOT EXISTS ' "${tmp}/restore.sql"
grep -q '^CREATE UNIQUE INDEX IF NOT EXISTS ' "${tmp}/restore.sql"
grep -q '^DO \$restore\$ BEGIN IF NOT EXISTS ' "${tmp}/restore.sql"

echo "ok: migration 114 restore preflight is secret-safe and generates a restartable artifact (${drop_count} indexes)"
