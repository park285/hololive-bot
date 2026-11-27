INSERT INTO bot_webhook_inbox (message_id, room_id, ordering_key, payload, available_at, created_at, updated_at)
VALUES (?1, ?2, ?3, ?4, ?5, ?5, ?5)
ON CONFLICT (message_id) DO NOTHING
