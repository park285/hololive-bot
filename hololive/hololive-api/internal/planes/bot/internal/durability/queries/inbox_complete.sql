UPDATE bot_webhook_inbox
SET status = 'succeeded',
    claim_token = NULL,
    lease_until = NULL,
    updated_at = clock_timestamp()
WHERE message_id = $1
  AND claim_token = $2
  AND status = 'processing'
