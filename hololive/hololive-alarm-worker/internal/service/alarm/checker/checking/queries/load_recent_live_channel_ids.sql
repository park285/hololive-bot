
		SELECT DISTINCT channel_id
		FROM youtube_live_sessions
		WHERE channel_id = ANY($1)
		  AND status = $2
		  AND status_observed_at BETWEEN $3 AND $4
		ORDER BY channel_id
	