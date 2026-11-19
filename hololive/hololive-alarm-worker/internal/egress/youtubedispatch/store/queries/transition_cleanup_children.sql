WITH locked_rows AS MATERIALIZED (
    SELECT delivery.id,
           delivery.outbox_id,
           delivery.room_id,
           delivery.status,
           delivery.attempt_count,
           delivery.next_attempt_at,
           delivery.created_at,
           delivery.locked_at,
           delivery.sent_at,
           COALESCE(delivery.error, '') AS error,
           delivery.row_version,
           outbox.kind,
           outbox.channel_id,
           outbox.content_id,
           outbox.payload::text AS payload,
           outbox.created_at AS outbox_created_at,
           outbox.sent_at AS outbox_sent_at
    FROM youtube_notification_delivery AS delivery
    JOIN youtube_notification_outbox AS outbox ON outbox.id = delivery.outbox_id
    WHERE delivery.outbox_id = ANY($1::bigint[])
    FOR UPDATE OF delivery SKIP LOCKED
)
SELECT locked_rows.id,
       locked_rows.outbox_id,
       locked_rows.room_id,
       locked_rows.status,
       locked_rows.attempt_count,
       locked_rows.next_attempt_at,
       locked_rows.created_at,
       locked_rows.locked_at,
       locked_rows.sent_at,
       locked_rows.error,
       locked_rows.row_version,
       locked_rows.kind,
       locked_rows.channel_id,
       locked_rows.content_id,
       locked_rows.payload,
       locked_rows.outbox_created_at,
       locked_rows.outbox_sent_at,
       TRUE AS locked
FROM locked_rows
UNION ALL
SELECT delivery.id,
       delivery.outbox_id,
       delivery.room_id,
       delivery.status,
       delivery.attempt_count,
       delivery.next_attempt_at,
       delivery.created_at,
       delivery.locked_at,
       delivery.sent_at,
       COALESCE(delivery.error, '') AS error,
       delivery.row_version,
       outbox.kind,
       outbox.channel_id,
       outbox.content_id,
       outbox.payload::text AS payload,
       outbox.created_at AS outbox_created_at,
       outbox.sent_at AS outbox_sent_at,
       FALSE AS locked
FROM youtube_notification_delivery AS delivery
JOIN youtube_notification_outbox AS outbox ON outbox.id = delivery.outbox_id
WHERE delivery.outbox_id = ANY($1::bigint[])
  AND NOT EXISTS (SELECT 1 FROM locked_rows WHERE locked_rows.id = delivery.id)
ORDER BY outbox_id, id;
