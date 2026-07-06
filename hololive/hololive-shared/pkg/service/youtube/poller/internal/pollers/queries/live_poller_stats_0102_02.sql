
		UPDATE youtube_live_sessions
		SET status = $1, ended_at = $2, last_seen_at = $2
		WHERE video_id = $3