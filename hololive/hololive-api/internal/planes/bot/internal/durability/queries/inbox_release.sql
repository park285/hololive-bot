UPDATE bot_webhook_inbox
SET status = 'retry',
    claim_token = NULL,
    lease_until = NULL,
    available_at = clock_timestamp() + ($3::bigint * INTERVAL '1 millisecond'),
    last_error = $4,
    updated_at = clock_timestamp()
WHERE message_id = $1
  AND claim_token = $2
  AND status = 'processing'
