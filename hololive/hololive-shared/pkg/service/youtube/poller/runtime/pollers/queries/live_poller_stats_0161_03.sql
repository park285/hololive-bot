
		SELECT
			COALESCE(MAX(concurrent_viewers), 0)::int AS max_viewers,
			COALESCE(AVG(concurrent_viewers), 0)::int AS avg_viewers,
			COUNT(video_id)::int AS count
		FROM youtube_live_viewer_samples
		WHERE video_id = $1
