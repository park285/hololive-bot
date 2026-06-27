CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_youtube_stats_history_channel_time 
ON youtube_stats_history (channel_id, time DESC);
