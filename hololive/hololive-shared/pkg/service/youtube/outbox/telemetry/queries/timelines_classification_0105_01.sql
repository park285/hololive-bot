
		UPDATE youtube_content_alarm_tracking
		SET latency_classification_status = $1,
		    delay_source = $2,
		    internal_delay_cause = $3,
		    updated_at = $4
		WHERE kind = $5 AND content_id = $6
	