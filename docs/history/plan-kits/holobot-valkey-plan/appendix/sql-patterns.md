# Appendix. SQL patterns

## 1. Event insert pattern

목표는 같은 event payload를 한 번만 저장하는 것입니다. 같은 event_key에 다른 payload_hash가 들어오면 덮어쓰지 않습니다.

```sql
INSERT INTO alarm_dispatch_events (
    event_key,
    payload_hash,
    alarm_type,
    channel_id,
    stream_id,
    category,
    payload_schema_version,
    payload
)
VALUES
    -- multi-row values
ON CONFLICT (event_key) DO NOTHING
RETURNING id, event_key, payload_hash;
```

그 다음 event_key 목록으로 기존 row를 조회합니다.

```sql
SELECT id, event_key, payload_hash
FROM alarm_dispatch_events
WHERE event_key = ANY($1);
```

application에서 input payload_hash와 DB payload_hash를 비교합니다. mismatch면 delivery insert를 진행하지 않습니다.

## 2. Delivery insert pattern

```sql
INSERT INTO alarm_dispatch_deliveries (
    event_id,
    room_id,
    dedupe_key,
    claim_keys,
    status,
    attempt_count,
    next_attempt_at
)
VALUES
    -- multi-row values
ON CONFLICT (dedupe_key) DO NOTHING
RETURNING id, dedupe_key, event_id, room_id, status;
```

`status`는 mode에 따라 다릅니다.

```text
shadow   -> shadowed
pg_first -> pending
```

## 3. ClaimDue pattern

```sql
WITH picked AS (
    SELECT id
    FROM alarm_dispatch_deliveries
    WHERE status IN ('pending', 'retry')
      AND next_attempt_at <= NOW()
    ORDER BY next_attempt_at ASC, id ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
), updated AS (
    UPDATE alarm_dispatch_deliveries d
    SET status = 'leased',
        locked_by = $2,
        locked_at = NOW(),
        lock_expires_at = NOW() + ($3::INT * INTERVAL '1 second'),
        updated_at = NOW()
    FROM picked
    WHERE d.id = picked.id
    RETURNING d.*
)
SELECT *
FROM updated
ORDER BY next_attempt_at ASC, id ASC;
```

## 4. LoadEventsByID pattern

```sql
SELECT id, event_key, payload_hash, alarm_type, channel_id, stream_id,
       category, payload_schema_version, payload, created_at, updated_at
FROM alarm_dispatch_events
WHERE id = ANY($1);
```

`$1`은 distinct event_id list입니다.

## 5. MarkSending pattern

```sql
UPDATE alarm_dispatch_deliveries
SET status = 'sending',
    sending_started_at = NOW(),
    lock_expires_at = NOW() + ($3::INT * INTERVAL '1 second'),
    updated_at = NOW()
WHERE id = ANY($1)
  AND status = 'leased'
  AND locked_by = $2;
```

application은 updated row count가 요청 id 수와 같은지 확인합니다. 다르면 Iris send 금지입니다.

## 6. MarkSent pattern

```sql
UPDATE alarm_dispatch_deliveries
SET status = 'sent',
    sent_at = NOW(),
    locked_by = NULL,
    locked_at = NULL,
    lock_expires_at = NULL,
    updated_at = NOW()
WHERE id = ANY($1)
  AND status = 'sending'
  AND locked_by = $2;
```

## 7. ScheduleRetry pattern

```sql
UPDATE alarm_dispatch_deliveries
SET status = 'retry',
    attempt_count = attempt_count + 1,
    next_attempt_at = $3,
    locked_by = NULL,
    locked_at = NULL,
    lock_expires_at = NULL,
    last_error_code = $4,
    last_error = $5,
    updated_at = NOW()
WHERE id = ANY($1)
  AND status = 'leased'
  AND locked_by = $2;
```

주의: 기본적으로 `sending`에서 retry로 되돌리지 않습니다. Iris idempotency 도입 후 별도 정책으로만 허용합니다.

## 8. MoveToDLQ pattern

```sql
UPDATE alarm_dispatch_deliveries
SET status = 'dlq',
    dlq_at = NOW(),
    locked_by = NULL,
    locked_at = NULL,
    lock_expires_at = NULL,
    last_error_code = $3,
    last_error = $4,
    updated_at = NOW()
WHERE id = ANY($1)
  AND status IN ('leased', 'retry')
  AND (locked_by = $2 OR locked_by IS NULL);
```

실제 구현에서는 상태별 method를 나누는 편이 더 안전할 수 있습니다.

## 9. Quarantine pattern

```sql
UPDATE alarm_dispatch_deliveries
SET status = 'quarantined',
    quarantined_at = NOW(),
    locked_by = NULL,
    locked_at = NULL,
    lock_expires_at = NULL,
    last_error_code = $3,
    last_error = $4,
    updated_at = NOW()
WHERE id = ANY($1)
  AND status = 'sending'
  AND locked_by = $2;
```

## 10. Bounded retention pattern

```sql
WITH picked AS (
    SELECT id
    FROM alarm_dispatch_deliveries
    WHERE status = $1
      AND updated_at < $2
    ORDER BY id ASC
    LIMIT $3
)
DELETE FROM alarm_dispatch_deliveries d
USING picked
WHERE d.id = picked.id;
```

## 11. Bounded orphan event cleanup

```sql
WITH picked AS (
    SELECT e.id
    FROM alarm_dispatch_events e
    WHERE e.created_at < $1
      AND NOT EXISTS (
          SELECT 1
          FROM alarm_dispatch_deliveries d
          WHERE d.event_id = e.id
      )
    ORDER BY e.id ASC
    LIMIT $2
)
DELETE FROM alarm_dispatch_events e
USING picked
WHERE e.id = picked.id;
```

## 12. 금지 SQL pattern

recurring job에서 금지:

```sql
DELETE FROM alarm_dispatch_deliveries WHERE status='sent';
UPDATE alarm_dispatch_deliveries SET status='retry' WHERE status='leased';
SELECT status, COUNT(*) FROM alarm_dispatch_deliveries GROUP BY status; -- high-frequency dashboard 금지
```

수동 진단으로는 가능하더라도, 반복 job/dashboard hot path에 넣지 않습니다.
