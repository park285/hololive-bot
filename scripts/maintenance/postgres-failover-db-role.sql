\set ON_ERROR_STOP on

SELECT EXISTS (
  SELECT 1
  FROM pg_roles
  WHERE rolname = :'failover_user'
    AND rolcanlogin
    AND rolreplication
    AND NOT rolsuper
    AND NOT rolcreaterole
    AND NOT rolcreatedb
) AS failover_role_valid \gset

\if :failover_role_valid
\else
  \echo 'failover role is missing or overprivileged' >&2
  \quit 1
\endif

SELECT format('GRANT CONNECT ON DATABASE %I TO %I', :'failover_database', :'failover_user') \gexec
\connect :failover_database
SELECT format(
  'GRANT EXECUTE ON FUNCTION pg_catalog.pg_promote(boolean, integer) TO %I',
  :'failover_user'
) \gexec
