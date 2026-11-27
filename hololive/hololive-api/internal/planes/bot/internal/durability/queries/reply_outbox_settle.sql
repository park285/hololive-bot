UPDATE bot_reply_outbox
SET status = ?3,
    claim_token = NULL,
    lease_until = NULL,
    last_error = ?4,
    updated_at = ?5,
    row_version = row_version + 1
WHERE id = ?1
  AND claim_token = ?2
  AND status = 'submitting'
