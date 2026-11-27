UPDATE bot_webhook_inbox
SET status = 'succeeded',
    payload = '{}',
    claim_token = NULL,
    lease_until = NULL,
    updated_at = ?3
WHERE message_id = ?1
  AND claim_token = ?2
  AND status = 'processing'
RETURNING ordering_key
