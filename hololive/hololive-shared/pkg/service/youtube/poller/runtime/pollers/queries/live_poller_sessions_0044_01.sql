
			INSERT INTO youtube_live_sessions
				(video_id, channel_id, status, title, scheduled_start_time, started_at, ended_at, live_first_seen_at, topic_id, thumbnail_url, last_seen_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (video_id) DO UPDATE SET
				status = CASE
					WHEN youtube_live_sessions.status = 'ENDED' THEN youtube_live_sessions.status
					WHEN youtube_live_sessions.status = 'LIVE' AND excluded.status = 'UPCOMING' THEN youtube_live_sessions.status
					ELSE excluded.status
				END,
				title = excluded.title,
				scheduled_start_time = excluded.scheduled_start_time,
				started_at = COALESCE(youtube_live_sessions.started_at, excluded.started_at),
				ended_at = COALESCE(youtube_live_sessions.ended_at, excluded.ended_at),
				topic_id = COALESCE(NULLIF(excluded.topic_id, ''), youtube_live_sessions.topic_id),
				thumbnail_url = COALESCE(NULLIF(excluded.thumbnail_url, ''), youtube_live_sessions.thumbnail_url),
				live_first_seen_at = COALESCE(youtube_live_sessions.live_first_seen_at, excluded.live_first_seen_at),
				last_seen_at = GREATEST(youtube_live_sessions.last_seen_at, excluded.last_seen_at)
			WHERE
				CASE
					WHEN youtube_live_sessions.status = 'ENDED' THEN youtube_live_sessions.status
					WHEN youtube_live_sessions.status = 'LIVE' AND excluded.status = 'UPCOMING' THEN youtube_live_sessions.status
					ELSE excluded.status
				END IS DISTINCT FROM youtube_live_sessions.status
				OR excluded.title IS DISTINCT FROM youtube_live_sessions.title
				OR excluded.scheduled_start_time IS DISTINCT FROM youtube_live_sessions.scheduled_start_time
				OR (youtube_live_sessions.started_at IS NULL AND excluded.started_at IS NOT NULL)
				OR (youtube_live_sessions.ended_at IS NULL AND excluded.ended_at IS NOT NULL)
				OR (youtube_live_sessions.live_first_seen_at IS NULL AND excluded.live_first_seen_at IS NOT NULL)
				OR COALESCE(NULLIF(excluded.topic_id, ''), youtube_live_sessions.topic_id) IS DISTINCT FROM youtube_live_sessions.topic_id
				OR COALESCE(NULLIF(excluded.thumbnail_url, ''), youtube_live_sessions.thumbnail_url) IS DISTINCT FROM youtube_live_sessions.thumbnail_url
				OR excluded.last_seen_at >= youtube_live_sessions.last_seen_at + ($12::bigint * interval '1 microsecond')
