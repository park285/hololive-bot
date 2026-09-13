
		SELECT id, channel_id, english_name, japanese_name, korean_name, short_korean_name,
		       is_graduated, aliases, photo, org, suborg, sync_source, twitch_user_id, birthday, debut_date, official_link, units, chzzk_channel_id
		FROM members
		WHERE channel_id = ANY($1::text[])
	