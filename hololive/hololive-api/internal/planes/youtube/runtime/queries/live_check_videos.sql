-- 구조 membership·검토 판정은 원본과 같은 트랜잭션에서 확정된 상태를 읽는다.
-- 미래 일정·신선도는 저장하지 않는다. 같은 statement의 DB 시각으로 대상과 사실 시각을 반환한다.
-- 구조 membership 필터 뒤 상한 판정용 $2행을 읽어 미래 일정 때문에 적격 영상이 잘리지 않게 한다.
SELECT video_id,channel_id,session_status='UPCOMING' AS is_upcoming,statement_timestamp() AS as_of,
       CASE WHEN session_status='LIVE' THEN last_live_positive_at ELSE last_upcoming_positive_at END AS positive_at,
       CASE WHEN session_status='LIVE' THEN last_live_positive_seen_at ELSE last_upcoming_positive_seen_at END AS positive_seen_at,
       CASE WHEN session_status='UPCOMING' THEN availability_at END AS availability_at,
       CASE WHEN session_status='UPCOMING' THEN availability_seen_at END AS availability_seen_at,
       -- LIVE는 재확인 주기를 늦추지 않는다. UPCOMING도 모든 positive보다 늦게 시작한 추적만 쓴다.
       CASE WHEN session_status='UPCOMING'
                 AND unresolvable_since>COALESCE(GREATEST(last_upcoming_positive_at,last_live_positive_at),'-infinity')
            THEN unresolvable_since END AS unresolvable_since
FROM youtube_live_runtime_state
WHERE session_status IN ('LIVE','UPCOMING')
  AND (channel_id IN (SELECT unnest($1::text[]))) IS TRUE
  AND (session_status='LIVE' OR scheduled_start_time<statement_timestamp()
       OR (lifecycle_origin='legacy_unknown' AND scheduled_start_time IS NULL))
  AND (session_status='LIVE' OR reviewed_at IS NULL)
ORDER BY video_id COLLATE "C"
LIMIT $2
