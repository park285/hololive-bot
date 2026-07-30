
		UPDATE youtube_notification_delivery
		SET attempt_count = CASE WHEN attempt_count >= ? THEN attempt_count ELSE ? END,
		    error = ?,
		    status = ?,
		    locked_at = NULL
		WHERE 