SELECT
    audit.id,
    audit.outbox_id,
    audit.grant_number::bigint,
    audit.event_type,
    audit.actor,
    audit.reason,
    floor(extract(epoch FROM audit.recorded_at) * 1000)::bigint
FROM public.bot_reply_outbox_replay_audit AS audit
ORDER BY audit.id
