WITH candidate AS (
    SELECT id, attempts, target_status, terminal
    FROM unnest($1::bigint[], $2::int[], $3::text[], $4::boolean[])
        AS input(id, attempts, target_status, terminal)
), settled AS (
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
),
{{inbox_head_advance}}
SELECT
    count(status) FILTER (WHERE status = 'retry')::bigint AS requeued,
    count(status) FILTER (WHERE status = 'dead')::bigint AS abandoned
FROM settled
