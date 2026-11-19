WITH candidates AS MATERIALIZED (
    SELECT s.video_id, s.channel_id, s.status_observed_at, f.queue, f.next_attempt_at
    FROM youtube_live_sessions s
    LEFT JOIN youtube_live_reconciliation_heads h USING (video_id)
    LEFT JOIN youtube_live_time_finalizations f USING (video_id)
    WHERE s.status = 'ENDED' AND s.times_source = 'observed'
      AND COALESCE(h.end_reason, '') <> 'CANCELLED_BEFORE_LIVE'
      AND (s.started_at IS NOT NULL OR h.last_live_positive_at IS NOT NULL)
      AND ((f.video_id IS NULL AND s.status_observed_at >= statement_timestamp() - $4::interval)
           OR f.outcome = 'pending')
), new_videos AS (
    SELECT video_id, channel_id,
           COALESCE(next_attempt_at, status_observed_at + $5::interval) AS not_before
    FROM candidates
    WHERE queue IS NULL OR queue = 'new'
    ORDER BY status_observed_at DESC, video_id
    LIMIT $2
), ranked_backlog AS MATERIALIZED (
    SELECT f.video_id, f.outcome, f.next_attempt_at, f.last_attempt_at, f.enqueued_at, f.completed_at,
           row_number() OVER (ORDER BY f.observed_ended_at DESC NULLS LAST, f.video_id) - 1 AS position
    FROM youtube_live_time_finalizations f
    LEFT JOIN candidates c ON c.video_id = f.video_id
    WHERE f.queue = 'backlog' AND $1::int > 0 AND (f.outcome <> 'pending' OR c.video_id IS NOT NULL)
), backlog AS MATERIALIZED (
    SELECT video_id, outcome, next_attempt_at, last_attempt_at, enqueued_at, completed_at, position,
           position / NULLIF($1::int, 0) AS batch_no
    FROM ranked_backlog
), active AS (
    SELECT first_open.batch_no
    FROM (
        SELECT min(batch_no) FILTER (WHERE outcome = 'pending' AND next_attempt_at IS NULL) AS batch_no
        FROM backlog
    ) first_open
    WHERE first_open.batch_no IS NOT NULL
      AND (SELECT count(retrying.video_id) FROM backlog retrying
           WHERE retrying.outcome = 'pending' AND retrying.next_attempt_at IS NOT NULL
             AND retrying.batch_no <> first_open.batch_no) + $1::int <= $3::int
), turn AS (
    SELECT b.video_id,
           GREATEST((SELECT max(last_attempt_at) FROM backlog), b.enqueued_at) + interval '6 seconds' AS not_before
    FROM backlog b
    JOIN active a ON a.batch_no = b.batch_no
    WHERE b.outcome = 'pending' AND b.next_attempt_at IS NULL
    ORDER BY b.position
    LIMIT 1
), backlog_videos AS (
    SELECT b.video_id, s.channel_id,
           CASE WHEN b.outcome <> 'pending' THEN b.completed_at + interval '30 days'
                WHEN b.next_attempt_at IS NOT NULL THEN b.next_attempt_at
                WHEN t.video_id IS NOT NULL THEN t.not_before
                ELSE b.enqueued_at + interval '365 days'
           END AS not_before
    FROM backlog b
    JOIN youtube_live_sessions s ON s.video_id = b.video_id
    LEFT JOIN active a ON a.batch_no = b.batch_no
    LEFT JOIN turn t ON t.video_id = b.video_id
    WHERE a.batch_no IS NOT NULL OR (b.outcome = 'pending' AND b.next_attempt_at IS NOT NULL)
)
SELECT video_id, channel_id, not_before FROM new_videos
UNION ALL
SELECT video_id, channel_id, not_before FROM backlog_videos
