-- 협조적으로 Release하는 워커는 행을 processing에 남기지 않아 lease 만료 회수가 영원히 돌지 않는다.
-- 시도 상한을 여기서 함께 보지 않으면 poison 한 건이 ordering_key 전체를 영구 정지시킨다.
WITH settled AS (
UPDATE bot_webhook_inbox
SET status = CASE WHEN attempts >= $5::int THEN 'dead' ELSE 'retry' END,
    payload = CASE WHEN attempts >= $5::int THEN '{}'::jsonb ELSE payload END,
    claim_token = NULL,
    lease_until = NULL,
    available_at = clock_timestamp() + ($3::bigint * INTERVAL '1 millisecond'),
    terminal_at = CASE WHEN attempts >= $5::int THEN clock_timestamp() ELSE terminal_at END,
    terminal_reason = CASE
        WHEN attempts >= $5::int THEN 'released after max attempts'
        ELSE terminal_reason
    END,
    last_error = $4,
    updated_at = clock_timestamp()
WHERE message_id = $1
  AND claim_token = $2
  AND status = 'processing'
RETURNING message_id, ordering_key, status
), successor AS MATERIALIZED (
SELECT settled.message_id AS old_message_id, settled.ordering_key, settled.status, next_row.message_id
FROM settled LEFT JOIN LATERAL (
    SELECT message_id FROM bot_webhook_inbox
    WHERE ordering_key = settled.ordering_key AND message_id <> settled.message_id
      AND status IN ('pending', 'processing', 'retry')
    ORDER BY id LIMIT 1
) AS next_row ON settled.status = 'dead'
), advanced AS (
UPDATE bot_webhook_heads AS head SET message_id = successor.message_id, updated_at = clock_timestamp()
FROM successor WHERE successor.status = 'dead' AND head.ordering_key = successor.ordering_key
  AND head.message_id = successor.old_message_id AND successor.message_id IS NOT NULL
), removed AS (
DELETE FROM bot_webhook_heads AS head USING successor
WHERE successor.status = 'dead' AND head.ordering_key = successor.ordering_key
  AND head.message_id = successor.old_message_id AND successor.message_id IS NULL
)
SELECT status FROM settled
