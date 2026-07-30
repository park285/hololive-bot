INSERT INTO bot_webhook_inbox (message_id, room_id, ordering_key, payload)
VALUES ($1, $2, $3, $4::jsonb)
ON CONFLICT (message_id) DO NOTHING
