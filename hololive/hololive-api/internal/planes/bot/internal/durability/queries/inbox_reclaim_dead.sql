UPDATE bot_webhook_inbox
SET status = 'dead',
    payload = '{}',
    claim_token = NULL,
    lease_until = NULL,
    available_at = ?3,
    terminal_at = ?3,
    terminal_reason = 'claim lease expired after max attempts',
    last_error = 'claim lease expired',
    updated_at = ?3
WHERE id = ?1
  AND status = 'processing'
  AND attempts = ?2
RETURNING message_id, ordering_key
