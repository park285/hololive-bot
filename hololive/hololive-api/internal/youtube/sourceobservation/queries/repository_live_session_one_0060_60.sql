SELECT video_id, channel_id, status, COALESCE(title, '') AS title, topic_id, thumbnail_url,
       scheduled_start_time, started_at, ended_at, live_first_seen_at, last_seen_at,
       is_premiere, lifecycle_origin, status_observed_at, schedule_observed_at, title_observed_at
FROM youtube_live_sessions
WHERE video_id = $1
FOR UPDATE
