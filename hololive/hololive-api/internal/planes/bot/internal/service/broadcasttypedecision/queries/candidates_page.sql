SELECT s.video_id,
	s.channel_id,
	COALESCE(s.title, '') AS title,
	COALESCE(NULLIF(s.topic_id, ''), NULLIF(e.topic_id, ''), '') AS topic_id,
	COALESCE(NULLIF(s.thumbnail_url, ''), NULLIF(e.thumbnail_url, ''), '') AS thumbnail_url,
	s.history_sort_at
FROM youtube_live_sessions s
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
  AND s.history_sort_at >= $1::timestamptz
  AND (s.history_sort_at, s.video_id) < (COALESCE($2::timestamptz, 'infinity'::timestamptz), $3::text)
  AND (s.status_observed_at IS NULL OR s.status_observed_at <= $5::timestamptz)
  AND NOT EXISTS (
      SELECT 1
      FROM youtube_live_time_finalizations f
      WHERE f.video_id = s.video_id
        AND f.outcome = 'members_only'
  )
  AND NOT EXISTS (
      SELECT 1
      FROM youtube_video_availability a
      WHERE a.video_id = s.video_id
        AND a.availability = 'MEMBERS_ONLY'
  )
  AND NOT EXISTS (
      SELECT 1
      FROM youtube_broadcast_type_decisions d
      WHERE d.video_id = s.video_id
        AND d.title = COALESCE(s.title, '')
  )
ORDER BY s.history_sort_at DESC,
         s.video_id DESC
LIMIT $4
