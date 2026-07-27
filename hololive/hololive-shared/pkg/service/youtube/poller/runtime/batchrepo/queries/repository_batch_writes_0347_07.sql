
		INSERT INTO youtube_content_watermarks
			(channel_id, watermark_type, initialized, last_content_id, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (channel_id, watermark_type) DO UPDATE
		SET initialized = EXCLUDED.initialized,
		    last_content_id = EXCLUDED.last_content_id,
		    updated_at = EXCLUDED.updated_at
	