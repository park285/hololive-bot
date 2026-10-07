-- SQL 본문이나 앱 payload를 출력하지 않는 누적 통계 snapshot입니다.
\set ON_ERROR_STOP on
SET default_transaction_read_only = on;
SHOW transaction_read_only;
BEGIN;
SET LOCAL statement_timeout = '5s';
SET LOCAL lock_timeout = '1s';

SELECT clock_timestamp() AS sampled_at, pg_postmaster_start_time() AS server_started_at,
       stats_reset AS statements_reset_at, dealloc AS statements_deallocations
FROM public.pg_stat_statements_info;

SELECT s.queryid, s.userid, s.toplevel, s.calls, s.total_exec_time AS statement_exec_ms,
       k.exec_user_time + k.exec_system_time AS kernel_cpu_seconds,
       k.exec_reads AS kernel_read_bytes, k.exec_writes AS kernel_write_bytes,
       k.exec_nvcsws AS voluntary_context_switches, k.exec_nivcsws AS involuntary_context_switches
FROM public.pg_stat_kcache() k
JOIN public.pg_stat_statements s
  ON (k.queryid,k.dbid,k.userid,k.top) = (s.queryid,s.dbid,s.userid,s.toplevel)
WHERE k.dbid = (SELECT oid FROM pg_database WHERE datname=current_database()) AND k.top
ORDER BY k.exec_user_time + k.exec_system_time DESC
LIMIT 30;

-- wait profile에는 dbid/userid가 없으므로 다른 통계와 무조건 join하지 않습니다.
SELECT 'cluster' AS scope, count(*) AS profile_entries FROM public.pg_wait_sampling_profile;
SELECT queryid, event_type, event, sum(count) AS samples
FROM public.pg_wait_sampling_profile
WHERE queryid <> 0
GROUP BY queryid,event_type,event
ORDER BY sum(count) DESC
LIMIT 30;
COMMIT;
