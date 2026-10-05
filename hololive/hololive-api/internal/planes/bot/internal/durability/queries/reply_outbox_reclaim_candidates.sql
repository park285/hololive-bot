WITH locked AS MATERIALIZED (
    SELECT id, status, attempts, operator_replay_grants, first_attempt_at
    FROM bot_reply_outbox
    WHERE status IN ('submitting', 'accepted')
      AND lease_until <= clock_timestamp()
    ORDER BY lease_until ASC, id ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
SELECT id, status, attempts, operator_replay_grants,
       first_attempt_at <= clock_timestamp() - ($2::bigint * INTERVAL '1 millisecond') AS horizon_expired
FROM locked
