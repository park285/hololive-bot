SELECT message_id
FROM bot_webhook_inbox
WHERE ordering_key = ?1
  AND message_id <> ?2
  AND status IN ('pending', 'processing', 'retry')
ORDER BY id
LIMIT 1
