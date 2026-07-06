
		INSERT INTO youtube_live_viewer_samples
			(video_id, captured_at, channel_id, concurrent_viewers)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING