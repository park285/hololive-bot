WITH candidate AS MATERIALIZED (
    SELECT candidate.id,
           COALESCE(
               candidate.attempts >= $3 + candidate.operator_replay_grants
               OR candidate.first_attempt_at <= clock_timestamp() - ($4::bigint * INTERVAL '1 millisecond'),
               false
           ) AS safety_expired
    FROM bot_reply_outbox AS candidate
    WHERE candidate.status IN ('pending', 'retryable_pre_dispatch', 'outcome_unknown')
      AND candidate.available_at <= clock_timestamp()
      AND NOT EXISTS (
          SELECT 1
          FROM bot_reply_outbox AS predecessor
          WHERE predecessor.room_id = candidate.room_id
            AND predecessor.id < candidate.id
            AND predecessor.status IN (
                'pending', 'submitting', 'accepted', 'retryable_pre_dispatch', 'outcome_unknown'
            )
      )
    ORDER BY candidate.available_at ASC, candidate.id ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
), transitioned AS (
    UPDATE bot_reply_outbox AS outbox
    SET status = CASE WHEN candidate.safety_expired THEN 'manual_review' ELSE 'submitting' END,
        claim_token = CASE WHEN candidate.safety_expired THEN NULL ELSE $1 END,
        lease_until = CASE
            WHEN candidate.safety_expired THEN NULL
            ELSE clock_timestamp() + ($2::bigint * INTERVAL '1 millisecond')
        END,
        first_attempt_at = CASE
            WHEN candidate.safety_expired THEN outbox.first_attempt_at
            ELSE COALESCE(outbox.first_attempt_at, clock_timestamp())
        END,
        attempts = CASE WHEN candidate.safety_expired THEN outbox.attempts ELSE outbox.attempts + 1 END,
        last_error = CASE
            WHEN candidate.safety_expired THEN 'automatic replay safety boundary reached'
            ELSE outbox.last_error
        END,
        updated_at = clock_timestamp()
    FROM candidate
    WHERE outbox.id = candidate.id
    RETURNING outbox.*
)
SELECT id, message_id, phase, ordinal, room_id, payload, client_request_id, attempts
FROM transitioned
WHERE status = 'submitting'
