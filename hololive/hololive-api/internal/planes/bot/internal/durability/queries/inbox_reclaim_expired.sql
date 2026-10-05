WITH candidate AS (
    SELECT id, attempts, target_status, terminal
    FROM unnest($1::bigint[], $2::int[], $3::text[], $4::boolean[])
        AS input(id, attempts, target_status, terminal)
), reclaimed AS (
    UPDATE bot_webhook_inbox AS inbox
    SET status = candidate.target_status,
        payload = CASE WHEN candidate.terminal THEN '{}'::jsonb ELSE inbox.payload END,
        claim_token = NULL,
        lease_until = NULL,
        available_at = clock_timestamp(),
        terminal_at = CASE
            WHEN candidate.terminal THEN clock_timestamp()
            ELSE inbox.terminal_at
        END,
        terminal_reason = CASE
            WHEN candidate.terminal THEN 'claim lease expired after max attempts'
            ELSE inbox.terminal_reason
        END,
        last_error = 'claim lease expired',
        updated_at = clock_timestamp()
    FROM candidate
    WHERE inbox.id = candidate.id
      AND inbox.status = 'processing' AND inbox.attempts = candidate.attempts
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
