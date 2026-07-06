
		SELECT channel_id,
		       MAX(GREATEST(COALESCE(actual_published_at, '-infinity'::timestamptz), detected_at, created_at)) AS activity_at
		FROM youtube_content_alarm_tracking
		WHERE channel_id = ANY($1)
		GROUP BY channel_id
	