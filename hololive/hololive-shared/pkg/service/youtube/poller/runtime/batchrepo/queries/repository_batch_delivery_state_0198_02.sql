
		UPDATE youtube_notification_delivery
		SET status = ?,
		    attempt_count = 0,
		    next_attempt_at = ?,
		    locked_at = NULL,
		    sent_at = NULL,
		    error = ''
		WHERE outbox_id IN (