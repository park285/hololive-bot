SELECT video_id, channel_id, COALESCE(title, '') AS title, status,
       scheduled_start_time, started_at
FROM youtube_live_sessions
WHERE channel_id = ANY($1)
  AND ((status = 'UPCOMING' AND scheduled_start_time >= $2 AND scheduled_start_time < $3)
    OR (status = 'LIVE' AND COALESCE(started_at, scheduled_start_time) >= $2 AND COALESCE(started_at, scheduled_start_time) < $3))
  AND last_seen_at >= $4
ORDER BY COALESCE(started_at, scheduled_start_time), video_id
