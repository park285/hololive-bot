WITH settled AS (
UPDATE bot_webhook_inbox
SET status = 'succeeded',
    payload = '{}'::jsonb,
    claim_token = NULL,
    lease_until = NULL,
    updated_at = clock_timestamp()
WHERE message_id = $1
  AND claim_token = $2
  AND status = 'processing'
RETURNING message_id, ordering_key, status
),
{{inbox_head_advance}}
SELECT EXISTS (SELECT 1 FROM settled)
