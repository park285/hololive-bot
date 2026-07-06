
		INSERT INTO youtube_channel_profiles
			(channel_id, avatar, banner, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (channel_id) DO UPDATE SET
			avatar = excluded.avatar,
			banner = excluded.banner,
			updated_at = excluded.updated_at