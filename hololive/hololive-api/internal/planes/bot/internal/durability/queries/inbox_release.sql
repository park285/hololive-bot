UPDATE bot_webhook_inbox
SET status = 'dead',
    payload = '{}',
    claim_token = NULL,
    lease_until = NULL,
    available_at = ?3 + ?4,
    terminal_at = ?3,
    terminal_reason = 'released after max attempts',
    last_error = ?5,
    updated_at = ?3
WHERE message_id = ?1
  AND claim_token = ?2
  AND status = 'processing'
  AND attempts = ?6
RETURNING ordering_key
