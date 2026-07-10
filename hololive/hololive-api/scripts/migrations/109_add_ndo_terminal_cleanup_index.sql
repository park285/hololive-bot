-- 109_add_ndo_terminal_cleanup_index.sql
-- retention DELETE의 predicate가 status IN ('SENT','FAILED','QUARANTINED')로 확장되어
-- 구 partial index(idx_ndo_sent_cleanup, 'SENT','FAILED')로는 커버되지 않는다. 110이 구 인덱스를 제거한다.

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ndo_terminal_cleanup
    ON notification_delivery_outbox (COALESCE(sent_at, created_at))
    WHERE status IN ('SENT', 'FAILED', 'QUARANTINED');
