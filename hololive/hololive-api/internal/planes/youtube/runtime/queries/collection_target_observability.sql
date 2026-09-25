WITH mapping(kind, observation_kinds, rpc_calls) AS (
    VALUES
        ('youtubejs_channel_live', ARRAY['live_snapshot'], 1),
        ('community_collect', ARRAY['community_page'], 1),
        ('youtubejs_content', ARRAY['video_list', 'shorts_list'], 2),
        ('youtubejs_channel_metadata', ARRAY['channel_stats', 'channel_profile', 'channel_photo'], 1)
), current_projection AS (
    SELECT generation FROM youtube_collection_projection_generations
    WHERE status = 'CURRENT' AND valid_until > statement_timestamp()
), targets AS (
    SELECT m.kind, CASE WHEN m.kind = 'youtubejs_content' THEN COUNT(t.observation_kind) ELSE m.rpc_calls END AS rpc_calls, t.subject_key,
           MIN(t.poll_interval_ms) AS interval_ms, MIN(t.created_at) AS created_at
    FROM mapping m
    JOIN youtube_collection_targets t ON t.observation_kind = ANY(m.observation_kinds)
    JOIN current_projection g ON g.generation = t.projection_generation
    WHERE t.enabled AND t.valid_until > statement_timestamp()
    GROUP BY m.kind, m.rpc_calls, t.subject_key
), samples AS (
    SELECT t.kind, t.rpc_calls, t.subject_key, t.interval_ms, l.last_completed_at,
           CASE
               WHEN l.job_key IS NULL THEN t.created_at
               WHEN l.slot_state = 'IDLE' THEN l.next_due_at
               WHEN l.slot_state = 'DEFERRED' THEN l.retry_not_before
               WHEN l.slot_state = 'ACTIVE' THEN l.lease_expires_at
           END AS due_at
    FROM targets t
    LEFT JOIN youtube_collection_job_leases l
      ON l.job_key = 'collector:youtubejs:' || t.kind || ':' || t.subject_key
), active_live_videos AS (
    SELECT video_id FROM youtube_live_reconciliation_heads WHERE status IN ('LIVE', 'UPCOMING')
    UNION
    SELECT video_id FROM youtube_live_sessions WHERE status IN ('LIVE', 'UPCOMING')
), live_states AS (
    SELECT v.video_id, h.status AS state, product.status AS product_state, product.scheduled_start_time
    FROM active_live_videos v
    JOIN youtube_live_sessions product ON product.video_id = v.video_id
    JOIN targets t ON t.kind = 'youtubejs_channel_live' AND t.subject_key = product.channel_id
    LEFT JOIN youtube_live_reconciliation_heads h ON h.video_id = v.video_id
), live_summary AS (
    SELECT COUNT(video_id) FILTER (WHERE state = 'LIVE') AS live,
           COUNT(video_id) FILTER (WHERE state = 'UPCOMING') AS upcoming,
           COUNT(video_id) FILTER (WHERE state IS NULL OR state NOT IN ('LIVE', 'UPCOMING')) AS other,
           COUNT(video_id) FILTER (WHERE state IS DISTINCT FROM product_state) AS state_mismatch,
           COUNT(video_id) FILTER (WHERE state = 'UPCOMING' AND scheduled_start_time < statement_timestamp()) AS past_due,
           COUNT(video_id) FILTER (WHERE state = 'UPCOMING' AND scheduled_start_time < statement_timestamp() - INTERVAL '7 days') AS past_due_7d
    FROM live_states
), target_summary AS (
    SELECT m.kind, EXISTS(SELECT 1 FROM current_projection) AS projection_valid,
           COUNT(s.subject_key) AS targets,
           COUNT(s.subject_key) FILTER (WHERE s.last_completed_at IS NULL) AS never_completed,
           COUNT(s.subject_key) FILTER (WHERE s.last_completed_at < statement_timestamp() - s.interval_ms * INTERVAL '1 millisecond') AS stale,
           COALESCE(MAX(GREATEST(EXTRACT(EPOCH FROM statement_timestamp() - s.last_completed_at), 0)), 0)::double precision AS oldest_completion_age,
           COUNT(s.subject_key) FILTER (WHERE s.due_at <= statement_timestamp()) AS due,
           COALESCE(MAX(GREATEST(EXTRACT(EPOCH FROM statement_timestamp() - s.due_at), 0)), 0)::double precision AS oldest_due_age,
           COALESCE(SUM(s.rpc_calls * 1000.0 / s.interval_ms), 0)::double precision AS required_rpc_rate
    FROM mapping m LEFT JOIN samples s ON s.kind = m.kind
    GROUP BY m.kind
)
SELECT t.kind, t.projection_valid, t.targets, t.never_completed, t.stale,
       t.oldest_completion_age, t.due, t.oldest_due_age, t.required_rpc_rate,
       l.live, l.upcoming, l.other, l.state_mismatch, l.past_due, l.past_due_7d
FROM target_summary t CROSS JOIN live_summary l
ORDER BY t.kind;
