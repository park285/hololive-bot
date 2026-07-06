
		SELECT id, slug, channel_id, english_name, japanese_name, korean_name, short_korean_name,
		       status, is_graduated, aliases, org, suborg, sync_source, twitch_user_id
		FROM members
		WHERE (LOWER(english_name) = LOWER($1)
		   OR LOWER(korean_name) = LOWER($1)
		   OR aliases->'ko' @> to_jsonb($1::text)
		   OR aliases->'ja' @> to_jsonb($1::text))
		  AND LOWER(org) = LOWER($2)
		LIMIT 1
	