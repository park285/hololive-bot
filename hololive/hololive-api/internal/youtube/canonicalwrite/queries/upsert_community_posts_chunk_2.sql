
		ON CONFLICT (post_id) DO UPDATE
		SET last_seen_at = EXCLUDED.last_seen_at,
		    published_at = COALESCE(p.published_at, EXCLUDED.published_at),
		    like_count = EXCLUDED.like_count,
		    comment_count = EXCLUDED.comment_count
		WHERE (p.like_count, p.comment_count) IS DISTINCT FROM (EXCLUDED.like_count, EXCLUDED.comment_count)
		   OR (p.published_at IS NULL AND EXCLUDED.published_at IS NOT NULL)
	