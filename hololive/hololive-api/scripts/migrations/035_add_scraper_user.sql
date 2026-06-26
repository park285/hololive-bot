-- 035: hololive_scraper role 최소 권한 부여
-- 목적: Rust scraper 서비스 전용 DB 사용자 권한 세팅

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        -- major_events: 스크래퍼가 upsert/상태 갱신 수행
        GRANT SELECT, INSERT, UPDATE ON TABLE major_events TO hololive_scraper;
        GRANT USAGE, SELECT ON SEQUENCE major_events_id_seq TO hololive_scraper;

        -- major_event_subscriptions: 읽기 전용
        GRANT SELECT ON TABLE major_event_subscriptions TO hololive_scraper;
    ELSE
        RAISE NOTICE 'Role hololive_scraper does not exist, skipping GRANT';
    END IF;
END $$;

-- Rollback:
-- REVOKE ALL PRIVILEGES ON TABLE major_events FROM hololive_scraper;
-- REVOKE ALL PRIVILEGES ON SEQUENCE major_events_id_seq FROM hololive_scraper;
-- REVOKE ALL PRIVILEGES ON TABLE major_event_subscriptions FROM hololive_scraper;
