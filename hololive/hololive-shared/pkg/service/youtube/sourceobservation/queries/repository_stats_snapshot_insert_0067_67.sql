INSERT INTO youtube_channel_stats_snapshots (
    channel_id, captured_at, subscriber_count, view_count, video_count
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (channel_id, captured_at) DO NOTHING
