\set ON_ERROR_STOP on
BEGIN;
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';
DROP VIEW IF EXISTS hololive_observability.statement_resources RESTRICT;
DROP VIEW IF EXISTS hololive_observability.statement_info RESTRICT;
DROP EXTENSION IF EXISTS amcheck RESTRICT;
DROP EXTENSION IF EXISTS pg_buffercache RESTRICT;
DROP EXTENSION IF EXISTS pg_wait_sampling RESTRICT;
DROP EXTENSION IF EXISTS pg_stat_kcache RESTRICT;
DROP SCHEMA IF EXISTS hololive_observability RESTRICT;
COMMIT;
