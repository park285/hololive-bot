WITH target AS MATERIALIZED (
    SELECT id
    FROM youtube_notification_outbox
    WHERE id = $6
      AND status = $7
      AND attempt_count = $8
      AND locked_at = $9
    FOR UPDATE
), clock AS MATERIALIZED (
    SELECT clock_timestamp() AS at
    FROM (SELECT count(id) AS locked_count FROM target) AS acquired
)
UPDATE youtube_notification_outbox
SET status = $1,
    attempt_count = $2,
    next_attempt_at = (SELECT at FROM clock) + $3::bigint * INTERVAL '1 millisecond',
    locked_at = NULL,
    sent_at = NULL,
    terminal_at = CASE WHEN $4::boolean THEN (SELECT at FROM clock) END,
    error = $5
WHERE id IN (SELECT id FROM target)
RETURNING id;
