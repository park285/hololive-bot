SELECT
    outbox.id,
    outbox.status,
    outbox.sent_at,
    delivery.latest_sent_at
FROM youtube_notification_outbox AS outbox
LEFT JOIN LATERAL (
    SELECT MAX(sent_at) AS latest_sent_at
    FROM youtube_notification_delivery
    WHERE outbox_id = outbox.id
) AS delivery ON true
WHERE outbox.id > $1
  AND outbox.id <= $2
ORDER BY outbox.id
LIMIT $3
FOR UPDATE OF outbox;
