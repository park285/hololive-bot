-- 운영 roster 채널의 canonical LIVE session 중 head LIVE positive 시각(effective·seen)이
-- 이 statement의 DB 시각 기준 신선도 예산 밖이거나, 없거나(head 부재 포함), 미래인 영상만 고른다.
-- 호출자가 미리 잡은 시각을 쓰지 않으므로 그 뒤 커밋된 신선한 positive도 신선하게 본다.
-- UPCOMING·ENDED session은 대상이 아니며, 호출자가 LIMIT 초과를 입력 이상으로 거부한다.
WITH clock AS MATERIALIZED (
    SELECT statement_timestamp() AS as_of
)
SELECT s.video_id, s.channel_id
FROM youtube_live_sessions s
CROSS JOIN clock
LEFT JOIN youtube_live_reconciliation_heads h ON h.video_id = s.video_id
WHERE s.status = 'LIVE'
  AND s.channel_id = ANY($1::text[])
  AND NOT COALESCE(
      h.last_live_positive_at BETWEEN clock.as_of - $2::bigint * INTERVAL '1 millisecond' AND clock.as_of
      AND h.last_live_positive_seen_at BETWEEN clock.as_of - $2::bigint * INTERVAL '1 millisecond' AND clock.as_of,
      false
  )
ORDER BY s.video_id COLLATE "C"
LIMIT $3
