SELECT s.video_id,
	s.channel_id,
	COALESCE(s.title, '') AS title,
	-- 방송 이력 조회(broadcast_history_list_page_template.sql)와 같은 주제·썸네일 보완이어야 미분류 판단이 같다.
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
  -- 멤버 한정은 형식보다 앞서는 접근 유형인데 Decisions 입력(제목·주제·썸네일)으로는 알 수 없다.
  -- player가 멤버 전용이라고 확인한 방송은 묻지 않아 잡담·게임 같은 형식으로 잘못 저장하지 않는다.
  -- 그 확인은 종료 뒤에 기록되므로 종료 관측이 $5보다 이른 방송만 묻는다.
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
