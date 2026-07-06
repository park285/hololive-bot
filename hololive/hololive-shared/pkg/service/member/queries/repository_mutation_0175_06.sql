
		INSERT INTO members (
			slug, channel_id, english_name, japanese_name, korean_name,
			status, is_graduated, aliases, org, suborg, sync_source
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, NULL, $10)
	