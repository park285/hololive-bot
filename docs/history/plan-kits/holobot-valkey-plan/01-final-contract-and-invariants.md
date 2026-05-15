# 01. 최종 계약과 불변식

## 1. 시스템 역할 분리

### PostgreSQL

PostgreSQL은 알람 dispatch의 정본입니다. 실제 발송 대상은 반드시 PostgreSQL delivery row를 가져야 합니다.

담당 상태:

- logical event payload
- room별 delivery pending/retry/leased/sending/sent/dlq/quarantine 상태
- dedupe key
- lease/lock 상태
- retry schedule
- terminal audit trail

### Valkey

Valkey는 정본이 아닙니다. 없어져도 correctness가 깨지면 안 됩니다.

담당 상태:

- dispatch wakeup token
- 재구성 가능한 cache
- 재구성 가능한 subscriber/member index
- bounded local fast-path hint

금지:

- 발송 대기 payload의 유일한 저장소
- retry queue의 유일한 저장소
- DLQ의 유일한 저장소
- terminal dedupe의 유일한 저장소

### Iris

Iris는 외부 전송 시스템입니다. PostgreSQL transaction과 원자화할 수 없습니다. 따라서 `sending` 이후에는 결과 불명 상태를 별도로 다룹니다.

## 2. 핵심 불변식

### Invariant 1. PG delivery 없는 production dispatch는 없습니다

`pg_first` 이후 실제 발송 대상 알림은 반드시 `alarm_dispatch_deliveries` row를 가집니다.

위반 예:

- publisher가 Valkey queue에만 payload를 넣음
- dispatcher가 Valkey payload만 보고 Iris에 전송함
- retry/DLQ가 Valkey에만 존재함

### Invariant 2. Event payload는 room-agnostic입니다

`alarm_dispatch_events.payload`에는 `room_id`, `roomId`, room name, room별 claim key, room별 retry 정보가 들어가면 안 됩니다.

필요한 room별 데이터는 다음 중 하나로 처리합니다.

1. `alarm_dispatch_deliveries.room_id`에 저장
2. `alarm_dispatch_deliveries.claim_keys`에 저장
3. 매우 작은 `delivery_context`에 저장
4. dispatch 시점에 cache/PG에서 조회

### Invariant 3. Event와 delivery dedupe 기준을 분리합니다

`event_key`는 logical event dedupe입니다. 같은 event_key는 같은 payload_hash를 가져야 합니다.

`dedupe_key`는 room별 delivery dedupe입니다. 같은 logical event라도 room이 다르면 delivery row는 다를 수 있습니다.

예시:

```text
event_key  = live:{channel_id}:{stream_id}:{scheduled_minute}:{category}:{offset}
dedupe_key = room:{room_id}:event:{event_key}
```

### Invariant 4. Shadow row는 발송 대상이 아닙니다

shadow mode에서 생성된 delivery는 `status='shadowed'`입니다. PG consumer는 `pending`과 `retry`만 claim합니다.

`shadowed`를 자동으로 `pending`으로 승격하지 않습니다. shadow row는 legacy Valkey path에서 이미 발송되었을 수 있기 때문입니다.

### Invariant 5. Terminal status는 자동으로 되돌리지 않습니다

terminal status:

```text
sent
dlq
quarantined
cancelled
```

`ON CONFLICT` 처리나 reconciliation에서 terminal row를 자동으로 `pending` 또는 `retry`로 되돌리면 안 됩니다.

### Invariant 6. `leased`와 `sending`은 복구 정책이 다릅니다

`leased`는 외부 send 전 상태입니다. worker crash, render crash, process 종료가 발생해도 retry가 가능합니다.

`sending`은 외부 send를 시작한 상태입니다. Iris idempotency가 없는 동안에는 stale `sending`을 자동 retry하면 중복 발송 위험이 있습니다. 기본 복구 정책은 quarantine입니다.

### Invariant 7. Valkey wakeup은 유실 가능해야 합니다

Valkey wakeup token이 유실되거나 Valkey가 일시적으로 죽어도 PG fallback scan이 due delivery를 찾습니다. wakeup token에는 payload나 offset을 싣지 않습니다.

