WITH candidate AS MATERIALIZED (
    SELECT id
    FROM bot_webhook_inbox
    WHERE status IN ('pending', 'retry')
      AND available_at <= clock_timestamp()
    ORDER BY available_at ASC, id ASC
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
RETURNING inbox.message_id, inbox.room_id, inbox.ordering_key, inbox.payload, inbox.attempts
