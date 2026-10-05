UPDATE bot_reply_outbox
SET status = $3,
    claim_token = NULL,
    lease_until = NULL,
    last_error = $4,
    available_at = clock_timestamp() + ($6::bigint * INTERVAL '1 millisecond'),
    updated_at = clock_timestamp()
WHERE id = $1
  AND claim_token = $2
  AND status = ANY($5::text[])
