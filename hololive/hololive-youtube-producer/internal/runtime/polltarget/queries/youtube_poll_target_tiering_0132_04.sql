
		SELECT channel_id,
		       MAX(GREATEST(COALESCE(published_at, '-infinity'::timestamptz), first_seen_at)) AS activity_at
		FROM youtube_community_posts
		WHERE channel_id = ANY($1)
		GROUP BY channel_id
	