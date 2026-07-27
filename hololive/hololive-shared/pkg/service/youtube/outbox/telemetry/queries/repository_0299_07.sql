
		UPDATE youtube_notification_delivery_telemetry
		SET locked_at = NULL, next_attempt_at = ?, error = ?
		WHERE 