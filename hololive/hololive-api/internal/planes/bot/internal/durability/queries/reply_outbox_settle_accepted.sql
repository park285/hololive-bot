UPDATE bot_reply_outbox
SET status = 'accepted',
    iris_request_id = ?3,
    claim_token = NULL,
    lease_until = NULL,
    accepted_at = ?4,
    handoff_next_check_at = ?4 + ?5,
    last_error = '',
    updated_at = ?4,
    row_version = row_version + 1
WHERE id = ?1
  AND claim_token = ?2
  AND status = 'submitting'
