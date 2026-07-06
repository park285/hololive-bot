
		SELECT id, slug, channel_id, english_name, japanese_name, korean_name, short_korean_name,
		       status, is_graduated, aliases, photo, org, suborg, sync_source, twitch_user_id
		FROM members
		ORDER BY english_name
	