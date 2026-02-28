#!/bin/sh
set -eu

# Role / DB defaults
POSTGRES_ADMIN_USER="${POSTGRES_ADMIN_USER:-postgres_admin}"
POSTGRES_ADMIN_PASSWORD="${POSTGRES_ADMIN_PASSWORD:-${POSTGRES_PASSWORD:-}}"

HOLOLIVE_DB_NAME="${HOLOLIVE_DB_NAME:-hololive}"
HOLOLIVE_DB_USER="${HOLOLIVE_DB_USER:-hololive_runtime}"
HOLOLIVE_MIGRATOR_USER="${HOLOLIVE_MIGRATOR_USER:-hololive_migrator}"
HOLOLIVE_SCRAPER_USER="${HOLOLIVE_SCRAPER_USER:-hololive_scraper}"

DB_PASSWORD_FALLBACK="${DB_PASSWORD:-${POSTGRES_PASSWORD:-}}"

HOLOLIVE_DB_PASSWORD="${HOLOLIVE_DB_PASSWORD:-$DB_PASSWORD_FALLBACK}"
HOLOLIVE_MIGRATOR_PASSWORD="${HOLOLIVE_MIGRATOR_PASSWORD:-$DB_PASSWORD_FALLBACK}"
HOLOLIVE_SCRAPER_PASSWORD="${HOLOLIVE_SCRAPER_PASSWORD:-$DB_PASSWORD_FALLBACK}"

if [ -z "${HOLOLIVE_DB_PASSWORD}" ]; then
  echo "DB password is empty. Set DB_PASSWORD or per-role passwords." >&2
  exit 1
fi

if [ -z "${POSTGRES_ADMIN_PASSWORD}" ]; then
  echo "POSTGRES_ADMIN_PASSWORD/POSTGRES_PASSWORD is required." >&2
  exit 1
fi

echo "Initializing hololive Postgres roles/databases with least privilege..."

psql -v ON_ERROR_STOP=1 \
  --username "${POSTGRES_USER}" \
  --dbname "postgres" \
  --set=postgres_admin_user="${POSTGRES_ADMIN_USER}" \
  --set=postgres_admin_password="${POSTGRES_ADMIN_PASSWORD}" \
  --set=hololive_db="${HOLOLIVE_DB_NAME}" \
  --set=hololive_user="${HOLOLIVE_DB_USER}" \
  --set=hololive_user_password="${HOLOLIVE_DB_PASSWORD}" \
  --set=hololive_migrator="${HOLOLIVE_MIGRATOR_USER}" \
  --set=hololive_migrator_password="${HOLOLIVE_MIGRATOR_PASSWORD}" \
  --set=hololive_scraper="${HOLOLIVE_SCRAPER_USER}" \
  --set=hololive_scraper_password="${HOLOLIVE_SCRAPER_PASSWORD}" <<'EOSQL'
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

echo "Hololive Postgres hardening completed (roles/databases/privileges)."
