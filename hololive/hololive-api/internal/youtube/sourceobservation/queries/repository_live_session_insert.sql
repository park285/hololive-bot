-- 잠금 안에서 행이 없다고 읽은 영상만 만든다. 그 사이 다른 writer가 만든 행은 덮어쓰거나 병합하지 않고
-- 0행으로 끝나며, 호출자가 충돌로 보고한다.
INSERT INTO youtube_live_sessions (
    video_id, channel_id, status, title, topic_id, thumbnail_url,
    scheduled_start_time, started_at, ended_at, live_first_seen_at, last_seen_at,
    is_premiere, lifecycle_origin, status_observed_at, schedule_observed_at, title_observed_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
ON CONFLICT (video_id) DO NOTHING
