
			UPDATE youtube_notification_outbox
			SET status = ?, sent_at = ?, locked_at = NULL, error = ''
			WHERE 