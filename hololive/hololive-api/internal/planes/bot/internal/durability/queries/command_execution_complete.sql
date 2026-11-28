UPDATE bot_command_executions
SET status = ?3,
    completed_at = ?4,
    updated_at = ?4
WHERE message_id = ?1
  AND claim_token = ?2
  AND status = 'claimed'
