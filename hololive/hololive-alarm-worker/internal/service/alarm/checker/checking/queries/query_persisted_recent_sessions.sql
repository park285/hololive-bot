
		SELECT video_id, channel_id, status, title, scheduled_start_time, started_at, ended_at,
		       live_first_seen_at, topic_id, thumbnail_url, is_premiere, status_observed_at, schedule_observed_at
		FROM youtube_live_sessions
		WHERE channel_id = ANY($1)
		  AND (
		      (status = $2 AND status_observed_at BETWEEN $3 AND $5)
		      OR (status = $4 AND scheduled_start_time >= $5 AND scheduled_start_time <= $6
		          AND schedule_observed_at BETWEEN $7 AND $5)
		  )
		ORDER BY CASE WHEN status = $2 THEN status_observed_at ELSE schedule_observed_at END DESC
	