		UPDATE youtube_notification_delivery
		SET status = ?, locked_at = NULL, next_attempt_at = ?
		WHERE id = ? AND status = ?
