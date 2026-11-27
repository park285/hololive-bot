UPDATE bot_reply_outbox
SET status = 'pending',
    claim_token = NULL,
    lease_until = NULL,
    last_error = 'submit lease expired',
    updated_at = ?2,
    row_version = row_version + 1
WHERE id = ?1
  AND status = 'submitting'
  AND first_attempt_at > ?2 - ?3
