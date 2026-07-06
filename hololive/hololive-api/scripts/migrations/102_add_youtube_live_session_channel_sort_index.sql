CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_yls_ended_channel_sort_video
    ON youtube_live_sessions (channel_id, (COALESCE(ended_at, started_at, scheduled_start_time, last_seen_at)) DESC, video_id DESC)
    WHERE status = 'ENDED';
