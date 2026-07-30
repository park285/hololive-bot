
		SELECT DISTINCT channel_id
		FROM youtube_live_sessions
		WHERE channel_id = ANY($1)
		  AND status = $2
		  AND last_seen_at >= $3
		ORDER BY channel_id
	