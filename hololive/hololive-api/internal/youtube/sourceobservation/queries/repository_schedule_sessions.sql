-- schedule reducer가 읽는 열과 함께 저장 열 전체를 잠근 채 읽는다. 저장은 이 행을 기준으로 조건부 갱신한다.
SELECT video_id, channel_id, status, COALESCE(title, '') AS title, topic_id, thumbnail_url,
       scheduled_start_time, started_at, ended_at, live_first_seen_at, last_seen_at,
       is_premiere, lifecycle_origin, status_observed_at, schedule_observed_at, title_observed_at
FROM youtube_live_sessions
WHERE video_id = ANY($1::text[])
ORDER BY video_id
FOR UPDATE
