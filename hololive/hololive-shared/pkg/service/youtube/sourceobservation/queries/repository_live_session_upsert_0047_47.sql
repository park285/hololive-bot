INSERT INTO youtube_live_sessions (
    video_id, channel_id, status, title, topic_id, thumbnail_url,
    scheduled_start_time, started_at, ended_at, live_first_seen_at, last_seen_at,
    is_premiere, lifecycle_origin, status_observed_at, schedule_observed_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $14, $15, $16)
ON CONFLICT (video_id) DO UPDATE SET
    status = CASE
        WHEN $13::boolean THEN youtube_live_sessions.status
        WHEN youtube_live_sessions.status = 'ENDED' THEN youtube_live_sessions.status
        WHEN youtube_live_sessions.status = 'LIVE' AND excluded.status = 'UPCOMING' THEN youtube_live_sessions.status
        ELSE excluded.status
    END,
    title = CASE
        WHEN $13::boolean OR excluded.title = '' THEN youtube_live_sessions.title
        ELSE excluded.title
    END,
    topic_id = CASE
        WHEN $13::boolean OR excluded.topic_id = '' THEN youtube_live_sessions.topic_id
        ELSE excluded.topic_id
    END,
    thumbnail_url = CASE
        WHEN $13::boolean OR excluded.thumbnail_url = '' THEN youtube_live_sessions.thumbnail_url
        ELSE excluded.thumbnail_url
    END,
    scheduled_start_time = CASE
        WHEN $13::boolean THEN youtube_live_sessions.scheduled_start_time
        ELSE COALESCE(excluded.scheduled_start_time, youtube_live_sessions.scheduled_start_time)
    END,
    started_at = CASE
        WHEN $13::boolean THEN youtube_live_sessions.started_at
        ELSE COALESCE(youtube_live_sessions.started_at, excluded.started_at)
    END,
    ended_at = CASE
        WHEN $13::boolean THEN youtube_live_sessions.ended_at
        ELSE COALESCE(youtube_live_sessions.ended_at, excluded.ended_at)
    END,
    live_first_seen_at = CASE
        WHEN $13::boolean THEN youtube_live_sessions.live_first_seen_at
        ELSE COALESCE(youtube_live_sessions.live_first_seen_at, excluded.live_first_seen_at)
    END,
    last_seen_at = CASE
        WHEN $13::boolean THEN youtube_live_sessions.last_seen_at
        ELSE GREATEST(youtube_live_sessions.last_seen_at, excluded.last_seen_at)
    END,
    is_premiere = COALESCE(youtube_live_sessions.is_premiere, excluded.is_premiere),
    lifecycle_origin = CASE
        WHEN $13::boolean OR excluded.lifecycle_origin <> 'observed' THEN youtube_live_sessions.lifecycle_origin
        ELSE excluded.lifecycle_origin
    END,
    status_observed_at = CASE
        WHEN $13::boolean OR youtube_live_sessions.status = 'ENDED'
            OR (youtube_live_sessions.status = 'LIVE' AND excluded.status = 'UPCOMING')
            THEN youtube_live_sessions.status_observed_at
        WHEN youtube_live_sessions.status = excluded.status
            THEN COALESCE(excluded.status_observed_at, youtube_live_sessions.status_observed_at)
        ELSE excluded.status_observed_at
    END,
    schedule_observed_at = CASE
        WHEN $13::boolean OR excluded.scheduled_start_time IS NULL THEN youtube_live_sessions.schedule_observed_at
        ELSE excluded.schedule_observed_at
    END
WHERE
    (
        $13::boolean
        AND youtube_live_sessions.is_premiere IS NULL
        AND excluded.is_premiere IS NOT NULL
    )
    OR (
        NOT $13::boolean
        AND (
            CASE
                WHEN youtube_live_sessions.status = 'ENDED' THEN youtube_live_sessions.status
                WHEN youtube_live_sessions.status = 'LIVE' AND excluded.status = 'UPCOMING' THEN youtube_live_sessions.status
                ELSE excluded.status
            END IS DISTINCT FROM youtube_live_sessions.status
            OR (
                excluded.title <> ''
                AND excluded.title IS DISTINCT FROM youtube_live_sessions.title
            )
            OR (
                excluded.topic_id <> ''
                AND excluded.topic_id IS DISTINCT FROM youtube_live_sessions.topic_id
            )
            OR (
                excluded.thumbnail_url <> ''
                AND excluded.thumbnail_url IS DISTINCT FROM youtube_live_sessions.thumbnail_url
            )
            OR COALESCE(excluded.scheduled_start_time, youtube_live_sessions.scheduled_start_time)
                IS DISTINCT FROM youtube_live_sessions.scheduled_start_time
            OR (youtube_live_sessions.started_at IS NULL AND excluded.started_at IS NOT NULL)
            OR (youtube_live_sessions.ended_at IS NULL AND excluded.ended_at IS NOT NULL)
            OR (youtube_live_sessions.live_first_seen_at IS NULL AND excluded.live_first_seen_at IS NOT NULL)
            OR GREATEST(youtube_live_sessions.last_seen_at, excluded.last_seen_at)
                IS DISTINCT FROM youtube_live_sessions.last_seen_at
            OR (youtube_live_sessions.is_premiere IS NULL AND excluded.is_premiere IS NOT NULL)
            OR (excluded.lifecycle_origin = 'observed' AND youtube_live_sessions.lifecycle_origin <> 'observed')
            OR CASE
                WHEN youtube_live_sessions.status = 'ENDED'
                    OR (youtube_live_sessions.status = 'LIVE' AND excluded.status = 'UPCOMING')
                    THEN youtube_live_sessions.status_observed_at
                WHEN youtube_live_sessions.status = excluded.status
                    THEN COALESCE(excluded.status_observed_at, youtube_live_sessions.status_observed_at)
                ELSE excluded.status_observed_at
            END IS DISTINCT FROM youtube_live_sessions.status_observed_at
            OR (excluded.scheduled_start_time IS NOT NULL
                AND excluded.schedule_observed_at IS DISTINCT FROM youtube_live_sessions.schedule_observed_at)
        )
    )
