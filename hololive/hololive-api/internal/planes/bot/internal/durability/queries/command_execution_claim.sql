INSERT INTO bot_command_executions (message_id, command_kind)
VALUES ($1, $2)
ON CONFLICT (message_id) DO NOTHING
