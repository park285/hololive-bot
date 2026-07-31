UPDATE bot_webhook_inbox
SET lease_until = clock_timestamp() + ($3::bigint * INTERVAL '1 millisecond'),
    updated_at = clock_timestamp()
WHERE message_id = $1
  AND claim_token = $2
  AND status = 'processing'
RETURNING lease_until
