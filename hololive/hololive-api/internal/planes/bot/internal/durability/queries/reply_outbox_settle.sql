UPDATE bot_reply_outbox
SET status = $3,
    claim_token = NULL,
    lease_until = NULL,
    last_error = $4,
    available_at = CASE
        WHEN $3 IN ('retryable_pre_dispatch', 'outcome_unknown')
            THEN clock_timestamp() + ($6::bigint * INTERVAL '1 millisecond')
        ELSE available_at
    END,
    payload = CASE
        WHEN $3 IN ('handoff_completed', 'dead', 'permanent_conflict') THEN NULL
        ELSE payload
    END,
    updated_at = clock_timestamp()
WHERE id = $1
  AND claim_token = $2
  AND status = ANY($5::text[])
