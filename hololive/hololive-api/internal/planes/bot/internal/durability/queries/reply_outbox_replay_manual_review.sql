SELECT public.grant_bot_reply_outbox_manual_replay(
    :'outbox_id'::bigint,
    :'operator_actor'::text,
    :'operator_reason'::text
) AS outcome;
