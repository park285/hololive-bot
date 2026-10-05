INSERT INTO youtube_live_sessions (
    video_id, channel_id, status, title, topic_id, thumbnail_url,
    scheduled_start_time, started_at, ended_at, live_first_seen_at, last_seen_at,
    is_premiere, lifecycle_origin, status_observed_at, schedule_observed_at, title_observed_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
-- 확인된 Premiere 분류만 보완한다. 기존 수명·메타데이터·관측 시각은 갱신하지 않는다.
ON CONFLICT (video_id) DO UPDATE
SET is_premiere = excluded.is_premiere
WHERE youtube_live_sessions.is_premiere IS NULL AND excluded.is_premiere IS NOT NULL
