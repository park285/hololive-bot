SELECT status, claimed_at
FROM bot_command_executions
WHERE message_id = $1
