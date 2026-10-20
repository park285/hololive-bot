-- 만료 lease는 submitting과, 이전 바이너리가 lease를 남긴 채 기록한 accepted만 가진다. lease 없는
-- accepted는 handoff reconciler 소유라 고르지 않는다.
WITH locked AS MATERIALIZED (
    SELECT id, status, attempts, operator_replay_grants, first_attempt_at
    FROM bot_reply_outbox
    WHERE status IN ('submitting', 'accepted')
      AND (status = 'submitting' OR claim_token IS NOT NULL)
      AND lease_until <= clock_timestamp()
    ORDER BY lease_until ASC, id ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
SELECT id, status, attempts, operator_replay_grants,
       first_attempt_at <= clock_timestamp() - ($2::bigint * INTERVAL '1 millisecond') AS horizon_expired
FROM locked
