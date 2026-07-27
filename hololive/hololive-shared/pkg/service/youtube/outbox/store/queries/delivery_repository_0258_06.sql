
		UPDATE youtube_notification_delivery
		SET attempt_count = attempt_count + 1,
		    error = ?,
		    status = CASE WHEN attempt_count + 1 >= ? THEN ? ELSE ? END,
		    next_attempt_at = CASE WHEN attempt_count + 1 >= ? THEN next_attempt_at ELSE ? END,
		    locked_at = NULL
		WHERE 