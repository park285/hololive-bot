-- pg18_db_usage_optional_concurrent_indexes.sql
-- 이 파일은 manifest migration이 아닙니다.
-- 운영 중 대형 인덱스 후보를 추가해야 할 때 psql에서 수동 실행하세요.
-- CREATE INDEX CONCURRENTLY는 transaction block 안에서 실행하면 안 됩니다.
-- 실행 전 기준:
--   1) pg_stat_statements mean_exec_time > 5ms
--   2) EXPLAIN (ANALYZE, BUFFERS)에서 Rows Removed by Filter > 1000
--   3) pending backlog가 10,000 rows 이상 유지
--   4) pg_stat_io relation read 증가

-- youtube_notification_delivery/outbox claim 인덱스는 migration 067_align_claim_index_due_first.sql에서
-- 기존 prefix 인덱스를 (next_attempt_at, created_at, id) 완전 매칭으로 정식 교체했다(여기서 수동 생성하지 않음).

-- alarm_dispatch_deliveries terminal retention 인덱스는 migration 059_harden_alarm_dispatch_outbox.sql이
-- 이미 idx_alarm_dispatch_deliveries_{sent,dlq,quarantined,cancelled}_retention로 동일 정의를 제공하므로
-- 여기서 재생성하지 않습니다. (CREATE INDEX ... IF NOT EXISTS는 이름만 비교 → 다른 이름이면 중복 인덱스가 생김)

-- youtube_notification_delivery_telemetry retention 인덱스는 migration
-- 096_sql_integrity_retention_followups.sql이 실제 삭제 술어(logged_at IS NOT NULL AND event_at < cutoff)와
-- 일치하는 idx_ydt_logged_event_retention (event_at, id) WHERE logged_at IS NOT NULL로 제공하므로
-- 여기서 수동 생성하지 않습니다. (과거 안내였던 idx_ydt_logged_retention(logged_at ASC, id ASC)은
-- 술어 컬럼이 달라 retention DELETE에 쓰이지 않는 잘못된 권고였음)

-- telemetry retention 예시. 실제 보관 기간은 운영 정책에 맞춰 조정하세요.
-- WITH picked AS (
--     SELECT id
--     FROM youtube_notification_delivery_telemetry
--     WHERE logged_at IS NOT NULL
--       AND event_at < NOW() - INTERVAL '90 days'
--     ORDER BY event_at ASC, id ASC
--     LIMIT 1000
-- )
-- DELETE FROM youtube_notification_delivery_telemetry t
-- USING picked
-- WHERE t.id = picked.id;
