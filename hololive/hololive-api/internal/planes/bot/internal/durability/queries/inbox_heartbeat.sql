UPDATE bot_webhook_inbox
SET lease_until = ?3 + ?4,
    updated_at = ?3
WHERE message_id = ?1
  AND claim_token = ?2
  AND status = 'processing'
RETURNING lease_until
