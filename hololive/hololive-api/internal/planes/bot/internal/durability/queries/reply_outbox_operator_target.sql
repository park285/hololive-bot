SELECT status, created_at, operator_replay_grants
FROM bot_reply_outbox
WHERE id = ?1
