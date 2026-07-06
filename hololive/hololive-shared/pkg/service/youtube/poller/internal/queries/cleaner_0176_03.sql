
		WITH picked AS (
			SELECT s.video_id, s.captured_at
			FROM youtube_live_viewer_samples s
			JOIN youtube_live_sessions l ON l.video_id = s.video_id
			WHERE l.status = $1 AND l.ended_at < $2
			ORDER BY s.video_id ASC, s.captured_at ASC
			LIMIT $3
		)
		DELETE FROM youtube_live_viewer_samples
		USING picked
		WHERE youtube_live_viewer_samples.video_id = picked.video_id
			AND youtube_live_viewer_samples.captured_at = picked.captured_at