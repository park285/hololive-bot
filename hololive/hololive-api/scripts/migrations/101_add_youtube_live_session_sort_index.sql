CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_yls_ended_sort_video
    ON youtube_live_sessions ((COALESCE(ended_at, started_at, scheduled_start_time, last_seen_at)) DESC, video_id DESC)
    WHERE status = 'ENDED';
