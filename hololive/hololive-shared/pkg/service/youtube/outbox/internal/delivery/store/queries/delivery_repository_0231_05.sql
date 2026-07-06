
		UPDATE youtube_notification_delivery
		SET attempt_count = attempt_count + 1,
		    error = $1,
		    status = CASE WHEN attempt_count + 1 >= $2 THEN $3 ELSE $4 END,
		    next_attempt_at = CASE WHEN attempt_count + 1 >= $5 THEN next_attempt_at ELSE $6 END,
		    locked_at = NULL
		WHERE id = $7
	