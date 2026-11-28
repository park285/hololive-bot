DELETE FROM bot_webhook_heads
WHERE ordering_key = ?1
  AND message_id = ?2
