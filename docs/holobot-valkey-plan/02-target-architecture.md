# 02. 목표 아키텍처

## 1. 전체 흐름

```text
alarm-worker / scheduler
        |
        | PublishBatch(notifications)
        v
PostgreSQL transaction
        |
        | insert alarm_dispatch_events
        | insert alarm_dispatch_deliveries
        | commit
        v
Valkey wakeup token, best effort
        |
        v
dispatcher-go
        |
        | wait wakeup or fallback timeout
        | ClaimDue from PG
        | LoadEventsByID distinct event IDs
        | render/group/send
        | MarkSending -> Iris send -> MarkSent
        v
Iris
```

## 2. 핵심 테이블

### `alarm_dispatch_events`

logical event payload를 저장합니다. 같은 event가 여러 room으로 fan-out되어도 payload는 한 번만 저장합니다.

중요 필드:

```text
id
event_key
payload_hash
alarm_type
channel_id
stream_id
category
payload_schema_version
payload
created_at
updated_at
```

`event_key`는 logical event의 dedupe key입니다. 같은 event_key로 다른 payload_hash가 들어오면 update하지 말고 오류 또는 conflict metric으로 처리합니다.

### `alarm_dispatch_deliveries`

room별 delivery 상태를 저장합니다. 실제 dispatch 대상은 이 테이블입니다.

중요 필드:

```text
id
event_id
room_id
dedupe_key
claim_keys
status
attempt_count
next_attempt_at
locked_by
locked_at
lock_expires_at
sending_started_at
sent_at
dlq_at
quarantined_at
cancelled_at
last_error_code
last_error
created_at
updated_at
```

`dedupe_key`는 room delivery dedupe입니다. `UNIQUE(dedupe_key)`가 최종 중복 가드입니다.

## 3. Publisher detail

publisher는 event 단위로 grouping합니다.

```text
input notifications
  -> validate
  -> build canonical event payload
  -> compute event_key, payload_hash
  -> build delivery rows
  -> chunk by max events/deliveries
  -> transaction
       insert events ON CONFLICT DO NOTHING
       select event ids by event_key
       verify payload_hash matches
       insert deliveries ON CONFLICT DO NOTHING
     commit
  -> best-effort Valkey wakeup
```

중요한 점은 `PublishBatch()` 내부에서 `Publish()`를 반복 호출하지 않는 것입니다. batch SQL을 사용해야 합니다.

## 4. Dispatcher detail

PG consumer는 due delivery만 claim합니다. claim 결과에 event payload를 join하지 않습니다. 먼저 delivery rows만 가져온 뒤, distinct event_id 목록으로 events를 따로 조회합니다.

```text
ClaimDue(limit)
  -> delivery rows only

LoadEventsByID(unique event ids)
  -> event map

BuildSendGroups(deliveries, eventMap)
  -> room별 또는 Iris API 요구 형태별 group

For each group:
  MarkSending(delivery ids)
  Iris send
  MarkSent(delivery ids)
```

claim SQL에서 event payload까지 join하면 같은 payload가 room 수만큼 네트워크로 반환됩니다. 2테이블 구조의 이점이 줄어드므로 피합니다.

## 5. Wakeup detail

기본 wakeup은 payload 없는 list token입니다.

```text
key: alarm:dispatch:wakeup
value: "1"
```

publisher는 commit 이후에만 wakeup을 보냅니다. commit 전 wakeup은 dispatcher가 깨어났는데 row가 아직 보이지 않는 race를 만들 수 있습니다.

wakeup 실패는 publish 실패가 아닙니다. fallback scan이 있으므로 wakeup 실패는 지연만 만들고 유실을 만들지 않아야 합니다.

## 6. Complexity policy detail

Valkey hot path는 O(1) 계열만 씁니다.

wakeup 기본 구현은 다음을 권장합니다.

```text
SET alarm:dispatch:wakeup:guard 1 NX PX 3000
성공한 경우:
  LPUSH alarm:dispatch:wakeup 1
  PEXPIRE alarm:dispatch:wakeup 2500
```

중요한 점은 list TTL이 guard TTL보다 짧거나 같아야 한다는 것입니다. dispatcher가 죽어 있어도 wakeup list가 무한히 쌓이지 않습니다. wakeup은 durable queue가 아니므로 token이 만료되어도 괜찮습니다.

consumer는 반드시 single fixed key만 block합니다.

```text
BRPOP alarm:dispatch:wakeup <timeout>
```

## 7. Delivery state와 Iris ambiguity

외부 send 전까지는 retry 가능합니다. 외부 send를 시작한 뒤 결과를 모르면 retry하면 안 됩니다.

```text
leased  : retry 가능
sending : 기본 quarantine
```

Iris idempotency 도입 전:

```text
network timeout       -> quarantined
connection reset      -> quarantined
process crash sending -> quarantined by reconciliation
send success + MarkSent failure -> stale sending -> quarantined
```

Iris idempotency 도입 후:

- per-delivery send: `Idempotency-Key: alarm-delivery-{delivery_id}`
- group send 유지: send attempt ledger 추가 후 `Idempotency-Key: alarm-send-{send_attempt_id}`

## 8. 확장 방향

초기 V3는 2테이블 구조입니다. 추후 필요하면 세 번째 테이블을 추가합니다.

```text
alarm_dispatch_send_attempts
```

이 테이블은 Iris group send idempotency, request/response audit, retryable send attempt를 정확히 관리하기 위한 것입니다. 단, 현재 단계에서는 복잡도를 줄이기 위해 도입하지 않습니다. Iris idempotency가 실제로 지원될 때 추가하는 것이 좋습니다.
