SELECT id, client_request_id, iris_request_id, attempts, operator_replay_grants, created_at, updated_at, last_error
FROM bot_reply_outbox
WHERE status = 'manual_review'
ORDER BY updated_at, id
LIMIT ?1
