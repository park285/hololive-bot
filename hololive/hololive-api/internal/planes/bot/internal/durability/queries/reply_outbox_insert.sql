INSERT INTO bot_reply_outbox (
    message_id,
    phase,
    ordinal,
    room_id,
    payload,
    payload_hash,
    client_request_id,
    available_at,
    created_at,
    updated_at
)
VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?8, ?8)
ON CONFLICT (message_id, phase, ordinal) DO NOTHING
