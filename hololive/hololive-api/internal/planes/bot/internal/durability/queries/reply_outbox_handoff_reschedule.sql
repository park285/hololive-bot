UPDATE bot_reply_outbox
SET handoff_next_check_at = min(?5 + ?6, accepted_at + ?7),
    row_version = row_version + 1
WHERE id = ?1
  AND status = 'accepted'
  AND claim_token IS NULL
  AND handoff_next_check_at = ?2
  AND iris_request_id = ?3
  AND row_version = ?4
RETURNING max(handoff_next_check_at - ?5, 0)
