BEGIN;
SET LOCAL lock_timeout = '3s';

-- migrator의 기본 ACL은 새 테이블에 runtime CRUD를 부여합니다. payload 발행은 scraper,
-- 회수와 참조 잠금은 기존 SECURITY DEFINER 함수가 소유하므로 직접 변경 권한을 남기지 않습니다.
REVOKE ALL ON TABLE public.source_observation_payloads FROM PUBLIC;
REVOKE ALL ON TABLE public.source_observation_payload_gc_state FROM PUBLIC;
REVOKE ALL ON SEQUENCE public.source_observation_payloads_id_seq FROM PUBLIC;

DO $roles$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE ALL ON TABLE public.source_observation_payloads FROM hololive_runtime;
        GRANT SELECT ON TABLE public.source_observation_payloads TO hololive_runtime;
        REVOKE ALL ON TABLE public.source_observation_payload_gc_state FROM hololive_runtime;
        REVOKE ALL ON SEQUENCE public.source_observation_payloads_id_seq FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE ALL ON TABLE public.source_observation_payloads FROM hololive_scraper;
        GRANT SELECT, INSERT ON TABLE public.source_observation_payloads TO hololive_scraper;
        REVOKE ALL ON TABLE public.source_observation_payload_gc_state FROM hololive_scraper;
        REVOKE ALL ON SEQUENCE public.source_observation_payloads_id_seq FROM hololive_scraper;
        GRANT USAGE, SELECT ON SEQUENCE public.source_observation_payloads_id_seq TO hololive_scraper;
    END IF;
END
$roles$;

COMMIT;
