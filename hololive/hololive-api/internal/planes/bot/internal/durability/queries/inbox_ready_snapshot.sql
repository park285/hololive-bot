WITH ready AS (
    SELECT due.id, due.created_at
    FROM bot_webhook_heads AS head
    JOIN bot_webhook_inbox AS due ON due.message_id = head.message_id
    WHERE due.status IN ('pending', 'retry')
      AND due.available_at <= clock_timestamp()
)
SELECT COUNT(id),
       COALESCE(GREATEST(EXTRACT(EPOCH FROM (clock_timestamp() - MIN(created_at))), 0), 0)
FROM ready
