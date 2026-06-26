-- ============================================================================
-- 021: alarms / youtube_notification_outbox 쿼리 패턴 인덱스 최적화
-- 목적:
--   1) alarms 조회의 WHERE + ORDER BY(created_at) 패턴 최적화
--   2) outbox 큐 클레임/정리 쿼리의 부분 인덱스 최적화
-- ============================================================================

-- alarms: FindByUser(room_id, user_id) + ORDER BY created_at
CREATE INDEX IF NOT EXISTS idx_alarms_room_user_created
    ON alarms (room_id, user_id, created_at);

-- alarms: FindByChannel(channel_id) + ORDER BY created_at
CREATE INDEX IF NOT EXISTS idx_alarms_channel_created
    ON alarms (channel_id, created_at);

-- alarms: GetMemberName / GetAllMemberNames (member_name 존재 행만 대상)
CREATE INDEX IF NOT EXISTS idx_alarms_channel_member_latest
    ON alarms (channel_id, created_at DESC)
    WHERE member_name IS NOT NULL AND member_name <> '';

-- outbox: fetchAndLock (status='PENDING', next_attempt_at <= now, ORDER BY created_at)
CREATE INDEX IF NOT EXISTS idx_yno_pending_next_created
    ON youtube_notification_outbox (next_attempt_at, created_at)
    WHERE status = 'PENDING';

-- outbox: cleanup (status='SENT' AND sent_at < cutoff)
CREATE INDEX IF NOT EXISTS idx_yno_sent_cleanup
    ON youtube_notification_outbox (sent_at)
    WHERE status = 'SENT';

ANALYZE alarms;
ANALYZE youtube_notification_outbox;
