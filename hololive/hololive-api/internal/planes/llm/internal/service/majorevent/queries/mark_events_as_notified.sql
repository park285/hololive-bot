
		UPDATE major_events
		SET notified_at = NOW(),
			notified_week = $1,
			updated_at = NOW()
		WHERE id = ANY($2)
	