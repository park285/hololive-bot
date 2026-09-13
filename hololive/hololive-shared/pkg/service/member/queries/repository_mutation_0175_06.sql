
		INSERT INTO members (
			slug, channel_id, english_name, japanese_name, korean_name,
			status, is_graduated, aliases, org, suborg, sync_source, units, official_link, birthday, debut_date, short_korean_name, chzzk_channel_id, twitch_user_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, NULL, $10, COALESCE($11::text[], '{}'::text[]), NULLIF($12, ''), $13, $14, NULLIF($15, ''), NULLIF($16, ''), NULLIF($17, ''))
	