
		SELECT m.id, m.slug, m.channel_id, m.english_name, m.japanese_name, m.korean_name, m.short_korean_name,
		       m.status, m.is_graduated, m.aliases, m.org, m.suborg, m.sync_source, m.twitch_user_id, m.birthday, m.debut_date, m.official_link, m.units, m.chzzk_channel_id
		FROM members m
		WHERE m.aliases->'ko' ? $1
		   OR m.aliases->'ja' ? $1
		   OR lower(m.english_name) = lower($1)
		   OR lower(m.japanese_name) = lower($1)
		   OR lower(m.korean_name) = lower($1)
		ORDER BY m.id
		LIMIT 1
	