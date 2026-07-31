-- accepted는 admission만 증명하고 handoff는 증명하지 않으므로 자동 재발송 없이 manual_review로 보낸다.
-- submitting도 발송 여부가 미확정이라 안전 경계 안에서만 pending으로 되돌린다.
WITH candidate AS MATERIALIZED (
    SELECT id, status, attempts, first_attempt_at
    FROM bot_reply_outbox
    WHERE status IN ('submitting', 'accepted')
      AND lease_until <= clock_timestamp()
    ORDER BY lease_until ASC, id ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
), reclaimed AS (
    UPDATE bot_reply_outbox AS outbox
    SET status = CASE
            WHEN candidate.status = 'accepted'
                OR candidate.attempts >= $2 + outbox.operator_replay_grants
                OR candidate.first_attempt_at <= clock_timestamp() - ($3::bigint * INTERVAL '1 millisecond')
                THEN 'manual_review'
            ELSE 'pending'
        END,
        claim_token = NULL,
        lease_until = NULL,
        last_error = CASE
            WHEN candidate.status = 'accepted'
                OR candidate.attempts >= $2 + outbox.operator_replay_grants
                OR candidate.first_attempt_at <= clock_timestamp() - ($3::bigint * INTERVAL '1 millisecond')
                THEN 'automatic replay safety boundary reached'
            ELSE 'submit lease expired'
        END,
        updated_at = clock_timestamp()
    FROM candidate
    WHERE outbox.id = candidate.id
    RETURNING outbox.status, candidate.status AS source_status
)
SELECT
    count(status) FILTER (WHERE status = 'pending')::bigint AS requeued,
    count(status) FILTER (
        WHERE status = 'manual_review' AND source_status = 'accepted'
    )::bigint AS accepted_manual_review,
    count(status) FILTER (
        WHERE status = 'manual_review' AND source_status = 'submitting'
    )::bigint AS safety_manual_review
FROM reclaimed
