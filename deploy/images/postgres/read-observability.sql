\set ON_ERROR_STOP on
SET default_transaction_read_only = on;
SHOW transaction_read_only;
BEGIN;
SET LOCAL statement_timeout = '5s';
SET LOCAL lock_timeout = '1s';

SELECT clock_timestamp() AS sampled_at, pg_postmaster_start_time() AS server_started_at,
       statements_reset_at, statements_deallocations
FROM hololive_observability.statement_info;

SELECT * FROM hololive_observability.statement_resources
ORDER BY kernel_cpu_seconds DESC
LIMIT 30;

SELECT 'cluster' AS scope, count(*) AS profile_entries FROM hololive_observability.pg_wait_sampling_profile;
SELECT queryid, event_type, event, sum(count) AS samples
FROM hololive_observability.pg_wait_sampling_profile
WHERE queryid <> 0
GROUP BY queryid,event_type,event
ORDER BY sum(count) DESC
LIMIT 30;
COMMIT;
