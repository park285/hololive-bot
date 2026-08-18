SELECT public.discard_bot_reply_outbox_manual_review(
    :'outbox_id'::bigint,
    :'operator_actor'::text,
    :'operator_reason'::text,
    :'observed_iris_state'::text
) AS outcome;
