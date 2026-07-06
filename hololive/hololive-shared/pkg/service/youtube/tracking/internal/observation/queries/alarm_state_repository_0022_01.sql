
		SELECT kind, post_id, content_id, channel_id, actual_published_at, detected_at,
		       authorized_at, alarm_sent_at, delivery_status, created_at, updated_at
		FROM youtube_community_shorts_alarm_states
		WHERE kind = ? AND post_id = ?
		LIMIT 1
	