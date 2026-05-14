ALTER TABLE youtube_live_sessions
    ADD COLUMN IF NOT EXISTS live_first_seen_at TIMESTAMPTZ;

UPDATE youtube_live_sessions
SET live_first_seen_at = COALESCE(live_first_seen_at, last_seen_at)
WHERE status = 'LIVE';

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_yls_live_first_seen
    ON youtube_live_sessions (live_first_seen_at, channel_id)
    WHERE status = 'LIVE';

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_alarm_dispatch_deliveries_sent_event_room
    ON alarm_dispatch_deliveries (event_id, room_id, sent_at DESC)
    WHERE status = 'sent' AND sent_at IS NOT NULL;

COMMENT ON COLUMN youtube_live_sessions.live_first_seen_at IS 'First time this video was observed as LIVE by the live poller.';
COMMENT ON INDEX idx_yls_live_first_seen IS 'Supports YouTube live guardrail grace checks by first LIVE observation time.';
COMMENT ON INDEX idx_alarm_dispatch_deliveries_sent_event_room IS 'Supports YouTube live guardrail lookup of sent rooms by dispatch event.';
