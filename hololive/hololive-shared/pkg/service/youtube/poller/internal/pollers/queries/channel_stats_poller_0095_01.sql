
		INSERT INTO youtube_channel_stats_snapshots
			(channel_id, captured_at, subscriber_count, view_count, video_count, joined_date, description, country, handle)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)