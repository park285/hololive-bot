SELECT
    head.ordering_key,
    head.message_id,
    floor(extract(epoch FROM head.updated_at) * 1000)::bigint
FROM public.bot_webhook_heads AS head
ORDER BY head.ordering_key
