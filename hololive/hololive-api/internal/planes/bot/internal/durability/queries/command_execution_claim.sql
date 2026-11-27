INSERT INTO bot_command_executions (message_id, command_kind, claim_token, claimed_at, created_at, updated_at)
VALUES (?1, ?2, ?3, ?4, ?4, ?4)
ON CONFLICT (message_id) DO NOTHING
