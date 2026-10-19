-- lease 만료·신선도는 문장 첫머리에서 한 번 채취한 DB 시계(clock)로 판정하고, 그 시계를 checked_at으로 돌려
-- 호출자가 그룹 재확인과 revive 신선도 검사에 같은 기준을 쓰게 한다. clock은 행을 잠그기 전에 채취한다.
-- $1 상태, $2 lease ms, $3 신선도 ms, $4 한도.
WITH clock AS MATERIALIZED (
    SELECT sampled.at,
           sampled.at - $2::bigint * INTERVAL '1 millisecond' AS lock_cutoff,
           sampled.at - $3::bigint * INTERVAL '1 millisecond' AS fresh_cutoff
    FROM (SELECT clock_timestamp() AS at) AS sampled
)
SELECT delivery.id,
       delivery.outbox_id,
       delivery.room_id,
       delivery.status,
       delivery.attempt_count,
       delivery.next_attempt_at,
       delivery.created_at,
       delivery.locked_at,
       delivery.locked_by,
       delivery.sent_at,
       COALESCE(delivery.error, '') AS error,
       delivery.row_version,
       outbox.kind,
       outbox.channel_id,
       outbox.content_id,
       outbox.payload::text AS payload,
       outbox.created_at AS outbox_created_at,
       outbox.sent_at AS outbox_sent_at,
       (SELECT at FROM clock) AS checked_at
FROM youtube_notification_delivery AS delivery
JOIN youtube_notification_outbox AS outbox ON outbox.id = delivery.outbox_id
WHERE delivery.status = $1
  AND (delivery.locked_at IS NULL OR delivery.locked_at < (SELECT lock_cutoff FROM clock))
  AND outbox.created_at >= (SELECT fresh_cutoff FROM clock)
  AND outbox.sent_at IS NULL
  AND COALESCE(delivery.error, '') NOT IN ('delivery freshness expired', 'client request id generations exhausted')
ORDER BY delivery.created_at, delivery.id
LIMIT $4
FOR UPDATE OF delivery SKIP LOCKED;
