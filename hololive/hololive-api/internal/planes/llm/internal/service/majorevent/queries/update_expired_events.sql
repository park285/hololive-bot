
		UPDATE major_events
		SET status = $1,
			updated_at = NOW()
		WHERE status = $2
		  AND (
			event_end_date < CURRENT_DATE
			OR (event_end_date IS NULL AND event_start_date < CURRENT_DATE)
		  )
	