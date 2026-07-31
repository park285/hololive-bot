SELECT inbox.ordering_key
FROM bot_webhook_heads AS head
JOIN bot_webhook_inbox AS inbox ON inbox.message_id = head.message_id
WHERE inbox.status = 'processing'
  AND inbox.lease_until <= clock_timestamp()
ORDER BY inbox.ordering_key
LIMIT $1
