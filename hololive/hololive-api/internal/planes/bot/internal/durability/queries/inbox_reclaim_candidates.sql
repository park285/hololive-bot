SELECT inbox.id, inbox.attempts
FROM bot_webhook_heads AS head
JOIN bot_webhook_inbox AS inbox ON inbox.message_id = head.message_id
WHERE inbox.status = 'processing'
  AND inbox.lease_until <= clock_timestamp()
  AND inbox.ordering_key = ANY($2::text[])
ORDER BY inbox.lease_until ASC, inbox.id ASC
LIMIT $1
FOR UPDATE SKIP LOCKED
