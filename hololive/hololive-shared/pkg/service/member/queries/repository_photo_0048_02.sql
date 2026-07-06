
		SELECT photo
		FROM members
		WHERE channel_id = $1
		LIMIT 1
	