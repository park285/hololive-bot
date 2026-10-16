SELECT id
FROM youtube_notification_outbox
WHERE id = ANY($1::bigint[])
ORDER BY id
FOR NO KEY UPDATE;
