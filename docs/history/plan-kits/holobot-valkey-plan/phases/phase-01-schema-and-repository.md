# Phase 01. PostgreSQL schema와 repository 추가

## 목표

`alarm_dispatch_events`와 `alarm_dispatch_deliveries`를 추가합니다. 이 phase에서는 runtime publish/dispatch 동작을 아직 바꾸지 않습니다. schema, domain model, repository, SQL test만 추가합니다.

V2의 단일 `alarm_dispatch_outbox`를 그대로 유지하거나 새로 만들지 않습니다. 최종 production 기본안은 2테이블입니다.

## 설계 원칙

1. event payload는 한 번만 저장합니다.
2. delivery row는 room별 상태만 저장합니다.
3. `event_key`와 `dedupe_key`를 분리합니다.
4. `payload_hash` mismatch는 조용히 overwrite하지 않습니다.
5. claim query는 delivery row만 반환합니다.
6. event payload는 distinct event id로 별도 조회합니다.
7. 모든 repository method는 batch limit을 받습니다.

## migration

SQL template은 `sql/001_alarm_dispatch_events_deliveries.sql`에 포함되어 있습니다. 실제 migration 시스템의 naming convention에 맞게 복사하세요.

핵심 DDL:

```sql
CREATE TABLE alarm_dispatch_events (...);
CREATE TABLE alarm_dispatch_deliveries (...);
CREATE UNIQUE INDEX ...;
CREATE INDEX idx_alarm_dispatch_deliveries_due ... WHERE status IN ('pending','retry');
```

중요한 제약:

```sql
UNIQUE(event_key)
UNIQUE(dedupe_key)
CHECK(status IN (...))
CHECK(payload_hash ~ '^[0-9a-f]{64}$')
CHECK(NOT (payload ? 'room_id') AND NOT (payload ? 'roomId'))
```

`payload`에서 room 관련 key를 전부 SQL check로 완벽히 막기는 어렵습니다. 그래도 `room_id`, `roomId` 정도는 check로 막고, domain validation에서 더 강하게 검증합니다.

## repository interface 권장안

패키지 이름은 기존 repository 구조에 맞춰 조정합니다. 예시는 다음입니다.

```go
type DispatchLedgerRepository interface {
    InsertBatch(ctx context.Context, input PublishBatchInput) (PublishBatchResult, error)

    ClaimDue(ctx context.Context, workerID string, limit int, lease time.Duration) ([]DeliveryRecord, error)
    LoadEventsByID(ctx context.Context, eventIDs []int64) (map[int64]EventRecord, error)

    MarkSending(ctx context.Context, deliveryIDs []int64, workerID string, extendLease time.Duration) error
    MarkSent(ctx context.Context, deliveryIDs []int64, workerID string) error

    ScheduleRetry(ctx context.Context, updates []RetryUpdate, workerID string) error
    MoveToDLQ(ctx context.Context, updates []TerminalUpdate, workerID string) error
    Quarantine(ctx context.Context, updates []TerminalUpdate, workerID string) error
    Cancel(ctx context.Context, updates []TerminalUpdate, workerID string) error

    RecoverExpiredLeased(ctx context.Context, limit int) (int, error)
    QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int, error)

    DeleteTerminalDeliveries(ctx context.Context, status DeliveryStatus, olderThan time.Duration, limit int) (int, error)
    DeleteOrphanEvents(ctx context.Context, olderThan time.Duration, limit int) (int, error)
}
```

## InsertBatch 세부 로직

`InsertBatch`는 반드시 하나의 transaction 안에서 처리합니다.

```text
1. validate input size <= configured max
2. validate event payload room-agnostic
3. canonical JSON 생성
4. sha256 payload_hash 계산
5. tx begin
6. events insert multi-row ON CONFLICT DO NOTHING RETURNING id,event_key,payload_hash
7. event_key list로 existing rows 조회
8. input hash와 DB hash mismatch 검사
9. deliveries insert multi-row ON CONFLICT DO NOTHING RETURNING id,dedupe_key
10. tx commit
11. result에 inserted/duplicate/hashConflict 수 반환
```

