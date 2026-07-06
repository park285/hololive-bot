
		SELECT video_id, channel_id, status, title, scheduled_start_time, started_at, ended_at,
		       live_first_seen_at, topic_id, thumbnail_url, last_seen_at
		FROM youtube_live_sessions
		WHERE channel_id = ANY($1)
		  AND (
		      (status = $2 AND last_seen_at >= $3)
		      OR (status = $4 AND scheduled_start_time >= $5 AND scheduled_start_time <= $6 AND last_seen_at >= $7)
		  )
		ORDER BY last_seen_at DESC
	