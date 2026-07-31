WITH inserted AS (
    INSERT INTO bot_webhook_inbox (message_id, room_id, ordering_key, payload)
    VALUES ($1, $2, $3, $4::jsonb)
    ON CONFLICT (message_id) DO NOTHING
    RETURNING message_id, ordering_key
), installed_head AS (
    INSERT INTO bot_webhook_heads (ordering_key, message_id)
    SELECT ordering_key, message_id FROM inserted
    ON CONFLICT (ordering_key) DO NOTHING
)
SELECT EXISTS (SELECT 1 FROM inserted)
