SELECT video_id, channel_id, status, title, scheduled_start_time, started_at, ended_at, live_first_seen_at, last_seen_at
FROM youtube_live_sessions
WHERE channel_id = ANY($1::text[])
   OR video_id = ANY($2::text[])
FOR UPDATE
