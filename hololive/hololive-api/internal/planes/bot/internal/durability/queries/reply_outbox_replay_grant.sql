UPDATE bot_reply_outbox
SET status = 'pending',
    claim_token = NULL,
    lease_until = NULL,
    last_error = '',
    operator_replay_grants = ?2,
    available_at = ?3,
    updated_at = ?3,
    row_version = row_version + 1
WHERE id = ?1
  AND status = 'manual_review'
