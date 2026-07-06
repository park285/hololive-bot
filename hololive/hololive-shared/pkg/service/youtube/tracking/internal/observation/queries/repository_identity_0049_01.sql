
		)
		SELECT DISTINCT t.kind, t.content_id, t.canonical_content_id, t.channel_id, t.actual_published_at, t.detected_at,
		       t.alarm_sent_at, t.alarm_latency_millis, t.alarm_latency_exceeded, t.delivery_status,
		       COALESCE(t.latency_classification_status, '') AS latency_classification_status,
		       COALESCE(t.delay_source, '') AS delay_source,
		       COALESCE(t.internal_delay_cause, '') AS internal_delay_cause,
		       t.created_at, t.updated_at
		FROM youtube_content_alarm_tracking t
		JOIN input i
		  ON t.kind = i.kind
		 AND (t.canonical_content_id = i.preferred_content_id OR t.content_id = i.candidate_content_id)