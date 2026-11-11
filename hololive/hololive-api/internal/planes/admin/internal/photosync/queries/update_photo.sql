
		UPDATE members
		SET photo = $2, photo_updated_at = $3
		WHERE channel_id = $1
	