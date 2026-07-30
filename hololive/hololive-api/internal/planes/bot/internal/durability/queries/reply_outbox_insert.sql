INSERT INTO bot_reply_outbox (
    message_id,
    phase,
    ordinal,
    room_id,
    payload,
    payload_hash,
    client_request_id
)
VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7)
ON CONFLICT (message_id, phase, ordinal) DO NOTHING
