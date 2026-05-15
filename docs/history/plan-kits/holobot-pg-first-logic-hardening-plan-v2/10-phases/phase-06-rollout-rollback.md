# Phase 06. Rollout and Rollback

## 목적

PG-first logic hardening을 안전하게 배포하고, 문제가 생겼을 때 중복/누락 없이 후퇴할 수 있게 합니다.

## 전제

계약은 변경하지 않습니다. 배포는 로직 개선과 운영 설정 보강입니다.

## Rollout sequence

### Step 1. Preflight

```bash
go test ./hololive/hololive-alarm-worker/internal/app -count=1
go test ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox -count=1
go test ./hololive/hololive-dispatcher-go/internal/app -count=1
```

```bash
TEST_DATABASE_URL=postgres://... go test -tags=integration ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox -count=1
```

### Step 2. Schema/index check

```sql
SELECT to_regclass('alarm_dispatch_events');
SELECT to_regclass('alarm_dispatch_deliveries');

SELECT indexname
FROM pg_indexes
WHERE tablename = 'alarm_dispatch_deliveries'
ORDER BY indexname;
```

### Step 3. Legacy residue check

```bash
valkey-cli LLEN alarm:dispatch:queue
valkey-cli ZCARD alarm:dispatch:retry
valkey-cli LLEN alarm:dispatch:dlq
```

### Step 4. Mode check

```bash
docker compose -f docker-compose.prod.yml config   | grep -E 'ALARM_DISPATCH_(PUBLISH_MODE|CONSUMER_MODE)'
```

Expected:

```text
ALARM_DISPATCH_PUBLISH_MODE=pg_first
ALARM_DISPATCH_CONSUMER_MODE=pg
```

### Step 5. Deploy logic hardening

배포 대상:

```text
alarm-worker image
optional dispatcher-go image if used
```

### Step 6. Watch first 30 minutes

```sql
SELECT status, count(*)
FROM alarm_dispatch_deliveries
GROUP BY status
ORDER BY status;
```

```sql
SELECT EXTRACT(EPOCH FROM NOW() - MIN(next_attempt_at))
FROM alarm_dispatch_deliveries
WHERE status IN ('pending', 'retry')
  AND next_attempt_at <= NOW();
```

중점 metric:

```text
alarm_dispatch_pg_quarantined_total
alarm_dispatch_pg_mark_sent_failed_total
alarm_dispatch_wakeup_failed_total
alarm_dispatch_publish_hash_conflict_total
alarm_dispatch_pg_oldest_pending_age_seconds
```

## Rollback policy

### 금지 rollback

문제가 생겼다고 바로 `publisher=valkey_only`, `consumer=valkey`로 되돌리면 PG에 이미 들어간 `pending/retry` row가 stranded 될 수 있습니다.

금지:

```text
pg_first/valkey
shadow/pg
valkey_only/pg
```

### Rollback option A: stop producer, drain PG

가벼운 장애:

1. 새 alarm publish를 멈춥니다.
2. consumer는 `pg`로 유지합니다.
3. PG backlog를 drain합니다.
4. 원인 수정 후 다시 publish를 켭니다.

### Rollback option B: controlled legacy rollback

치명적 장애:

1. PG active rows를 회계 처리합니다.
2. pending/retry/leased/sending row 수와 oldest age를 기록합니다.
3. legacy Valkey residue를 확인합니다.
4. 운영자가 stranded PG row 처리 방침을 승인합니다.
5. `valkey_only/valkey`로 되돌립니다.

절대 하지 말 것:

- PG pending row를 Valkey로 임의 replay.
- shadowed row를 pending으로 promotion.
- quarantined row를 ack 없이 requeue.
- sending row를 자동 retry.

## 완료 기준

- rollback path가 mode mismatch를 만들지 않습니다.
- stranded PG rows를 기록하지 않고 legacy로 되돌리지 않습니다.
- quarantine rows는 operator ack 없이 replay하지 않습니다.
