
		INSERT INTO youtube_community_shorts_alarm_states
		    (kind, post_id, content_id, channel_id, actual_published_at, detected_at, authorized_at, alarm_sent_at, delivery_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (kind, post_id) DO UPDATE
		SET content_id = EXCLUDED.content_id,
		    channel_id = EXCLUDED.channel_id,
		    actual_published_at = COALESCE(youtube_community_shorts_alarm_states.actual_published_at, EXCLUDED.actual_published_at),
		    detected_at = CASE
		        WHEN EXCLUDED.detected_at < youtube_community_shorts_alarm_states.detected_at THEN EXCLUDED.detected_at
		        ELSE youtube_community_shorts_alarm_states.detected_at
		    END,
		    authorized_at = EXCLUDED.authorized_at,
		    delivery_status = EXCLUDED.delivery_status,
		    updated_at = EXCLUDED.updated_at
		WHERE youtube_community_shorts_alarm_states.authorized_at IS NULL
		  AND youtube_community_shorts_alarm_states.alarm_sent_at IS NULL
		RETURNING authorized_at, alarm_sent_at
	