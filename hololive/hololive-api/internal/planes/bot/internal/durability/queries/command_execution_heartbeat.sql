UPDATE bot_command_executions
SET claimed_at = ?3,
    updated_at = ?3
WHERE message_id = ?1
  AND claim_token = ?2
  AND status = 'claimed'
