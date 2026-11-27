SELECT
    inbox.id,
    inbox.message_id,
    inbox.room_id,
    inbox.ordering_key,
    inbox.payload::text,
    inbox.status,
    inbox.attempts::bigint,
    inbox.claim_token,
    floor(extract(epoch FROM inbox.lease_until) * 1000)::bigint,
    floor(extract(epoch FROM inbox.available_at) * 1000)::bigint,
    inbox.terminal_reason,
    floor(extract(epoch FROM inbox.terminal_at) * 1000)::bigint,
    inbox.last_error,
    floor(extract(epoch FROM inbox.created_at) * 1000)::bigint,
    floor(extract(epoch FROM inbox.updated_at) * 1000)::bigint
FROM public.bot_webhook_inbox AS inbox
ORDER BY inbox.id
