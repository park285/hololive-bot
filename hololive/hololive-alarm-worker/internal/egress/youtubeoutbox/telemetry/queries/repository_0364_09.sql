
		WITH picked AS (
			SELECT id FROM youtube_notification_delivery_telemetry
			WHERE logged_at IS NOT NULL AND event_at < $1
			ORDER BY event_at ASC, id ASC
			LIMIT $2
		)
		DELETE FROM youtube_notification_delivery_telemetry t
		USING picked
		WHERE t.id = picked.id

