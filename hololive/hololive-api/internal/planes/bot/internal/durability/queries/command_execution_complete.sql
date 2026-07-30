UPDATE bot_command_executions
SET status = $2,
    result_summary = $3,
    completed_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE message_id = $1
  AND status = 'claimed'
