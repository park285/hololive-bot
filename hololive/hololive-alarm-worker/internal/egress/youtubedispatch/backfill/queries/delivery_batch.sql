SELECT
    delivery.id,
    outbox.kind,
    outbox.content_id,
    outbox.payload::text,
    delivery.room_id,
    delivery.status,
    delivery.sent_at
FROM youtube_notification_delivery AS delivery
JOIN youtube_notification_outbox AS outbox ON outbox.id = delivery.outbox_id
WHERE delivery.id > $1
  AND delivery.id <= $2
ORDER BY delivery.id
LIMIT $3
FOR UPDATE OF delivery;
