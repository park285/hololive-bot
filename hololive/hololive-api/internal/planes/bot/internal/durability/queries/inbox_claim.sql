-- 같은 ordering_key의 head 행만 후보로 삼는다. blocker의 status 집합이 pending→processing 전이를
-- 모두 덮으므로, 앞선 claim이 아직 커밋되지 않은 스냅샷에서도 뒤 행은 계속 막혀 순서가 보장된다.
WITH candidate AS MATERIALIZED (
    SELECT due.id
    FROM bot_webhook_inbox AS due
    WHERE due.status IN ('pending', 'retry')
      AND due.available_at <= clock_timestamp()
      AND NOT EXISTS (
          SELECT 1
          FROM bot_webhook_inbox AS blocker
          WHERE blocker.ordering_key = due.ordering_key
            AND blocker.id < due.id
            AND blocker.status IN ('pending', 'processing', 'retry')
      )
    ORDER BY due.available_at ASC, due.id ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE bot_webhook_inbox AS inbox
SET status = 'processing',
    claim_token = $1,
    lease_until = clock_timestamp() + ($2::bigint * INTERVAL '1 millisecond'),
    attempts = inbox.attempts + 1,
    updated_at = clock_timestamp()
FROM candidate
WHERE inbox.id = candidate.id
RETURNING inbox.message_id, inbox.room_id, inbox.ordering_key, inbox.payload, inbox.attempts
