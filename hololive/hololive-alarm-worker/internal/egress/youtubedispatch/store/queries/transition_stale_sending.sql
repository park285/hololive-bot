WITH clock AS MATERIALIZED (
    SELECT sampled.at, sampled.at - $2::bigint * INTERVAL '1 millisecond' AS lock_cutoff
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
  AND delivery.status = 'SENDING'
  AND delivery.locked_at < (SELECT lock_cutoff FROM clock)
ORDER BY delivery.locked_at, delivery.created_at, delivery.id
LIMIT $3
FOR UPDATE OF delivery SKIP LOCKED;
