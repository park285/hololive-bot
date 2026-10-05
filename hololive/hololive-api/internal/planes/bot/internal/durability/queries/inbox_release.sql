-- 협조적으로 Release하는 워커는 행을 processing에 남기지 않아 lease 만료 회수가 영원히 돌지 않는다.
-- Go가 소진을 판정한 claim의 횟수를 검증하고 terminal 저장과 다음 head 이동을 함께 처리한다.
WITH settled AS (
UPDATE bot_webhook_inbox
SET status = 'dead',
    payload = '{}'::jsonb,
    claim_token = NULL,
    lease_until = NULL,
    available_at = clock_timestamp() + ($3::bigint * INTERVAL '1 millisecond'),
    terminal_at = clock_timestamp(),
    terminal_reason = 'released after max attempts',
    last_error = $4,
    updated_at = clock_timestamp()
WHERE message_id = $1
  AND claim_token = $2
  AND status = 'processing'
  AND attempts = $5
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
