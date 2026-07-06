
		DELETE FROM youtube_notification_delivery_telemetry
		WHERE logged_at IS NOT NULL AND event_at < $1
	