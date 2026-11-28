SELECT
    audit.id,
    audit.outbox_id,
    audit.decision,
    audit.observed_iris_state,
    audit.actor,
    audit.reason,
    floor(extract(epoch FROM audit.recorded_at) * 1000)::bigint
FROM public.bot_reply_outbox_resolution_audit AS audit
ORDER BY audit.id
