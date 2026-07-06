CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_yls_status_topic_last_seen
    ON youtube_live_sessions (status, topic_id, last_seen_at DESC)
    WHERE topic_id <> '';
