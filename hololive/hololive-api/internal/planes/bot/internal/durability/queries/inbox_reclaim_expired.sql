WITH candidate AS MATERIALIZED (
    SELECT id, attempts
    FROM bot_webhook_inbox
    WHERE status = 'processing'
      AND lease_until <= clock_timestamp()
    ORDER BY lease_until ASC, id ASC
    LIMIT $2
    FOR UPDATE SKIP LOCKED
), reclaimed AS (
    UPDATE bot_webhook_inbox AS inbox
    SET status = CASE WHEN candidate.attempts >= $1::int THEN 'dead' ELSE 'retry' END,
        claim_token = NULL,
        lease_until = NULL,
        available_at = clock_timestamp(),
        terminal_at = CASE
            WHEN candidate.attempts >= $1::int THEN clock_timestamp()
            ELSE inbox.terminal_at
        END,
        terminal_reason = CASE
            WHEN candidate.attempts >= $1::int THEN 'claim lease expired after max attempts'
            ELSE inbox.terminal_reason
        END,
        last_error = 'claim lease expired',
        updated_at = clock_timestamp()
    FROM candidate
    WHERE inbox.id = candidate.id
    RETURNING inbox.status
)
SELECT
    count(status) FILTER (WHERE status = 'retry')::bigint AS requeued,
    count(status) FILTER (WHERE status = 'dead')::bigint AS abandoned
FROM reclaimed
