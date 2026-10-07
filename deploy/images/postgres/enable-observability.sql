-- 관리자 전용 활성화입니다. 이미지 설치만으로 운영 DB나 preload 설정을 바꾸지 않습니다.
-- 기존 volume에는 init-db가 재실행되지 않으므로 승인된 재시작 뒤 이 파일을 적용합니다.
\set ON_ERROR_STOP on

BEGIN;

DO $preload$
DECLARE
    required_library text;
BEGIN
    FOREACH required_library IN ARRAY ARRAY['pg_stat_statements', 'pg_stat_kcache', 'pg_wait_sampling'] LOOP
        IF NOT EXISTS (
            SELECT 1
            FROM unnest(string_to_array(current_setting('shared_preload_libraries'), ',')) AS configured(name)
            WHERE btrim(configured.name) = required_library
        ) THEN
            RAISE EXCEPTION 'required library is not preloaded: %', required_library;
        END IF;
    END LOOP;
END
$preload$;

CREATE EXTENSION IF NOT EXISTS pg_stat_statements WITH SCHEMA public;
-- public USAGE가 없는 모니터도 앱 객체 접근을 넓히지 않고 새 통계만 읽도록 분리합니다.
CREATE SCHEMA IF NOT EXISTS hololive_observability;
DO $owner$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_namespace
                   WHERE nspname='hololive_observability' AND nspowner=current_user::regrole) THEN
        RAISE EXCEPTION 'observability schema must be owned by the activating administrator';
    END IF;
END
$owner$;
REVOKE ALL ON SCHEMA hololive_observability FROM PUBLIC;
GRANT USAGE ON SCHEMA hololive_observability TO pg_read_all_stats;
CREATE EXTENSION IF NOT EXISTS pg_stat_kcache WITH SCHEMA hololive_observability;
CREATE EXTENSION IF NOT EXISTS pg_wait_sampling WITH SCHEMA hololive_observability;

DO $versions$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_extension
        WHERE extname = 'pg_stat_kcache' AND extversion = '2.3.2' AND extnamespace = 'hololive_observability'::regnamespace
    ) OR NOT EXISTS (
        SELECT 1 FROM pg_extension
        WHERE extname = 'pg_wait_sampling' AND extversion = '1.1' AND extnamespace = 'hololive_observability'::regnamespace
    ) THEN
        RAISE EXCEPTION 'unexpected observability extension version or schema; review before upgrading';
    END IF;
END
$versions$;

-- 새 통계는 기존 pg_read_all_stats 구성원과 관리자만 읽습니다. 앱 역할 권한은 넓히지 않습니다.
REVOKE ALL ON FUNCTION hololive_observability.pg_stat_kcache() FROM PUBLIC;
REVOKE ALL ON FUNCTION hololive_observability.pg_stat_kcache_reset() FROM PUBLIC;
REVOKE ALL ON TABLE hololive_observability.pg_stat_kcache, hololive_observability.pg_stat_kcache_detail FROM PUBLIC;
GRANT EXECUTE ON FUNCTION hololive_observability.pg_stat_kcache() TO pg_read_all_stats;
GRANT SELECT ON TABLE hololive_observability.pg_stat_kcache, hololive_observability.pg_stat_kcache_detail TO pg_read_all_stats;

REVOKE ALL ON FUNCTION hololive_observability.pg_wait_sampling_get_current(integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION hololive_observability.pg_wait_sampling_get_history() FROM PUBLIC;
REVOKE ALL ON FUNCTION hololive_observability.pg_wait_sampling_get_profile() FROM PUBLIC;
REVOKE ALL ON FUNCTION hololive_observability.pg_wait_sampling_reset_profile() FROM PUBLIC;
REVOKE ALL ON TABLE hololive_observability.pg_wait_sampling_current, hololive_observability.pg_wait_sampling_history, hololive_observability.pg_wait_sampling_profile FROM PUBLIC;
GRANT EXECUTE ON FUNCTION hololive_observability.pg_wait_sampling_get_current(integer), hololive_observability.pg_wait_sampling_get_history(), hololive_observability.pg_wait_sampling_get_profile() TO pg_read_all_stats;
GRANT SELECT ON TABLE hololive_observability.pg_wait_sampling_current, hololive_observability.pg_wait_sampling_history, hololive_observability.pg_wait_sampling_profile TO pg_read_all_stats;

-- 관리자 소유 view가 기존 public 통계를 참조합니다. 모니터에게 public USAGE를 부여하지 않습니다.
CREATE OR REPLACE VIEW hololive_observability.statement_info AS
SELECT stats_reset AS statements_reset_at, dealloc AS statements_deallocations
FROM public.pg_stat_statements_info;

CREATE OR REPLACE VIEW hololive_observability.statement_resources AS
SELECT s.queryid, s.userid, s.toplevel, s.calls, s.total_exec_time AS statement_exec_ms,
       k.exec_user_time + k.exec_system_time AS kernel_cpu_seconds,
       k.exec_reads AS kernel_read_bytes, k.exec_writes AS kernel_write_bytes,
       k.exec_nvcsws AS voluntary_context_switches, k.exec_nivcsws AS involuntary_context_switches
FROM hololive_observability.pg_stat_kcache() k
JOIN public.pg_stat_statements s
  ON (k.queryid,k.dbid,k.userid,k.top) = (s.queryid,s.dbid,s.userid,s.toplevel)
WHERE k.dbid = (SELECT oid FROM pg_database WHERE datname=current_database()) AND k.top;

REVOKE ALL ON TABLE hololive_observability.statement_info, hololive_observability.statement_resources FROM PUBLIC;
GRANT SELECT ON TABLE hololive_observability.statement_info, hololive_observability.statement_resources TO pg_read_all_stats;

COMMIT;
