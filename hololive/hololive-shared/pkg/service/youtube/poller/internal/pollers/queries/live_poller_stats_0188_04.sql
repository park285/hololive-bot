
		INSERT INTO youtube_stream_stats
			(video_id, channel_id, started_at, ended_at, max_concurrent_viewers, avg_concurrent_viewers, sample_count, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (video_id) DO UPDATE SET
			ended_at = excluded.ended_at,
			max_concurrent_viewers = excluded.max_concurrent_viewers,
			avg_concurrent_viewers = excluded.avg_concurrent_viewers,
			sample_count = excluded.sample_count,
			updated_at = excluded.updated_at