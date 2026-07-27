
		SELECT channel_id, watermark_type, initialized, last_content_id, updated_at
		FROM youtube_content_watermarks
		WHERE channel_id = $1 AND watermark_type = $2