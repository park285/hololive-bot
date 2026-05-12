# 00. V2 재검수 결과와 V3 전환 이유

## 1. V2의 장점

기존 V2 계획은 중요한 방향을 이미 잡고 있었습니다. Valkey를 durable store로 만들지 않고 PostgreSQL을 정본으로 삼는 점, shadow mode에서 중복 발송을 피하려고 `shadowed` 상태를 둔 점, `leased`와 `sending`을 분리한 점, dispatcher-go의 PostgreSQL wiring을 별도 페이즈로 본 점은 모두 맞습니다.

또한 V2는 “전환 중 사고를 줄이는 최소안”으로는 적합합니다. 단일 `alarm_dispatch_outbox`는 이해하기 쉽고, 빠르게 구현할 수 있으며, cutover 전후의 상태를 한 테이블에서 볼 수 있습니다.

하지만 사용자님이 지적한 부하 모델을 기준으로 보면, V2는 최종 production 설계로는 부족합니다.

## 2. V2의 핵심 한계

### 2.1 단일 outbox는 room fan-out에서 payload 중복이 큽니다

V2의 단일 `alarm_dispatch_outbox`는 사실상 room 단위 delivery row입니다. 이 구조에서는 같은 logical event가 여러 room으로 퍼질 때 event payload JSON이 room 수만큼 저장됩니다.

예를 들어 하나의 live reminder가 3,000개 room으로 전파되면 다음 문제가 생깁니다.

- 같은 payload JSON이 3,000번 저장됩니다.
- PostgreSQL WAL이 불필요하게 커집니다.
- vacuum/autovacuum 압력이 커집니다.
- claim query가 event payload를 같이 반환하면 네트워크 응답도 payload를 3,000번 반복합니다.
- payload schema 변경 또는 검증이 delivery row 전체에 흩어집니다.

따라서 final production 구조는 event payload와 room delivery state를 분리해야 합니다.

### 2.2 `Publish()` 중심 설계는 DB round-trip을 늘립니다

기존 publisher가 알림 1건마다 PG insert를 수행하면, publisher path가 알림 수에 정비례해서 느려집니다. 알림 worker가 한 tick에서 많은 room delivery를 만들 수 있다면, `Publish()`를 기본 API로 두는 것은 부하를 키우는 설계입니다.

V3에서는 `PublishBatch()`가 기본 API입니다. 기존 `Publish()`는 compatibility wrapper로만 남깁니다.

### 2.3 Valkey Pub/Sub wakeup은 O(1) 정책과 맞지 않습니다

V2/초안에서 언급된 `alarm:dispatch:wakeup` Pub/Sub 방식은 구현이 편하지만, `PUBLISH`는 subscriber 수와 pattern subscriber 수에 비례합니다. dispatcher replica 수가 작다면 실제 문제는 작을 수 있으나, 사용자님의 추가 제약인 “고복잡도 명령 엄금, O(1) 계열 위주”와는 맞지 않습니다.

V3는 Pub/Sub를 기본 wakeup으로 쓰지 않습니다. 대신 단일 fixed list key에 payload 없는 token을 넣고, dispatcher가 단일 key `BRPOP`으로 기다리는 구조를 기본으로 합니다.

### 2.4 V2의 retention/runbook에는 unbounded SQL이 남아 있었습니다

V2 runbook에는 다음과 같은 쿼리 형태가 남아 있었습니다.

```sql
DELETE FROM alarm_dispatch_outbox
WHERE status = 'sent'
  AND sent_at < now() - interval '90 days';
```

큰 테이블에서 이런 쿼리를 운영 job으로 반복 실행하면 lock, WAL, vacuum, replication lag 문제가 생길 수 있습니다. V3에서는 모든 recurring cleanup, reconciliation, retention 작업을 bounded CTE + `LIMIT` chunk 방식으로 작성합니다.

### 2.5 Iris idempotency key 설계가 group send와 충돌할 수 있습니다

“`Idempotency-Key: alarm-delivery-{delivery_id}`를 쓰자”는 방향은 per-delivery send에서는 맞습니다. 하지만 dispatcher가 같은 room의 여러 delivery를 하나의 Iris request로 묶는다면, request 하나가 여러 delivery id를 포함합니다. 이때 delivery 단위 idempotency key 하나만으로는 정확하지 않습니다.

V3에서는 다음처럼 분리합니다.

- per-delivery send를 선택하면 `alarm-delivery-{delivery_id}`가 적합합니다.
- group send를 유지하려면 추후 `alarm_dispatch_send_attempts` 같은 send attempt ledger가 필요합니다.
- Iris idempotency 도입 전까지는 ambiguous `sending`을 quarantine합니다.

### 2.6 `events + deliveries`만으로는 부족합니다. payload가 room-agnostic이어야 합니다

2테이블로 나눠도 `alarm_dispatch_events.payload` 안에 `room_id`, room name, room-specific claim key가 들어가면 중복 저장 문제가 다시 생깁니다.

V3의 핵심 조건은 다음입니다.

```text
alarm_dispatch_events.payload = event-level, room-agnostic JSON
alarm_dispatch_deliveries     = room_id, dedupe_key, claim_keys, delivery status
```

room별 render data가 꼭 필요하면 full payload가 아니라 작은 `delivery_context`만 delivery row에 둡니다. 가능하면 room name 등은 dispatch 시점에 cache/PG에서 조회합니다.

## 3. V3에서 변경된 최종 판단

V2의 단일 `alarm_dispatch_outbox`는 “빠른 안전 전환용 최소안”으로만 남깁니다. 본격 production 기본안은 다음입니다.

```text
alarm_dispatch_events      : logical event payload ledger
alarm_dispatch_deliveries  : room별 delivery 상태 ledger
Valkey                     : O(1) wakeup/cache/index helper
Publisher                  : PublishBatch() first
Dispatcher                 : Valkey wakeup + PG fallback scan + batch claim/update
Iris ambiguous send        : idempotency 전까지 quarantine
```

## 4. 이 문서를 LLM에게 넘길 때 강조할 점

LLM이 흔히 하는 실수는 다음입니다.

- 기존 단일 outbox를 그대로 두고 이름만 바꾸는 실수
- event payload에 room_id를 넣는 실수
- `Publish()` 반복 호출로 batch를 구현하는 실수
- claim SQL에서 event payload까지 join해서 room 수만큼 payload를 반환하는 실수
- `PUBLISH`를 wakeup 기본 구현으로 쓰는 실수
- stale `sending`을 retry로 되돌리는 실수
- cleanup SQL을 unbounded로 작성하는 실수

작업 LLM에게는 “기존 V2를 참고하되 그대로 구현하지 말라”고 명시해야 합니다.