주의: `ON CONFLICT DO UPDATE SET payload = EXCLUDED.payload`는 금지입니다. 같은 `event_key`의 payload가 달라졌다면 덮어쓰지 말고 설계 오류로 봅니다.

## ClaimDue SQL 원칙

`ClaimDue`는 `FOR UPDATE SKIP LOCKED`를 사용합니다. 여러 dispatcher가 동시에 claim해도 같은 delivery를 가져가면 안 됩니다.

claim 결과는 delivery만 반환합니다. event payload join 금지입니다.

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
SELECT * FROM updated
ORDER BY next_attempt_at ASC, id ASC;
```

## MarkSending 원칙

외부 Iris send 직전에만 호출합니다.

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

업데이트된 row 수가 요청한 delivery id 수와 다르면 Iris send를 시작하지 않습니다. 일부 row의 lease가 만료되었거나 다른 reconciliation과 충돌했을 수 있습니다.

## MarkSent 원칙

`MarkSent`는 `sending + locked_by 일치`에서만 성공합니다.

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

`leased`에서 바로 `sent`로 바꾸지 않습니다. send 전/후 경계를 명확히 해야 합니다.

## 테스트

필수 repository test:

1. 같은 event 1개, room 1000개 publish 시 events 1 row, deliveries 1000 row
2. 같은 event_key + 같은 payload_hash는 duplicate로 안전하게 처리
3. 같은 event_key + 다른 payload_hash는 error/hashConflict
4. 같은 dedupe_key 두 번 insert 시 두 번째는 duplicate
5. shadowed row는 ClaimDue에서 제외
6. pending/retry row만 ClaimDue 대상
7. 동시 ClaimDue 두 개가 같은 delivery를 claim하지 않음
8. MarkSending은 leased+locked_by 일치에서만 성공
9. MarkSent는 sending+locked_by 일치에서만 성공
10. stale leased recover는 retry로 전환
11. stale sending quarantine은 quarantined로 전환
12. retention delete는 limit만큼만 삭제

## 완료 기준

- migration 적용/rollback 절차 문서화
- repository test 통과
- runtime publish/dispatch 동작 변경 없음
- claim query가 event payload를 join하지 않음
- unbounded cleanup query가 없음

## no-go 조건

- 단일 `alarm_dispatch_outbox` 중심으로 구현함
- event payload에 room_id를 저장함
- `ON CONFLICT DO UPDATE`로 payload를 덮어씀
- `ClaimDue`가 event payload까지 join해서 반환함
- status update가 locked_by를 확인하지 않음
- retry/quarantine/retention query에 limit이 없음

## LLM 작업 프롬프트

```text
PostgreSQL alarm dispatch ledger schema와 repository를 추가하세요.
최종 구조는 alarm_dispatch_events + alarm_dispatch_deliveries 2테이블입니다.
단일 alarm_dispatch_outbox를 새 최종안으로 만들지 마세요.
이 phase에서는 runtime publish/dispatch 동작을 바꾸지 마세요.

핵심 요구사항:
- event payload는 room-agnostic이어야 합니다.
- event_key는 logical event dedupe, dedupe_key는 room delivery dedupe입니다.
- InsertBatch는 transaction 안에서 events와 deliveries를 batch insert합니다.
- 같은 event_key에 payload_hash가 다르면 overwrite하지 말고 error로 처리합니다.
- ClaimDue는 delivery row만 반환하고 event payload를 join하지 않습니다.
- MarkSending은 leased+locked_by에서만 됩니다.
- MarkSent는 sending+locked_by에서만 됩니다.
- stale sending은 기본 quarantine입니다.
- 모든 cleanup/reconciliation SQL은 bounded limit을 사용합니다.

완료 후 repository test를 추가하고 실행하세요.
```
