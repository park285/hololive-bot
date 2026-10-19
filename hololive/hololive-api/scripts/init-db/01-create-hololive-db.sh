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
set +x

# Fresh-PGDATA-only bootstrap uses psql role SQL. Never log those credential-bearing
# statements, even when production slow/error logging is enabled on the server.
# The ordinary Go migrator separately generates SCRAM verifiers client-side.
PGOPTIONS="${PGOPTIONS:+$PGOPTIONS }-c log_statement=none -c log_min_duration_statement=-1 \
-c log_min_duration_sample=-1 -c log_transaction_sample_rate=0 \
-c log_min_error_statement=panic -c log_min_messages=panic \
-c log_parameter_max_length=0 -c log_parameter_max_length_on_error=0 \
-c debug_print_parse=off -c debug_print_rewritten=off -c debug_print_plan=off \
-c auto_explain.log_min_duration=-1 -c pg_stat_statements.track=none \
-c track_activities=off -c password_encryption=scram-sha-256 -c client_encoding=UTF8"
export PGOPTIONS
export PGAPPNAME=hololive-init-db

POSTGRES_ADMIN_USER="${POSTGRES_ADMIN_USER:-postgres_admin}"
POSTGRES_ADMIN_PASSWORD="${POSTGRES_ADMIN_PASSWORD:-${POSTGRES_PASSWORD:-}}"

HOLOLIVE_DB_NAME="${HOLOLIVE_DB_NAME:-hololive}"
HOLOLIVE_MIGRATOR_USER="${HOLOLIVE_MIGRATOR_USER:-hololive_migrator}"

# 역할별 비밀번호는 모두 필수다. 비어 있을 때 admin DB_PASSWORD로 역할을 만들던 폴백은 역할 분리를 깨므로
# 두지 않는다(stack audit 2026-09-26, T18에서 중앙 compose.env에 세 키 확인).
HOLOLIVE_DB_PASSWORD="${HOLOLIVE_DB_PASSWORD:-}"
HOLOLIVE_MIGRATOR_PASSWORD="${HOLOLIVE_MIGRATOR_PASSWORD:-}"
HOLOLIVE_SCRAPER_PASSWORD="${HOLOLIVE_SCRAPER_PASSWORD:-}"

HOLOLIVE_REPLICATOR_USER="${HOLOLIVE_REPLICATOR_USER:-hololive_replicator}"
HOLOLIVE_REPLICATOR_PASSWORD="${HOLOLIVE_REPLICATOR_PASSWORD:-}"
export POSTGRES_ADMIN_PASSWORD HOLOLIVE_DB_PASSWORD HOLOLIVE_MIGRATOR_PASSWORD
export HOLOLIVE_SCRAPER_PASSWORD HOLOLIVE_REPLICATOR_PASSWORD

require_role_password() {
  if [ -z "$2" ]; then
    echo "$1 is required." >&2
    exit 1
  fi
}

require_role_password HOLOLIVE_DB_PASSWORD "${HOLOLIVE_DB_PASSWORD}"
require_role_password HOLOLIVE_MIGRATOR_PASSWORD "${HOLOLIVE_MIGRATOR_PASSWORD}"
require_role_password HOLOLIVE_SCRAPER_PASSWORD "${HOLOLIVE_SCRAPER_PASSWORD}"

if [ -z "${POSTGRES_ADMIN_PASSWORD}" ]; then
  echo "POSTGRES_ADMIN_PASSWORD/POSTGRES_PASSWORD is required." >&2
  exit 1
fi

echo "Initializing hololive Postgres roles/databases with least privilege..."

psql -X -v ON_ERROR_STOP=1 -v ECHO=none -v ECHO_HIDDEN=off -v VERBOSITY=sqlstate \
  --username "${POSTGRES_USER}" \
  --dbname "postgres" \
  --set=postgres_admin_user="${POSTGRES_ADMIN_USER}" \
  --set=hololive_db="${HOLOLIVE_DB_NAME}" \
  --set=hololive_user=hololive_runtime \
  --set=hololive_migrator="${HOLOLIVE_MIGRATOR_USER}" \
  --set=hololive_scraper=hololive_scraper <<'EOSQL'
\getenv postgres_admin_password POSTGRES_ADMIN_PASSWORD
\getenv hololive_user_password HOLOLIVE_DB_PASSWORD
\getenv hololive_migrator_password HOLOLIVE_MIGRATOR_PASSWORD
\getenv hololive_scraper_password HOLOLIVE_SCRAPER_PASSWORD

