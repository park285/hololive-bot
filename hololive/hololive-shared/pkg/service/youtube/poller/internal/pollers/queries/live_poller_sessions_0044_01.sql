
			INSERT INTO youtube_live_sessions
				(video_id, channel_id, status, title, scheduled_start_time, started_at, ended_at, live_first_seen_at, topic_id, thumbnail_url, last_seen_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (video_id) DO UPDATE SET
				status = excluded.status,
				title = excluded.title,
				scheduled_start_time = excluded.scheduled_start_time,
				started_at = excluded.started_at,
				ended_at = excluded.ended_at,
				topic_id = COALESCE(NULLIF(excluded.topic_id, ''), youtube_live_sessions.topic_id),
				thumbnail_url = COALESCE(NULLIF(excluded.thumbnail_url, ''), youtube_live_sessions.thumbnail_url),
				live_first_seen_at = COALESCE(youtube_live_sessions.live_first_seen_at, excluded.live_first_seen_at),
				last_seen_at = excluded.last_seen_at