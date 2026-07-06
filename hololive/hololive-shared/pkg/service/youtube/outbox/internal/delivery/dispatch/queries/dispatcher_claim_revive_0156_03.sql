
		UPDATE youtube_notification_outbox
		SET status = 'PENDING', attempt_count = 0, next_attempt_at = ?, locked_at = NULL, error = ''
		WHERE 