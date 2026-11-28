DELETE FROM bot_command_executions
WHERE id IN (
    SELECT id
    FROM bot_command_executions
    WHERE status IN ('succeeded', 'failed', 'outcome_unknown')
      AND updated_at < ?1
    ORDER BY updated_at ASC, id ASC
    LIMIT ?2
)
