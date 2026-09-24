WITH mapping(kind, observation_kinds, rpc_calls) AS (
    VALUES
        ('youtubejs_channel_live', ARRAY['live_snapshot'], 1),
        ('youtubejs_viewer', ARRAY['viewer_sample'], 1),
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
    SELECT t.kind, t.rpc_calls, t.subject_key, t.interval_ms, t.created_at, l.last_completed_at,
           CASE
               WHEN l.job_key IS NULL THEN t.created_at
               WHEN l.slot_state = 'IDLE' THEN l.next_due_at
               WHEN l.slot_state = 'DEFERRED' THEN l.retry_not_before
               WHEN l.slot_state = 'ACTIVE' THEN l.lease_expires_at
           END AS due_at,
           h.status AS viewer_state, product.status AS product_state,
           product.scheduled_start_time
    FROM targets t
    LEFT JOIN youtube_collection_job_leases l
      ON l.job_key = 'collector:youtubejs:' || t.kind || ':' || t.subject_key
    LEFT JOIN youtube_live_reconciliation_heads h
      ON t.kind = 'youtubejs_viewer' AND h.video_id = t.subject_key
    LEFT JOIN youtube_live_sessions product
      ON t.kind = 'youtubejs_viewer' AND product.video_id = t.subject_key
)
SELECT m.kind, EXISTS(SELECT 1 FROM current_projection) AS projection_valid,
       COUNT(s.subject_key) AS targets,
       COUNT(s.subject_key) FILTER (WHERE s.last_completed_at IS NULL) AS never_completed,
       COUNT(s.subject_key) FILTER (WHERE s.last_completed_at < statement_timestamp() - s.interval_ms * INTERVAL '1 millisecond') AS stale,
       COALESCE(MAX(GREATEST(EXTRACT(EPOCH FROM statement_timestamp() - s.last_completed_at), 0)), 0)::double precision AS oldest_completion_age,
       COUNT(s.subject_key) FILTER (WHERE s.due_at <= statement_timestamp()) AS due,
       COALESCE(MAX(GREATEST(EXTRACT(EPOCH FROM statement_timestamp() - s.due_at), 0)), 0)::double precision AS oldest_due_age,
       COALESCE(SUM(s.rpc_calls * 1000.0 / s.interval_ms), 0)::double precision AS required_rpc_rate,
       COUNT(s.subject_key) FILTER (WHERE s.viewer_state = 'LIVE') AS viewer_live,
       COUNT(s.subject_key) FILTER (WHERE s.viewer_state = 'UPCOMING') AS viewer_upcoming,
       COUNT(s.subject_key) FILTER (WHERE s.viewer_state IS NULL OR s.viewer_state NOT IN ('LIVE', 'UPCOMING')) AS viewer_other,
       COUNT(s.subject_key) FILTER (WHERE s.product_state IS NOT NULL AND s.viewer_state IS DISTINCT FROM s.product_state) AS viewer_state_mismatch,
       COUNT(s.subject_key) FILTER (WHERE s.viewer_state = 'UPCOMING' AND s.scheduled_start_time < statement_timestamp()) AS viewer_past_due,
       COUNT(s.subject_key) FILTER (WHERE s.viewer_state = 'UPCOMING' AND s.scheduled_start_time < statement_timestamp() - INTERVAL '7 days') AS viewer_past_due_7d
FROM mapping m LEFT JOIN samples s ON s.kind = m.kind
GROUP BY m.kind ORDER BY m.kind;
