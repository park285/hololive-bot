-- 결과를 모르는 claim을 되살리면 이미 발송된 응답이 다시 나가므로, 회수는 하지 않고 terminal로만 닫는다.
-- UNIQUE(message_id) + ON CONFLICT DO NOTHING 때문에 닫힌 뒤에도 재claim은 계속 0 rows다.
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
    result_summary = 'outcome_unknown',
    completed_at = clock_timestamp(),
    updated_at = clock_timestamp()
FROM candidate
WHERE executions.id = candidate.id
