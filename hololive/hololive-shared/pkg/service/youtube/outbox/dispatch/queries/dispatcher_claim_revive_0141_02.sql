
		UPDATE youtube_notification_delivery
		SET status = 'PENDING', attempt_count = 0, next_attempt_at = ?, locked_at = NULL, sent_at = NULL, error = ''
		WHERE 