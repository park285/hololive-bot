SELECT
    command.id,
    command.message_id,
    command.command_kind,
    command.status,
    command.claim_token,
    floor(extract(epoch FROM command.claimed_at) * 1000)::bigint,
    floor(extract(epoch FROM command.completed_at) * 1000)::bigint,
    floor(extract(epoch FROM command.created_at) * 1000)::bigint,
    floor(extract(epoch FROM command.updated_at) * 1000)::bigint
FROM public.bot_command_executions AS command
ORDER BY command.id
