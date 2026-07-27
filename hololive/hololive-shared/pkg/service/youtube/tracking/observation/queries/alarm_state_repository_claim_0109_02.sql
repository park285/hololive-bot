
		UPDATE youtube_community_shorts_alarm_states
		SET authorized_at = NULL,
		    delivery_status = ?,
		    updated_at = ?
		WHERE kind = ? AND post_id = ?
		  AND alarm_sent_at IS NULL
		  AND authorized_at = ?
	