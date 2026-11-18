SELECT video_id,channel_id,session_status='UPCOMING' AS is_upcoming,statement_timestamp() AS as_of,
       CASE WHEN session_status='LIVE' THEN last_live_positive_at ELSE last_upcoming_positive_at END AS positive_at,
       CASE WHEN session_status='LIVE' THEN last_live_positive_seen_at ELSE last_upcoming_positive_seen_at END AS positive_seen_at,
       CASE WHEN session_status='UPCOMING' THEN availability_at END AS availability_at,
       CASE WHEN session_status='UPCOMING' THEN availability_seen_at END AS availability_seen_at,
       CASE WHEN session_status='UPCOMING' THEN unresolvable_since END AS unresolvable_since
FROM youtube_live_runtime_state
WHERE session_status IN ('LIVE','UPCOMING')
  AND (channel_id IN (SELECT unnest($1::text[]))) IS TRUE
  AND (session_status='LIVE' OR scheduled_start_time<statement_timestamp()
       OR (lifecycle_origin='legacy_unknown' AND scheduled_start_time IS NULL))
  AND (session_status='LIVE' OR reviewed_at IS NULL)
ORDER BY video_id COLLATE "C"
LIMIT $2
