WITH due AS MATERIALIZED (
    SELECT id
    FROM bot_reply_outbox
    WHERE status = 'accepted'
      AND claim_token IS NULL
      AND handoff_next_check_at <= clock_timestamp()
    ORDER BY handoff_next_check_at ASC, id ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE bot_reply_outbox AS outbox
SET handoff_next_check_at = clock_timestamp() + ($2::bigint * INTERVAL '1 millisecond')
FROM due
WHERE outbox.id = due.id
RETURNING outbox.id,
          outbox.iris_request_id,
          outbox.handoff_next_check_at,
          floor(EXTRACT(EPOCH FROM (clock_timestamp() - outbox.accepted_at)) * 1000)::bigint,
          outbox.attempts,
          outbox.operator_replay_grants,
          COALESCE(outbox.first_attempt_at <= clock_timestamp() - ($3::bigint * INTERVAL '1 millisecond'), true)
