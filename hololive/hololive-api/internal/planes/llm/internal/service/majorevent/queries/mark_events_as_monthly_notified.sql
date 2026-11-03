
		UPDATE major_events
		SET notified_month = $1,
			updated_at = NOW()
		WHERE id = ANY($2)
	