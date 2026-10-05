
		UPDATE youtube_community_shorts_alarm_states
		SET authorized_at = NULL,
		    delivery_status = $1,
		    updated_at = $2
		WHERE kind = $3 AND post_id = $4
		  AND alarm_sent_at IS NULL
		  AND authorized_at = $5
	