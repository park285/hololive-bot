FROM bot_webhook_heads AS head
JOIN bot_webhook_inbox AS due ON due.message_id = head.message_id
WHERE due.status IN ('pending', 'retry')
  AND due.available_at <= statement_timestamp()
