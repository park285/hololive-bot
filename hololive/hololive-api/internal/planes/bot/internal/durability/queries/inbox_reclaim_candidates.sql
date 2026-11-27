SELECT inbox.id, inbox.attempts
FROM bot_webhook_heads AS head
JOIN bot_webhook_inbox AS inbox ON inbox.message_id = head.message_id
WHERE inbox.status = 'processing'
  AND inbox.lease_until <= ?1
ORDER BY inbox.lease_until ASC, inbox.id ASC
LIMIT ?2
