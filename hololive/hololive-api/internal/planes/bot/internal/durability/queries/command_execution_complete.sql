UPDATE bot_command_executions
SET status = $3,
    result_summary = $4,
    completed_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE message_id = $1
  AND claim_token = $2
  AND status = 'claimed'
