INSERT INTO bot_webhook_inbox (
    id, message_id, room_id, ordering_key, payload, status, attempts, claim_token, lease_until,
    available_at, terminal_reason, terminal_at, last_error, created_at, updated_at
) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?15)
