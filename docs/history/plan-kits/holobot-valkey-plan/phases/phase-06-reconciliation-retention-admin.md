# Phase 06. Reconciliation, retention, admin tooling

## 목표

정상 dispatcher loop만으로는 해결되지 않는 상태를 운영적으로 복구합니다. 이 phase의 핵심은 모든 작업을 bounded batch로 만드는 것입니다.

대상:

- expired `leased` 복구
- stale `sending` quarantine
- retry exhausted -> DLQ
- terminal row retention
- orphan event cleanup
- quarantine/DLQ 조회와 수동 처리 helper

## 원칙

1. recurring job은 한 번에 무제한 row를 처리하지 않습니다.
2. `UPDATE`, `DELETE`는 CTE에서 `LIMIT`으로 대상 id를 먼저 고릅니다.
3. 큰 `COUNT(*) GROUP BY status`를 짧은 주기로 실행하지 않습니다.
4. app metric을 dashboard 기본 소스로 삼고, SQL count는 저빈도 진단용으로 둡니다.
5. `sending`은 Iris idempotency 전까지 자동 retry하지 않습니다.

## RecoverExpiredLeased

`leased`는 외부 send 전입니다. lease가 만료되면 retry 가능합니다.

```sql
WITH picked AS (
    SELECT id
    FROM alarm_dispatch_deliveries
    WHERE status = 'leased'
      AND lock_expires_at < NOW()
    ORDER BY lock_expires_at ASC, id ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE alarm_dispatch_deliveries d
SET status = 'retry',
    next_attempt_at = NOW(),
    locked_by = NULL,
    locked_at = NULL,
    lock_expires_at = NULL,
    last_error_code = 'lease_expired_before_send',
    last_error = 'lease expired before external send',
    updated_at = NOW()
FROM picked
WHERE d.id = picked.id;
```

## QuarantineStaleSending

`sending`은 외부 send가 시작된 상태입니다. 결과가 불명확하므로 기본 quarantine입니다.

```sql
WITH picked AS (
    SELECT id
    FROM alarm_dispatch_deliveries
    WHERE status = 'sending'
      AND sending_started_at < NOW() - ($1::INT * INTERVAL '1 second')
    ORDER BY sending_started_at ASC, id ASC
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
UPDATE alarm_dispatch_deliveries d
SET status = 'quarantined',
    quarantined_at = NOW(),
    locked_by = NULL,
    locked_at = NULL,
    lock_expires_at = NULL,
    last_error_code = 'stale_sending_unknown_outcome',
    last_error = 'stale sending; external send outcome unknown',
    updated_at = NOW()
FROM picked
WHERE d.id = picked.id;
```

## Retry exhausted -> DLQ

retry count가 상한을 넘으면 DLQ로 보냅니다. 이 작업도 bounded입니다.

```sql
WITH picked AS (
    SELECT id
    FROM alarm_dispatch_deliveries
    WHERE status = 'retry'
      AND attempt_count >= $1
    ORDER BY updated_at ASC, id ASC
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
UPDATE alarm_dispatch_deliveries d
SET status = 'dlq',
    dlq_at = NOW(),
    last_error_code = 'retry_exhausted',
    updated_at = NOW()
FROM picked
WHERE d.id = picked.id;
```

## Terminal retention

sent/shadowed/cancelled는 비교적 짧은 retention을 둘 수 있습니다. dlq/quarantined는 운영 분석 후 더 오래 보존하거나 archive합니다.

권장:

```text
shadowed    : 7~14일
sent        : 30~90일
cancelled   : 30~90일
dlq         : 90일 이상 또는 archive
quarantined : 90일 이상 또는 archive
```

bounded delete:

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

`$2`는 application에서 계산한 cutoff timestamp입니다. SQL에서 문자열 interval을 조립하지 않습니다.

## Orphan event cleanup

delivery가 모두 삭제된 event는 일정 시간이 지난 뒤 삭제할 수 있습니다.

