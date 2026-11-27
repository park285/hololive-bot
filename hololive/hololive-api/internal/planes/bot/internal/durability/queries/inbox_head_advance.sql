UPDATE bot_webhook_heads
SET message_id = ?3,
    updated_at = ?4
WHERE ordering_key = ?1
  AND message_id = ?2
