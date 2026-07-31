WITH candidate AS MATERIALIZED (
    SELECT inbox.id, inbox.attempts
    FROM bot_webhook_heads AS head
    JOIN bot_webhook_inbox AS inbox ON inbox.message_id = head.message_id
    WHERE inbox.status = 'processing'
      AND inbox.lease_until <= clock_timestamp()
      AND inbox.ordering_key = ANY($3::text[])
    ORDER BY inbox.lease_until ASC, inbox.id ASC
    LIMIT $2
    FOR UPDATE SKIP LOCKED
), reclaimed AS (
    UPDATE bot_webhook_inbox AS inbox
    SET status = CASE WHEN candidate.attempts >= $1::int THEN 'dead' ELSE 'retry' END,
        payload = CASE WHEN candidate.attempts >= $1::int THEN '{}'::jsonb ELSE inbox.payload END,
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
    RETURNING inbox.message_id, inbox.ordering_key, inbox.status
), successor AS MATERIALIZED (
    SELECT reclaimed.message_id AS old_message_id, reclaimed.ordering_key, reclaimed.status, next_row.message_id
    FROM reclaimed LEFT JOIN LATERAL (
        SELECT message_id FROM bot_webhook_inbox
        WHERE ordering_key = reclaimed.ordering_key AND message_id <> reclaimed.message_id
          AND status IN ('pending', 'processing', 'retry')
        ORDER BY id LIMIT 1
    ) AS next_row ON reclaimed.status = 'dead'
), advanced AS (
    UPDATE bot_webhook_heads AS head SET message_id = successor.message_id, updated_at = clock_timestamp()
    FROM successor WHERE successor.status = 'dead' AND head.ordering_key = successor.ordering_key
      AND head.message_id = successor.old_message_id AND successor.message_id IS NOT NULL
), removed AS (
    DELETE FROM bot_webhook_heads AS head USING successor
    WHERE successor.status = 'dead' AND head.ordering_key = successor.ordering_key
      AND head.message_id = successor.old_message_id AND successor.message_id IS NULL
)
SELECT
    count(status) FILTER (WHERE status = 'retry')::bigint AS requeued,
    count(status) FILTER (WHERE status = 'dead')::bigint AS abandoned
FROM reclaimed
