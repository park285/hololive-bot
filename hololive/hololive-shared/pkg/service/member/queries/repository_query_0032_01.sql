
		SELECT id, slug, channel_id, english_name, japanese_name, korean_name, short_korean_name,
		       status, is_graduated, aliases, org, suborg, sync_source, twitch_user_id
		FROM members
		WHERE channel_id = $1
		LIMIT 1
	