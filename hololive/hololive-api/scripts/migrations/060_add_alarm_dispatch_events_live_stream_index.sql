CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_alarm_dispatch_events_live_stream_created
    ON alarm_dispatch_events (stream_id, created_at DESC)
    WHERE alarm_type = 'LIVE';

COMMENT ON INDEX idx_alarm_dispatch_events_live_stream_created IS 'Supports YouTube live guardrail lookup by stream_id over recent LIVE dispatch events.';
