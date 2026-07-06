
		DELETE FROM youtube_notification_outbox
		WHERE status IN (?, ?) AND COALESCE(sent_at, created_at) < ?
	