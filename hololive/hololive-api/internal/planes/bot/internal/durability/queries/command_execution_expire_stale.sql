UPDATE bot_command_executions
SET status = 'outcome_unknown',
    completed_at = ?1,
    updated_at = ?1
WHERE id IN (
    SELECT id
    FROM bot_command_executions
    WHERE status = 'claimed'
      AND claimed_at <= ?1 - ?2
    ORDER BY claimed_at ASC, id ASC
    LIMIT ?3
)
