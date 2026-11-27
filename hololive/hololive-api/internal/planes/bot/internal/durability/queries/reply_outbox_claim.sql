UPDATE bot_reply_outbox
SET status = 'submitting',
    claim_token = ?2,
    lease_until = ?3 + ?4,
    first_attempt_at = coalesce(first_attempt_at, ?3),
    attempts = attempts + 1,
    updated_at = ?3,
    row_version = row_version + 1
WHERE id = ?1
RETURNING id, message_id, phase, ordinal, room_id, payload, client_request_id, attempts
