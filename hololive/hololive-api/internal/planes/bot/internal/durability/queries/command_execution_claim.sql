INSERT INTO bot_command_executions (message_id, command_kind, claim_token)
VALUES ($1, $2, $3)
ON CONFLICT (message_id) DO NOTHING
