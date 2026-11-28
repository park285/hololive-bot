INSERT INTO bot_webhook_heads (ordering_key, message_id, updated_at)
VALUES (?1, ?2, ?3)
ON CONFLICT (ordering_key) DO NOTHING
