
		SELECT external_id, pub_date
		FROM major_events
		WHERE type = $1
		ORDER BY pub_date DESC NULLS LAST, updated_at DESC
		LIMIT $2
	