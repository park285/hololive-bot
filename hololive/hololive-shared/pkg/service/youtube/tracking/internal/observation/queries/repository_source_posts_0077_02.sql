
		ON CONFLICT (kind, post_id) DO UPDATE
		SET channel_id = EXCLUDED.channel_id,
		    actual_published_at = COALESCE(youtube_community_shorts_source_posts.actual_published_at, EXCLUDED.actual_published_at),
		    detected_at = CASE
		        WHEN EXCLUDED.detected_at < youtube_community_shorts_source_posts.detected_at THEN EXCLUDED.detected_at
		        ELSE youtube_community_shorts_source_posts.detected_at
		    END,
		    updated_at = EXCLUDED.updated_at
	