	SELECT s.video_id,
		s.channel_id,
		COALESCE(NULLIF(m.member_name, ''), s.channel_id) AS member_name,
		COALESCE(s.title, '') AS title,
		COALESCE(NULLIF(s.topic_id, ''), NULLIF(e.topic_id, ''), '') AS topic_id,
		COALESCE(NULLIF(s.thumbnail_url, ''), NULLIF(e.thumbnail_url, ''), '') AS thumbnail_url,
		s.scheduled_start_time,
		s.started_at,
		s.ended_at,
		s.last_seen_at,
		-- legacy_unknown 기록의 started_at은 실제 시작과 수 시간씩 어긋나 시작 시각 표시 여부를 이 값으로 판정한다.
		s.lifecycle_origin,
		-- 종료 시각이 관측 시각으로 채워졌을 때 실제 종료는 이 시각과 ended_at 사이다. 방송 길이 표시 판정에만 쓴다.
		h.last_live_positive_at,
		s.times_source,
		f.outcome,
		f.reason,
		d.broadcast_type,
		d.confidence,
		-- 종료 시각 확정이나 최신 시청 가능성 관측에서 YouTube player가 멤버 전용이라고 확인한 방송이다.
		(f.outcome IS NOT DISTINCT FROM 'members_only' OR a.availability IS NOT DISTINCT FROM 'MEMBERS_ONLY') AS player_members_only
	FROM youtube_live_sessions s
	LEFT JOIN youtube_live_reconciliation_heads h ON h.video_id = s.video_id
	LEFT JOIN youtube_live_time_finalizations f ON f.video_id = s.video_id
	LEFT JOIN youtube_video_availability a ON a.video_id = s.video_id
	-- 판정은 물을 때의 제목에 대한 답이라 제목이 바뀐 방송에는 쓰지 않는다.
	LEFT JOIN youtube_broadcast_type_decisions d ON d.video_id = s.video_id AND d.title = COALESCE(s.title, '')
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
	  AND s.video_id = $1
	LIMIT 1
