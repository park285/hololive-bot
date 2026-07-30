UPDATE bot_reply_outbox
SET status = 'accepted',
    iris_request_id = $3,
    updated_at = clock_timestamp()
WHERE id = $1
  AND claim_token = $2
  AND status = 'submitting'
