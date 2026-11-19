WITH clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
)
UPDATE bot_reply_outbox
SET status = 'accepted',
    iris_request_id = $3,
    claim_token = NULL,
    lease_until = NULL,
    accepted_at = clock.now,
    handoff_next_check_at = clock.now + ($4::bigint * INTERVAL '1 millisecond'),
    last_error = '',
    updated_at = clock.now
FROM clock
WHERE id = $1
  AND claim_token = $2
  AND status = 'submitting'
