SELECT video_id, channel_id, status, title, topic_id, thumbnail_url,
       scheduled_start_time, started_at, ended_at, live_first_seen_at, last_seen_at
FROM youtube_live_sessions
WHERE video_id = $1
FOR UPDATE
