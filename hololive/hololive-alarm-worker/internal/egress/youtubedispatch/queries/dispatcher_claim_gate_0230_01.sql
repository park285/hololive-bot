
		UPDATE youtube_notification_delivery
		SET status = ?, sent_at = ?, locked_at = NULL, error = ''
		WHERE 