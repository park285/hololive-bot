
		SELECT id, channel_id, english_name, japanese_name, korean_name, short_korean_name,
		       is_graduated, aliases, photo, org, suborg, sync_source, twitch_user_id
		FROM members
		WHERE channel_id = $1
		LIMIT 1
	