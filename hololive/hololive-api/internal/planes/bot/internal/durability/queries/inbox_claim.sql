UPDATE bot_webhook_inbox
SET status = 'processing',
    claim_token = ?2,
    lease_until = ?1 + ?3,
    attempts = attempts + 1,
    updated_at = ?1
WHERE id = (
    SELECT due.id
    {{inbox_ready_relation}}
    ORDER BY due.available_at ASC, due.id ASC
    LIMIT 1
)
RETURNING message_id, room_id, ordering_key, payload, attempts, lease_until
