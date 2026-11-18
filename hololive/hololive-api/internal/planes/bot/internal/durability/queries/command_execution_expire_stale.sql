WITH candidate AS MATERIALIZED (
    SELECT id
    FROM bot_command_executions
    WHERE status = 'claimed'
      AND claimed_at <= clock_timestamp() - ($1::bigint * INTERVAL '1 millisecond')
    ORDER BY claimed_at ASC, id ASC
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
UPDATE bot_command_executions AS executions
SET status = 'outcome_unknown',
    completed_at = clock_timestamp(),
    updated_at = clock_timestamp()
FROM candidate
WHERE executions.id = candidate.id
