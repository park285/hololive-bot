SELECT video_id, channel_id, status, COALESCE(title, '') AS title, topic_id, thumbnail_url,
       scheduled_start_time, started_at, ended_at, live_first_seen_at, last_seen_at,
       is_premiere, lifecycle_origin, status_observed_at, schedule_observed_at
FROM youtube_live_sessions
WHERE channel_id = ANY($1::text[])
   OR video_id = ANY($2::text[])
ORDER BY video_id
FOR UPDATE
