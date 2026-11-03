		SELECT DISTINCT ON (video_id) video_id, collabo_talent_names
		FROM youtube_schedule_items
		WHERE provider = 'hololive_official'
		  AND video_id = ANY($1)
		ORDER BY video_id, updated_at DESC
