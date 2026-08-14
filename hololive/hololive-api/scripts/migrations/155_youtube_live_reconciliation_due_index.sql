CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_youtube_live_reconciliation_due
    ON youtube_live_reconciliation_heads (next_end_check_at, video_id)
    WHERE next_end_check_at IS NOT NULL;
