		SELECT COALESCE(NULLIF(short_korean_name, ''), NULLIF(korean_name, ''), '') AS member_name
		FROM members
		WHERE channel_id = $1
		ORDER BY id ASC
		LIMIT 1
