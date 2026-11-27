UPDATE bot_webhook_inbox
SET status = 'dead',
    payload = '{}',
    claim_token = NULL,
    lease_until = NULL,
    terminal_at = ?3,
    terminal_reason = ?4,
    last_error = ?4,
    updated_at = ?3
WHERE message_id = ?1
  AND claim_token = ?2
  AND status = 'processing'
RETURNING ordering_key
