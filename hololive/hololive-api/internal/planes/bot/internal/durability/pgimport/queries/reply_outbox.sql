SELECT
    outbox.id,
    outbox.message_id,
    outbox.phase,
    outbox.ordinal,
    outbox.room_id,
    outbox.payload::text,
    outbox.payload_hash::text,
    outbox.client_request_id,
    outbox.status,
    outbox.attempts::bigint,
    floor(extract(epoch FROM outbox.first_attempt_at) * 1000)::bigint,
    outbox.iris_request_id,
    outbox.claim_token,
    floor(extract(epoch FROM outbox.lease_until) * 1000)::bigint,
    outbox.last_error,
    floor(extract(epoch FROM outbox.created_at) * 1000)::bigint,
    floor(extract(epoch FROM outbox.updated_at) * 1000)::bigint,
    floor(extract(epoch FROM outbox.available_at) * 1000)::bigint,
    outbox.operator_replay_grants::bigint,
    floor(extract(epoch FROM outbox.accepted_at) * 1000)::bigint,
    floor(extract(epoch FROM outbox.handoff_next_check_at) * 1000)::bigint
FROM public.bot_reply_outbox AS outbox
ORDER BY outbox.id