SELECT format(
  'CREATE ROLE %I LOGIN SUPERUSER CREATEDB CREATEROLE INHERIT PASSWORD %L',
  :'postgres_admin_user',
  :'postgres_admin_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'postgres_admin_user') \gexec

SELECT format(
  'ALTER ROLE %I WITH LOGIN SUPERUSER CREATEDB CREATEROLE INHERIT PASSWORD %L',
  :'postgres_admin_user',
  :'postgres_admin_password'
) \gexec

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'hololive_user', :'hololive_user_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'hololive_user') \gexec

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'hololive_migrator', :'hololive_migrator_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'hololive_migrator') \gexec

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'hololive_scraper', :'hololive_scraper_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'hololive_scraper') \gexec

SELECT format(
  'ALTER ROLE %I WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION INHERIT PASSWORD %L',
  :'hololive_user', :'hololive_user_password'
) \gexec

SELECT format(
  'ALTER ROLE %I WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION INHERIT PASSWORD %L',
  :'hololive_migrator', :'hololive_migrator_password'
) \gexec

SELECT format(
  'ALTER ROLE %I WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION INHERIT PASSWORD %L',
  :'hololive_scraper', :'hololive_scraper_password'
) \gexec

-- Aggregate role limits, not per-process limits; runtime includes 3 control slots.
-- Keep aligned with scripts/ci/postgres-capacity-policy.tsv and role-limits.sql.
SELECT format('ALTER ROLE %I CONNECTION LIMIT 25', :'hololive_user') \gexec
SELECT format('ALTER ROLE %I CONNECTION LIMIT 1', :'hololive_migrator') \gexec
SELECT format('ALTER ROLE %I CONNECTION LIMIT 28', :'hololive_scraper') \gexec

SELECT format('CREATE DATABASE %I OWNER %I', :'hololive_db', :'hololive_migrator')
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = :'hololive_db') \gexec

SELECT format('ALTER DATABASE %I OWNER TO %I', :'hololive_db', :'hololive_migrator') \gexec

SELECT format('REVOKE ALL ON DATABASE %I FROM PUBLIC', :'hololive_db') \gexec

SELECT format(
  'GRANT CONNECT ON DATABASE %I TO %I, %I, %I',
  :'hololive_db', :'hololive_user', :'hololive_migrator', :'hololive_scraper'
) \gexec

\connect :hololive_db
REVOKE ALL ON SCHEMA public FROM PUBLIC;
SELECT format('REVOKE ALL ON SCHEMA public FROM %I', :'hololive_user') \gexec
SELECT format('REVOKE ALL ON SCHEMA public FROM %I', :'hololive_migrator') \gexec
SELECT format('REVOKE ALL ON SCHEMA public FROM %I', :'hololive_scraper') \gexec
SELECT format('GRANT USAGE ON SCHEMA public TO %I', :'hololive_user') \gexec
SELECT format('GRANT USAGE, CREATE ON SCHEMA public TO %I', :'hololive_migrator') \gexec
SELECT format('GRANT USAGE ON SCHEMA public TO %I', :'hololive_scraper') \gexec

SELECT format(
  'ALTER %s %I.%I OWNER TO %I',
  CASE c.relkind
    WHEN 'v' THEN 'VIEW'
    WHEN 'm' THEN 'MATERIALIZED VIEW'
    ELSE 'TABLE'
  END,
  n.nspname,
  c.relname,
  :'hololive_migrator'
)
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public'
  AND c.relkind IN ('r','p','v','m') \gexec

SELECT format('REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM %I', :'hololive_user') \gexec
SELECT format('REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM %I', :'hololive_user') \gexec
SELECT format('REVOKE ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA public FROM %I', :'hololive_user') \gexec
SELECT format('GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO %I', :'hololive_user') \gexec
SELECT format('GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO %I', :'hololive_user') \gexec
SELECT format('GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO %I', :'hololive_user') \gexec

SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public REVOKE ALL ON TABLES FROM PUBLIC', :'hololive_migrator') \gexec
SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public REVOKE ALL ON SEQUENCES FROM PUBLIC', :'hololive_migrator') \gexec
SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public REVOKE ALL ON FUNCTIONS FROM PUBLIC', :'hololive_migrator') \gexec
SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %I', :'hololive_migrator', :'hololive_user') \gexec
SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO %I', :'hololive_migrator', :'hololive_user') \gexec
SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO %I', :'hololive_migrator', :'hololive_user') \gexec
EOSQL

if [ -n "${HOLOLIVE_REPLICATOR_PASSWORD}" ]; then
  psql -X -v ON_ERROR_STOP=1 -v ECHO=none -v ECHO_HIDDEN=off -v VERBOSITY=sqlstate \
    --username "${POSTGRES_USER}" \
    --dbname "postgres" \
    --set=hololive_db="${HOLOLIVE_DB_NAME}" \
    --set=hololive_replicator="${HOLOLIVE_REPLICATOR_USER}" <<'EOSQL'
\getenv hololive_replicator_password HOLOLIVE_REPLICATOR_PASSWORD

SELECT format('CREATE ROLE %I LOGIN REPLICATION PASSWORD %L', :'hololive_replicator', :'hololive_replicator_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'hololive_replicator') \gexec

SELECT format(
  'ALTER ROLE %I WITH LOGIN REPLICATION NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT PASSWORD %L',
  :'hololive_replicator', :'hololive_replicator_password'
) \gexec

SELECT format('GRANT CONNECT ON DATABASE %I TO %I', :'hololive_db', :'hololive_replicator') \gexec

\connect :hololive_db
SELECT format(
  'GRANT EXECUTE ON FUNCTION pg_catalog.pg_promote(boolean, integer) TO %I',
  :'hololive_replicator'
) \gexec
EOSQL
  echo "Streaming replication role provisioned."
fi

echo "Hololive Postgres hardening completed (roles/databases/privileges)."
