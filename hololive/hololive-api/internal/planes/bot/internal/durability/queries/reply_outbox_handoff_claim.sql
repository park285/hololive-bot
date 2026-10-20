-- lease 없는 accepted 행을 확인 예정 시각 순으로 고르고 handoff_next_check_at을 앞으로 밀어 soft claim한다.
-- 밀어 둔 값이 이후 전이의 CAS 토큰이다. 경과 시간과 재발송 지평은 DB 시계 하나로 계산한다.
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
