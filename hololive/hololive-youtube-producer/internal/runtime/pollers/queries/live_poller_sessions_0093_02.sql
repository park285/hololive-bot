
	SELECT video_id,
		channel_id,
		status,
		title,
		scheduled_start_time,
		started_at,
		ended_at,
		live_first_seen_at,
		is_premiere,
		topic_id,
		thumbnail_url,
		last_seen_at
	FROM youtube_live_sessions