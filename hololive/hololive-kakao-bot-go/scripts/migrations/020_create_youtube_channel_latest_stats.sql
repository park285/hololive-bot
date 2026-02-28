-- Migration 020: Create youtube_channel_latest_stats snapshot table
-- This table stores the most recent statistics for each YouTube channel to optimize queries
-- that previously relied on DISTINCT ON or ORDER BY/LIMIT 1 on the large history table.

CREATE TABLE IF NOT EXISTS youtube_channel_latest_stats (
    channel_id VARCHAR(255) PRIMARY KEY,
    member_name TEXT,
    subscribers BIGINT,
    videos BIGINT,
    views BIGINT,
    time TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Initialize table with latest data from history table
-- Using the newly created index idx_youtube_stats_channel_time from migration 019
INSERT INTO youtube_channel_latest_stats (channel_id, member_name, subscribers, videos, views, time)
SELECT DISTINCT ON (channel_id)
    channel_id, member_name, subscribers, videos, views, time
FROM youtube_stats_history
ORDER BY channel_id, time DESC
ON CONFLICT (channel_id) DO NOTHING;
