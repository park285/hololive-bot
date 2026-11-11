-- settled CTE(message_id, ordering_key, status)에 이어 붙는 head 전진 조각이다.
-- terminal로 끝난 행이 head였을 때만 다음 활성 행으로 옮기고, 활성 행이 없으면 head를 지운다.
successor AS MATERIALIZED (
SELECT settled.message_id AS old_message_id, settled.ordering_key, next_row.message_id
FROM settled LEFT JOIN LATERAL (
    SELECT message_id FROM bot_webhook_inbox
    WHERE ordering_key = settled.ordering_key AND message_id <> settled.message_id
      AND status IN ('pending', 'processing', 'retry')
    ORDER BY id LIMIT 1
) AS next_row ON true
WHERE settled.status IN ('succeeded', 'dead')
), advanced AS (
UPDATE bot_webhook_heads AS head SET message_id = successor.message_id, updated_at = clock_timestamp()
FROM successor WHERE head.ordering_key = successor.ordering_key
  AND head.message_id = successor.old_message_id AND successor.message_id IS NOT NULL
), removed AS (
DELETE FROM bot_webhook_heads AS head USING successor
WHERE head.ordering_key = successor.ordering_key AND head.message_id = successor.old_message_id
  AND successor.message_id IS NULL
)
