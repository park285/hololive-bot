# Phase 04. Retention and Maintenance

## 목적

PG outbox terminal rows와 orphan events가 무기한 쌓여 성능을 악화시키는 것을 방지합니다.

## 결정

retention은 contract 변경이 아닙니다. 기존 table, 기존 index, 기존 status를 사용합니다.

권장 구현은 PG advisory lock 기반 maintenance runner입니다.

## 왜 advisory lock인가

egress lease는 Valkey 기반이고 “누가 메시지를 보낼 것인가”를 정합니다. retention은 DB 정리 작업입니다. 따라서 PG advisory lock으로 단일 실행을 보장하는 편이 더 정확합니다.

## Touch paths

```text
hololive/hololive-alarm-worker/internal/app/alarm_dispatch_maintenance.go
hololive/hololive-alarm-worker/internal/app/build_egress.go
hololive/hololive-alarm-worker/internal/app/env.go
docker-compose.prod.yml
docs/current/runbooks/alarm-dispatch-pg-outbox-cutover.md
```

## No-touch paths

```text
hololive/hololive-kakao-bot-go/scripts/migrations/058_create_alarm_dispatch_outbox.sql
hololive/hololive-kakao-bot-go/scripts/migrations/059_harden_alarm_dispatch_outbox.sql
scripts/runtime/alarm-dispatch-outbox-retention.sh
```

manual script는 emergency fallback으로 유지합니다.

## Retention defaults

```text
ALARM_DISPATCH_RETENTION_ENABLED=true
ALARM_DISPATCH_RETENTION_INTERVAL_MS=3600000
ALARM_DISPATCH_RETENTION_QUERY_TIMEOUT_MS=30000
ALARM_DISPATCH_RETENTION_LIMIT=1000
ALARM_DISPATCH_RETENTION_SENT_DAYS=90
ALARM_DISPATCH_RETENTION_DLQ_DAYS=180
ALARM_DISPATCH_RETENTION_QUARANTINED_DAYS=180
ALARM_DISPATCH_RETENTION_CANCELLED_DAYS=90
ALARM_DISPATCH_RETENTION_EVENT_DAYS=90
```

## SQL policy

### sent

```sql
WITH picked AS (
    SELECT id
    FROM alarm_dispatch_deliveries
    WHERE status = 'sent'
      AND sent_at < NOW() - ($1::INT * INTERVAL '1 day')
    ORDER BY sent_at ASC, id ASC
    LIMIT $2
)
DELETE FROM alarm_dispatch_deliveries d
USING picked
WHERE d.id = picked.id;
```

### orphan events

```sql
WITH picked AS (
    SELECT e.id
    FROM alarm_dispatch_events e
    WHERE e.created_at < NOW() - ($1::INT * INTERVAL '1 day')
      AND NOT EXISTS (
          SELECT 1
          FROM alarm_dispatch_deliveries d
          WHERE d.event_id = e.id
      )
    ORDER BY e.created_at ASC, e.id ASC
    LIMIT $2
)
DELETE FROM alarm_dispatch_events e
USING picked
WHERE e.id = picked.id;
```

## Safety rules

- 한 interval에서 무한 반복 delete 금지.
- limit 최대 10000.
- query timeout 필수.
- peak time에는 interval을 늘립니다.
- `pending`, `retry`, `leased`, `sending` 삭제 금지.
- `quarantined` retention은 운영 정책에 따라 충분히 길게 둡니다.

## 테스트

- advisory lock 획득 실패 시 delete 미실행.
- terminal status별 cleanup.
- active status 삭제 금지.
- orphan event만 삭제.
- limit 적용.
- query timeout path.

## 완료 기준

- retention이 자동화되어 있습니다.
- manual script는 fallback으로 남아 있습니다.
- retention metric과 log가 있습니다.
- active dispatch row는 삭제되지 않습니다.
