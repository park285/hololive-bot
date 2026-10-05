SELECT video_id, channel_id, status, COALESCE(title, '') AS title,
       scheduled_start_time, last_seen_at, schedule_observed_at, title_observed_at
FROM youtube_live_sessions
WHERE video_id = ANY($1::text[])
ORDER BY video_id
FOR UPDATE
