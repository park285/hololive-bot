UPDATE bot_reply_outbox
SET status = 'discarded',
    payload = NULL,
    claim_token = NULL,
    lease_until = NULL,
    last_error = 'operator discarded manual review without replay',
    updated_at = ?2,
    row_version = row_version + 1
WHERE id = ?1
  AND status = 'manual_review'
