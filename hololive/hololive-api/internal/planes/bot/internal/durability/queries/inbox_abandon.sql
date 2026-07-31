WITH settled AS (
UPDATE bot_webhook_inbox
SET status = 'dead',
    payload = '{}'::jsonb,
    claim_token = NULL,
    lease_until = NULL,
    terminal_at = clock_timestamp(),
    terminal_reason = $3,
    last_error = $3,
    updated_at = clock_timestamp()
WHERE message_id = $1
  AND claim_token = $2
  AND status = 'processing'
RETURNING message_id, ordering_key
), successor AS MATERIALIZED (
SELECT settled.message_id AS old_message_id, settled.ordering_key, next_row.message_id
FROM settled LEFT JOIN LATERAL (
    SELECT message_id FROM bot_webhook_inbox
    WHERE ordering_key = settled.ordering_key AND message_id <> settled.message_id
      AND status IN ('pending', 'processing', 'retry')
    ORDER BY id LIMIT 1
) AS next_row ON true
), advanced AS (
UPDATE bot_webhook_heads AS head SET message_id = successor.message_id, updated_at = clock_timestamp()
FROM successor WHERE head.ordering_key = successor.ordering_key
  AND head.message_id = successor.old_message_id AND successor.message_id IS NOT NULL
), removed AS (
DELETE FROM bot_webhook_heads AS head USING successor
WHERE head.ordering_key = successor.ordering_key AND head.message_id = successor.old_message_id
  AND successor.message_id IS NULL
)
SELECT EXISTS (SELECT 1 FROM settled)
