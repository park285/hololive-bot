
	SELECT s.video_id,
		s.channel_id,
		COALESCE(NULLIF(m.member_name, ''), s.channel_id) AS member_name,
		COALESCE(s.title, '') AS title,
		COALESCE(NULLIF(s.topic_id, ''), NULLIF(e.topic_id, ''), '') AS topic_id,
		COALESCE(NULLIF(s.thumbnail_url, ''), NULLIF(e.thumbnail_url, ''), '') AS thumbnail_url,
		s.scheduled_start_time,
		s.started_at,
		s.ended_at,
		s.last_seen_at
	FROM youtube_live_sessions s
	LEFT JOIN LATERAL (
		SELECT string_agg(
			COALESCE(NULLIF(m.short_korean_name, ''), NULLIF(m.korean_name, ''), NULLIF(m.english_name, '')),
			' / ' ORDER BY m.english_name
		) AS member_name
		FROM members m
		WHERE m.channel_id = s.channel_id
	) m ON TRUE
	LEFT JOIN LATERAL (
		SELECT payload #>> '{notification,stream,topic_id}' AS topic_id,
		       payload #>> '{notification,stream,thumbnail}' AS thumbnail_url
		FROM alarm_dispatch_events e
		WHERE e.alarm_type = 'LIVE'
		  AND e.stream_id = s.video_id
		ORDER BY e.created_at DESC
		LIMIT 1
	) e ON TRUE
	WHERE s.status = 'ENDED'
	  AND ($1 = '' OR s.channel_id = $1)
	  AND ($2::timestamptz IS NULL OR COALESCE(s.ended_at, s.started_at, s.scheduled_start_time, s.last_seen_at) >= $2)
	  AND ($3::timestamptz IS NULL
	       OR COALESCE(s.ended_at, s.started_at, s.scheduled_start_time, s.last_seen_at) < $3
	       OR (
	           COALESCE(s.ended_at, s.started_at, s.scheduled_start_time, s.last_seen_at) = $3
	           AND s.video_id < $4
	       ))
	  AND ($5 = ''
	       OR octet_length(COALESCE(NULLIF(s.topic_id, ''), NULLIF(e.topic_id, ''), '')) <>
	          char_length(COALESCE(NULLIF(s.topic_id, ''), NULLIF(e.topic_id, ''), ''))
	       OR EXISTS (
	           SELECT 1
	           FROM regexp_split_to_table(
	               COALESCE(NULLIF(s.topic_id, ''), NULLIF(e.topic_id, ''), ''),
	               ','
	           ) AS topic_part(value)
	           WHERE lower(btrim(regexp_replace(topic_part.value, '[[:space:]]+', ' ', 'g'))) = $5
	       ))
	ORDER BY COALESCE(s.ended_at, s.started_at, s.scheduled_start_time, s.last_seen_at) DESC,
	         s.video_id DESC
	LIMIT $6
