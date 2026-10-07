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
CREATE EXTENSION IF NOT EXISTS pg_stat_kcache WITH SCHEMA public;
CREATE EXTENSION IF NOT EXISTS pg_wait_sampling WITH SCHEMA public;

DO $versions$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_extension
        WHERE extname = 'pg_stat_kcache' AND extversion = '2.3.2' AND extnamespace = 'public'::regnamespace
    ) OR NOT EXISTS (
        SELECT 1 FROM pg_extension
        WHERE extname = 'pg_wait_sampling' AND extversion = '1.1' AND extnamespace = 'public'::regnamespace
    ) THEN
        RAISE EXCEPTION 'unexpected observability extension version or schema; review before upgrading';
    END IF;
END
$versions$;

-- 새 통계는 기존 pg_read_all_stats 구성원과 관리자만 읽습니다. 앱 역할 권한은 넓히지 않습니다.
REVOKE ALL ON FUNCTION public.pg_stat_kcache() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.pg_stat_kcache_reset() FROM PUBLIC;
REVOKE ALL ON TABLE public.pg_stat_kcache, public.pg_stat_kcache_detail FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.pg_stat_kcache() TO pg_read_all_stats;
GRANT SELECT ON TABLE public.pg_stat_kcache, public.pg_stat_kcache_detail TO pg_read_all_stats;

REVOKE ALL ON FUNCTION public.pg_wait_sampling_get_current(integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.pg_wait_sampling_get_history() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.pg_wait_sampling_get_profile() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.pg_wait_sampling_reset_profile() FROM PUBLIC;
REVOKE ALL ON TABLE public.pg_wait_sampling_current, public.pg_wait_sampling_history, public.pg_wait_sampling_profile FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.pg_wait_sampling_get_current(integer), public.pg_wait_sampling_get_history(), public.pg_wait_sampling_get_profile() TO pg_read_all_stats;
GRANT SELECT ON TABLE public.pg_wait_sampling_current, public.pg_wait_sampling_history, public.pg_wait_sampling_profile TO pg_read_all_stats;

COMMIT;
