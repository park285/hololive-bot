UPDATE bot_reply_outbox
SET status = $4,
    payload = NULL,
    handoff_next_check_at = NULL,
    last_error = $5,
    updated_at = clock_timestamp()
WHERE id = $1
  AND status = 'accepted'
  AND claim_token IS NULL
  AND handoff_next_check_at = $2
  AND iris_request_id = $3
