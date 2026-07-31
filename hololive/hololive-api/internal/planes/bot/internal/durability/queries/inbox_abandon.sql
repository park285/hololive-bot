UPDATE bot_webhook_inbox
SET status = 'dead',
    claim_token = NULL,
    lease_until = NULL,
    terminal_at = clock_timestamp(),
    terminal_reason = $3,
    last_error = $3,
    updated_at = clock_timestamp()
WHERE message_id = $1
  AND claim_token = $2
  AND status = 'processing'
