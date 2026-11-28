SELECT id, attempts, operator_replay_grants, first_attempt_at <= ?1 - ?3
FROM bot_reply_outbox
WHERE status = 'submitting'
  AND lease_until <= ?1
ORDER BY lease_until ASC, id ASC
LIMIT ?2
