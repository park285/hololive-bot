
		UPDATE youtube_notification_outbox
		SET locked_at = NULL
		WHERE id = ? AND status = ?
	