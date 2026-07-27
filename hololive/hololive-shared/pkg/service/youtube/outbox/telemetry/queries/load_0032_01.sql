
		SELECT kind,
			content_id,
			COALESCE(canonical_content_id, '') AS canonical_content_id,
			channel_id,
			actual_published_at,
			detected_at,
			alarm_sent_at,
			alarm_latency_millis,
			alarm_latency_exceeded,
			delivery_status,
			COALESCE(latency_classification_status, '') AS latency_classification_status,
			COALESCE(delay_source, '') AS delay_source,
			COALESCE(internal_delay_cause, '') AS internal_delay_cause,
			created_at,
			updated_at
		FROM youtube_content_alarm_tracking
		WHERE 