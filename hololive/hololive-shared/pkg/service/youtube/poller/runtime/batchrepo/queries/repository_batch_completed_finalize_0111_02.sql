
		)
		UPDATE youtube_notification_delivery d
		SET status = ?,
		    locked_at = NULL,
		    sent_at = CASE WHEN d.sent_at IS NULL OR d.sent_at > i.sent_at THEN i.sent_at ELSE d.sent_at END,
		    error = ''
		FROM input i
		WHERE d.outbox_id = i.id AND d.status = ?