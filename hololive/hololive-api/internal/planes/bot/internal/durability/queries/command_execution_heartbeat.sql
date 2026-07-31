UPDATE bot_command_executions
SET claimed_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE message_id = $1
  AND claim_token = $2
  AND status = 'claimed'
