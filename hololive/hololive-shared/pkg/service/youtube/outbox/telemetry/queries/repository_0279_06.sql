
		UPDATE youtube_notification_delivery_telemetry
		SET logged_at = ?, locked_at = NULL, error = ''
		WHERE 