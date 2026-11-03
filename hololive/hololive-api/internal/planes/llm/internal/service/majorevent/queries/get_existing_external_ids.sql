
		SELECT external_id
		FROM major_events
		WHERE type = $1 AND external_id = ANY($2::text[])
	