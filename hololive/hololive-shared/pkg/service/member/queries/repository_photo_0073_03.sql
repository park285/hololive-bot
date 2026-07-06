
		SELECT channel_id
		FROM members
		WHERE channel_id IS NOT NULL
		  AND (photo IS NULL OR photo_updated_at IS NULL OR photo_updated_at < $1)
	