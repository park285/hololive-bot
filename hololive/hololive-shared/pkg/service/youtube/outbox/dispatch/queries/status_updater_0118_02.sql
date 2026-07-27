
		UPDATE youtube_notification_outbox
		SET status = $1, sent_at = $2, locked_at = NULL, error = ''
		WHERE id = $3 AND status = $4 AND locked_at = $5
	