
		)
		UPDATE youtube_notification_outbox o
		SET status = ?,
		    locked_at = NULL,
		    sent_at = CASE WHEN o.sent_at IS NULL OR o.sent_at > i.sent_at THEN i.sent_at ELSE o.sent_at END,
		    error = ''
		FROM input i
		WHERE o.id = i.id AND o.status = ?