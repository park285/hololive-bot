
		UPDATE youtube_notification_outbox
		SET status = $1, locked_at = NULL, attempt_count = $2, error = $3
		WHERE id = $4 AND status = $5 AND locked_at = $6
	