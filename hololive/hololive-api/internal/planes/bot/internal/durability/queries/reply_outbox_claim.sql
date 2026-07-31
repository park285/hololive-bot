WITH candidate AS MATERIALIZED (
    SELECT id
    FROM bot_reply_outbox
    WHERE status IN ('pending', 'retryable_pre_dispatch', 'outcome_unknown')
    ORDER BY created_at ASC, id ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE bot_reply_outbox AS outbox
SET status = 'submitting',
    claim_token = $1,
    lease_until = clock_timestamp() + ($2::bigint * INTERVAL '1 millisecond'),
    first_attempt_at = COALESCE(outbox.first_attempt_at, clock_timestamp()),
    attempts = outbox.attempts + 1,
    updated_at = clock_timestamp()
FROM candidate
WHERE outbox.id = candidate.id
RETURNING outbox.id,
          outbox.message_id,
          outbox.phase,
          outbox.ordinal,
          outbox.room_id,
          outbox.payload,
          outbox.client_request_id,
          outbox.attempts