```sql
WITH picked AS (
    SELECT e.id
    FROM alarm_dispatch_events e
    WHERE e.created_at < NOW() - INTERVAL '90 days'
      AND NOT EXISTS (
          SELECT 1
          FROM alarm_dispatch_deliveries d
          WHERE d.event_id = e.id
      )
    ORDER BY e.id ASC
    LIMIT $1
)
DELETE FROM alarm_dispatch_events e
USING picked
WHERE e.id = picked.id;
```

`NOT EXISTS`는 index `alarm_dispatch_deliveries(event_id)`가 있어야 합니다.

## Admin 조회

운영자가 확인할 수 있어야 하는 조회:

```sql
-- 최근 quarantine
SELECT d.id, d.room_id, d.status, d.attempt_count, d.last_error_code,
       d.last_error, d.updated_at, e.event_key, e.alarm_type, e.channel_id, e.stream_id
FROM alarm_dispatch_deliveries d
JOIN alarm_dispatch_events e ON e.id = d.event_id
WHERE d.status = 'quarantined'
ORDER BY d.updated_at DESC
LIMIT 100;

-- 특정 room 최근 delivery
SELECT d.*, e.event_key, e.alarm_type
FROM alarm_dispatch_deliveries d
JOIN alarm_dispatch_events e ON e.id = d.event_id
WHERE d.room_id = $1
ORDER BY d.created_at DESC
LIMIT 100;
```

이런 조회는 admin/manual 용도입니다. dashboard hot path에서 자주 실행하지 않습니다.

## Manual requeue 정책

quarantined row를 수동 requeue할 수는 있지만, 중복 발송 위험을 운영자가 이해하고 수행해야 합니다.

권장 helper는 다음 입력을 요구합니다.

```text
delivery_id
operator_id
reason
force_duplicate_risk_ack=true
```

수동 requeue SQL은 terminal을 되돌리는 예외이므로 audit log가 필요합니다. 가능하면 별도 `alarm_dispatch_admin_actions` 테이블을 둡니다.

초기 구현에서 audit table이 없다면, 최소한 structured log와 metric을 남깁니다.

## Metrics

필수:

```text
alarm_dispatch_recovered_leased_total
alarm_dispatch_quarantined_stale_sending_total
alarm_dispatch_retention_deleted_total{status}
alarm_dispatch_orphan_events_deleted_total
alarm_dispatch_manual_requeue_total{from_status}
alarm_dispatch_reconcile_job_duration_seconds{job}
alarm_dispatch_reconcile_job_error_total{job}
```

## 테스트

필수 test:

1. RecoverExpiredLeased가 limit만큼만 retry로 전환
2. RecoverExpiredLeased가 sending에는 영향 없음
3. QuarantineStaleSending이 limit만큼만 quarantine
4. QuarantineStaleSending이 leased에는 영향 없음
5. terminal retention delete가 limit만큼만 삭제
6. orphan event cleanup이 delivery 있는 event를 삭제하지 않음
7. manual requeue는 explicit force flag 없으면 거부
8. manual requeue는 audit log/metric을 남김

## 완료 기준

- stale leased/sending 처리 가능
- retention이 bounded batch로 동작
- admin 조회/수동 처리 절차 존재
- unbounded DELETE/UPDATE 없음
- dashboard는 metric 중심

## no-go 조건

- stale sending을 기본 retry로 돌림
- unbounded DELETE/UPDATE 사용
- operator audit 없이 quarantined row를 requeue함
- dashboard에서 큰 테이블 COUNT를 짧은 주기로 실행함

## LLM 작업 프롬프트

```text
alarm dispatch reconciliation과 retention job을 구현하세요.
모든 UPDATE/DELETE는 bounded CTE + LIMIT으로 작성하세요.
leased 만료는 retry로 복구할 수 있지만, sending stale은 Iris idempotency 전까지 quarantine해야 합니다.
terminal retention도 한 번에 무제한 삭제하지 마세요.
quarantined 수동 requeue는 중복 위험 acknowledgement와 audit log/metric이 있어야 합니다.
대시보드는 DB COUNT 반복이 아니라 app metric 중심으로 설계하세요.
```
