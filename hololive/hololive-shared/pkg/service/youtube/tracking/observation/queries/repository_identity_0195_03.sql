
		ON CONFLICT (kind, canonical_content_id) DO UPDATE
		SET channel_id = EXCLUDED.channel_id,
		    actual_published_at = COALESCE(youtube_content_alarm_tracking.actual_published_at, EXCLUDED.actual_published_at),
		    detected_at = CASE
		        WHEN EXCLUDED.detected_at < youtube_content_alarm_tracking.detected_at THEN EXCLUDED.detected_at
		        ELSE youtube_content_alarm_tracking.detected_at
		    END,
		    alarm_sent_at = CASE
		        WHEN youtube_content_alarm_tracking.alarm_sent_at IS NULL THEN EXCLUDED.alarm_sent_at
		        WHEN EXCLUDED.alarm_sent_at IS NULL THEN youtube_content_alarm_tracking.alarm_sent_at
		        WHEN EXCLUDED.alarm_sent_at < youtube_content_alarm_tracking.alarm_sent_at THEN EXCLUDED.alarm_sent_at
		        ELSE youtube_content_alarm_tracking.alarm_sent_at
		    END,
		    alarm_latency_millis = 