
		UPDATE youtube_notification_outbox
		SET status = ?::text,
		    locked_at = NULL,
		    sent_at = CASE WHEN ?::text = ?::text THEN ? ELSE sent_at END,
		    error = CASE WHEN ?::text = ?::text THEN ? ELSE '' END
		WHERE 