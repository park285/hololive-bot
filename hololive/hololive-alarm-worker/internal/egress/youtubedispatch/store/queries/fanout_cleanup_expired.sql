-- 보존 기간과 lease 만료는 문장 첫머리에서 한 번 채취한 DB 시계(clock)로 판정한다.
-- $1 PENDING, $2 보존 기간 ms, $3 lease ms, $4 한도.
WITH clock AS MATERIALIZED (
    SELECT sampled.at - $2::bigint * INTERVAL '1 millisecond' AS created_cutoff,
           sampled.at - $3::bigint * INTERVAL '1 millisecond' AS lock_cutoff
    FROM (SELECT clock_timestamp() AS at) AS sampled
), candidates AS (
    SELECT outbox.id
    FROM youtube_notification_outbox AS outbox
    WHERE outbox.status = $1
      AND outbox.sent_at IS NULL
      AND outbox.created_at < (SELECT created_cutoff FROM clock)
      AND (outbox.locked_at IS NULL OR outbox.locked_at < (SELECT lock_cutoff FROM clock))
      AND NOT EXISTS (
          SELECT 1
          FROM youtube_notification_delivery AS delivery
          WHERE delivery.outbox_id = outbox.id
      )
    ORDER BY outbox.created_at, outbox.id
    LIMIT $4
    FOR UPDATE OF outbox SKIP LOCKED
)
DELETE FROM youtube_notification_outbox AS outbox
USING candidates
WHERE outbox.id = candidates.id
RETURNING outbox.id;
