DELETE FROM bot_webhook_inbox
WHERE id IN (
    SELECT id
    FROM bot_webhook_inbox
    WHERE status IN ('dead', 'succeeded')
      AND updated_at < ?1
    ORDER BY updated_at ASC, id ASC
    LIMIT ?2
)
