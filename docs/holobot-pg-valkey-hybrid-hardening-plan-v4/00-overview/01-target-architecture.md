# 목표 아키텍처

## 한 줄 정의

PostgreSQL은 durable dispatch ledger이고, Valkey는 payload 없는 wakeup/cache/index helper입니다. 알림 발송의 source of truth는 항상 PostgreSQL입니다.

## 최종 데이터 흐름

```text
alarm-worker
  -> PublishBatch()
  -> PG transaction
       alarm_dispatch_events      insert/upsert
       alarm_dispatch_deliveries  insert/upsert
  -> commit
  -> Valkey wakeup token best-effort

hololive-dispatcher-go
  -> Valkey BRPOP wakeup wait or timeout
  -> PG ClaimDue(limit)
  -> LoadEventsByID(distinct event IDs)
  -> render by room group
  -> MarkSending
  -> Iris send
  -> MarkSent or quarantine/retry/DLQ
```

## Valkey 역할

허용:

- `alarm:dispatch:wakeup` 단일 list token
- dedup claim cache
- subscriber/member derived cache
- admin session/rate limit 등 휘발성 상태

금지:

- pending delivery source of truth
- retry/DLQ/quarantine durable state
- in-flight send durable state
- replay 대상 payload 저장

## PG 역할

- event payload 저장
- room delivery 상태 저장
- idempotency/dedupe 판단
- retry/DLQ/quarantine/terminal 상태 저장
- reconciliation 기준
- 운영자 수동 requeue audit 기준

## wakeup invariant

Valkey wakeup은 유실 가능해야 합니다. wakeup 유실은 latency 증가만 만들고 dispatch 유실을 만들면 안 됩니다.

즉 다음이 성립해야 합니다.

```text
Valkey wakeup success     -> dispatcher가 빨리 PG scan
Valkey wakeup fail/lost   -> dispatcher가 fallback interval 후 PG scan
Valkey unavailable        -> dispatcher는 PG fallback scan으로 계속 동작
```

## fan-out invariant

하나의 logical event가 여러 room으로 나갈 때 event payload는 한 번만 저장되어야 합니다.

```text
1 event + 1,000 rooms
  alarm_dispatch_events      1 row
  alarm_dispatch_deliveries  1,000 rows
```

payload 안에는 `room_id`, `roomId`, `room`, `users` 같은 delivery-specific 필드가 들어가면 안 됩니다.
