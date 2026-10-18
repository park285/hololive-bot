WITH new_queue AS (
    SELECT count(s.video_id) AS value
    FROM youtube_live_sessions s
    LEFT JOIN youtube_live_reconciliation_heads h USING (video_id)
    LEFT JOIN youtube_live_time_finalizations f USING (video_id)
    WHERE s.status = 'ENDED' AND s.times_source = 'observed'
      AND COALESCE(h.end_reason, '') <> 'CANCELLED_BEFORE_LIVE'
      AND (s.started_at IS NOT NULL OR h.last_live_positive_at IS NOT NULL)
      AND ((f.video_id IS NULL AND s.status_observed_at >= statement_timestamp() - interval '48 hours')
           OR (f.queue = 'new' AND f.outcome = 'pending'))
), delays AS (
    SELECT f.queue, extract(epoch FROM f.completed_at -
           CASE WHEN f.queue = 'new' THEN s.status_observed_at ELSE f.retry_base_at END) AS seconds
    FROM youtube_live_time_finalizations f
    JOIN youtube_live_sessions s USING (video_id)
    WHERE f.outcome = 'finalized'
      AND f.completed_at >= statement_timestamp() - interval '24 hours'
), bounds(label, seconds) AS (
    VALUES ('180', 180), ('1800', 1800), ('3600', 3600), ('10800', 10800), ('86400', 86400), ('+Inf', NULL)
)
SELECT 'outcomes' AS metric, outcome AS label, COALESCE(reason, '') AS detail, count(video_id) AS value
FROM youtube_live_time_finalizations
GROUP BY outcome, COALESCE(reason, '')
UNION ALL
SELECT 'queue', 'new', '', value FROM new_queue
UNION ALL
SELECT 'queue', 'backlog', '', count(video_id) FROM youtube_live_time_finalizations
WHERE queue = 'backlog' AND outcome = 'pending'
UNION ALL
SELECT 'delay', q.queue, b.label, count(d.seconds) FILTER (WHERE b.seconds IS NULL OR d.seconds <= b.seconds)
FROM (VALUES ('new'), ('backlog')) q(queue)
CROSS JOIN bounds b
LEFT JOIN delays d ON d.queue = q.queue
GROUP BY q.queue, b.label, b.seconds
