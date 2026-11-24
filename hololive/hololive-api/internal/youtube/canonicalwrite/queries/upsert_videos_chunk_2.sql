
		ON CONFLICT (video_id) DO UPDATE
		SET last_seen_at = EXCLUDED.last_seen_at,
		    published_at = COALESCE(youtube_videos.published_at, EXCLUDED.published_at),
		    view_count = EXCLUDED.view_count
		WHERE youtube_videos.view_count IS DISTINCT FROM EXCLUDED.view_count
		   OR (youtube_videos.published_at IS NULL AND EXCLUDED.published_at IS NOT NULL)
	