-- 만료 lease는 submitting만 가진다. accepted는 lease 없이 handoff reconciler가 소유한다.
WITH locked AS MATERIALIZED (
    SELECT id, attempts, operator_replay_grants, first_attempt_at
    FROM bot_reply_outbox
    WHERE status = 'submitting'
      AND lease_until <= clock_timestamp()
    ORDER BY lease_until ASC, id ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
SELECT id, attempts, operator_replay_grants,
       first_attempt_at <= clock_timestamp() - ($2::bigint * INTERVAL '1 millisecond') AS horizon_expired
FROM locked
