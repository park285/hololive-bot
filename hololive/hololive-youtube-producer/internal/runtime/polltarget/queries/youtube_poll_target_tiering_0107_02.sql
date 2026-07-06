
		SELECT channel_id,
		       MAX(GREATEST(
		           last_seen_at,
		           COALESCE(scheduled_start_time, '-infinity'::timestamptz),
		           COALESCE(started_at, '-infinity'::timestamptz),
		           COALESCE(ended_at, '-infinity'::timestamptz)
		       )) AS activity_at
		FROM youtube_live_sessions
		WHERE channel_id = ANY($1)
		GROUP BY channel_id
	