SELECT
    d.id AS delivery_id,
    o.kind,
    o.content_id,
    o.payload::text AS payload,
    d.room_id
FROM youtube_notification_delivery AS d
JOIN youtube_notification_outbox AS o ON o.id = d.outbox_id
WHERE 