### Invariant 8. PG operation은 batch 상한을 가져야 합니다

publisher insert, dispatcher claim, status update, retention, reconciliation은 모두 explicit limit을 가집니다.

예시 기본값:

```text
ALARM_DISPATCH_MAX_EVENTS_PER_BATCH=100
ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH=1000
ALARM_DISPATCH_CLAIM_LIMIT=100
ALARM_DISPATCH_MAX_BATCHES_PER_WAKE=10
ALARM_DISPATCH_RECONCILE_LIMIT=500
ALARM_DISPATCH_RETENTION_DELETE_LIMIT=1000
```

### Invariant 9. Valkey hot path는 O(1) 계열 명령만 사용합니다

dispatch hot path에서 기본 허용:

```text
SET key value NX PX ...
GET exact-key
EXISTS exact-key
LPUSH one-token
BRPOP one-fixed-key
LPOP/RPOP one item
EXPIRE/PEXPIRE exact-key
TTL/PTTL exact-key
UNLINK exact-key for cleanup only
```

주의:

- `BRPOP`은 key 개수에 비례하므로 반드시 fixed key 1개만 넘깁니다.
- `LPUSH`는 token 1개만 넣습니다.
- `UNLINK`도 exact key만 허용합니다.

금지:

```text
KEYS
unbounded SCAN
LRANGE 0 -1
SMEMBERS on unbounded set
HGETALL on unbounded hash
ZRANGE/ZREVRANGE without small LIMIT
SORT
Lua loop over variable-size data
PUBLISH as default wakeup
```

예외는 `appendix/valkey-command-policy.md`의 예외 승인 조건을 따릅니다.

## 3. 상태 머신

```text
shadowed    : shadow 검증용. PG consumer 대상 아님.
pending     : 발송 대기.
retry       : retry schedule 대기.
leased      : dispatcher가 claim했고 외부 send 전.
sending     : 외부 send 직전 또는 진행 중.
sent        : 외부 send 성공 후 DB 기록 완료.
dlq         : deterministic failure 또는 retry exhausted.
quarantined : send 결과 불명. 자동 재전송 금지.
cancelled   : 정책적 취소.
```

허용 전이:

```text
shadowed -> terminal 없음. 수동 분석/삭제만.
pending  -> leased
retry    -> leased
leased   -> retry
leased   -> dlq
leased   -> cancelled
leased   -> sending
sending  -> sent
sending  -> quarantined
sending  -> retry        # Iris idempotency 지원 이후에만 제한 허용
sending  -> dlq          # deterministic failure가 명확할 때만
```

## 4. 운영 모드

```text
ALARM_DISPATCH_PUBLISH_MODE=valkey_only|shadow|pg_first
ALARM_DISPATCH_CONSUMER_MODE=valkey|pg
ALARM_DISPATCH_WAKEUP_ENABLED=true|false
ALARM_DISPATCH_SHADOW_FATAL=false|true
ALARM_DISPATCH_LEASE_SECONDS=60
ALARM_DISPATCH_SENDING_STALE_SECONDS=300
ALARM_DISPATCH_STALE_LEASE_POLICY=retry|dlq
ALARM_DISPATCH_STALE_SENDING_POLICY=quarantine|retry
ALARM_DISPATCH_SEND_AMBIGUOUS_ERROR_POLICY=quarantine|retry
```

기본값:

```text
PUBLISH_MODE=valkey_only
CONSUMER_MODE=valkey
WAKEUP_ENABLED=true
SHADOW_FATAL=false
STALE_LEASE_POLICY=retry
STALE_SENDING_POLICY=quarantine
SEND_AMBIGUOUS_ERROR_POLICY=quarantine
```

금지 조합:

```text
publisher=pg_first, consumer=valkey
publisher=valkey_only/shadow, consumer=pg
```

위 조합은 신규 알림 누락 또는 중복 위험을 만듭니다. canary 전환에서는 두 값을 함께 전환하거나, 명시적인 bridge/drain 절차를 둡니다.
