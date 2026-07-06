
		UPDATE youtube_notification_outbox
		SET locked_at = NULL, attempt_count = $1, next_attempt_at = $2, error = $3
		WHERE id = $4
	