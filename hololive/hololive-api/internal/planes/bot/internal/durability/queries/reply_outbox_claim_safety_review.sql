UPDATE bot_reply_outbox
SET status = 'manual_review',
    claim_token = NULL,
    lease_until = NULL,
    last_error = 'automatic replay safety boundary reached',
    updated_at = ?2,
    row_version = row_version + 1
WHERE id = ?1
