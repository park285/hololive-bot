-- 신선도·lease 만료 판정과 되살린 next_attempt_at은 문장 첫머리에서 한 번 채취한 DB 시계(clock)로 정한다.
-- $1 FAILED, $2 신선도 ms, $3 lease ms, $4 한도, $5 PENDING.
WITH clock AS MATERIALIZED (
    SELECT sampled.at,
           sampled.at - $3::bigint * INTERVAL '1 millisecond' AS lock_cutoff,
           sampled.at - $2::bigint * INTERVAL '1 millisecond' AS fresh_cutoff
    FROM (SELECT clock_timestamp() AS at) AS sampled
), picked AS (
    SELECT outbox.id
    FROM youtube_notification_outbox AS outbox
    WHERE outbox.status = $1
      AND outbox.sent_at IS NULL
      AND outbox.created_at >= (SELECT fresh_cutoff FROM clock)
      AND (outbox.locked_at IS NULL OR outbox.locked_at < (SELECT lock_cutoff FROM clock))
      AND NOT EXISTS (
          SELECT 1
          FROM youtube_notification_delivery AS delivery
          WHERE delivery.outbox_id = outbox.id
      )
    ORDER BY outbox.created_at, outbox.id
    LIMIT $4
    FOR UPDATE OF outbox SKIP LOCKED
), revived AS (
    UPDATE youtube_notification_outbox AS outbox
    SET status = $5,
        attempt_count = 0,
        next_attempt_at = (SELECT at FROM clock),
        locked_at = NULL,
        sent_at = NULL,
        terminal_at = NULL,
        error = ''
    FROM picked
    WHERE outbox.id = picked.id
      AND outbox.status = $1
    RETURNING outbox.id
)
SELECT id FROM revived ORDER BY id;
