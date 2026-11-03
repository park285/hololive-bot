
		SELECT video_id
		FROM youtube_live_sessions
		WHERE video_id = ANY($1)
		  AND is_premiere IS TRUE
