-- 086_add_sending_stale_indexes.sql
-- stale-SENDING 회수 스윕이 30초 주기로 status='SENDING' 범위를 스캔하는데
-- 두 delivery 테이블 모두 SENDING partial index가 없어 테이블 크기에 비례한 스캔이 된다.
-- 각 스윕의 WHERE+ORDER BY(sending_started_at,id / locked_at,id)에 정확히 맞춘다.
-- CONCURRENTLY: apply-all.sh가 파일 단위 autocommit으로 적용하고 invalid 잔재는 runner가 감지·DROP한다.

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ndo_sending_stale
    ON notification_delivery_outbox (sending_started_at, id)
    WHERE status = 'SENDING';

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ynd_sending_stale
    ON youtube_notification_delivery (locked_at, id)
    WHERE status = 'SENDING';
