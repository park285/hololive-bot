WITH candidate AS MATERIALIZED (
    SELECT due.id
    {{inbox_ready_relation}}
    ORDER BY due.available_at ASC, due.id ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE bot_webhook_inbox AS inbox
SET status = 'processing',
    claim_token = $1,
    lease_until = clock_timestamp() + ($2::bigint * INTERVAL '1 millisecond'),
    attempts = inbox.attempts + 1,
    updated_at = clock_timestamp()
FROM candidate
WHERE inbox.id = candidate.id
RETURNING inbox.message_id, inbox.room_id, inbox.ordering_key, inbox.payload, inbox.attempts, inbox.lease_until
