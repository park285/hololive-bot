SELECT room_id
FROM youtube_notification_delivery
WHERE outbox_id = $1
ORDER BY room_id
FOR UPDATE;
