# 현재 구현 비판적 판정

## 결론

현재 구현된 `pg_first/pg + wakeup`은 목표 아키텍처와 방향이 맞습니다.

다만 “지금 전체 production을 전환해도 성능 이슈가 없다”라고 보기는 어렵습니다. 현재 코드는 안전한 1차 구현에 가깝고, 고 fan-out 상황에서 다음 병목이 예상됩니다.

## 이미 잘 된 부분

- 스키마가 `alarm_dispatch_events`와 `alarm_dispatch_deliveries`로 분리되어 있습니다.
- event payload는 room-agnostic 형태로 저장하려는 방향입니다.
- Valkey wakeup은 payload 없는 list token입니다.
- publisher는 `PublishBatch()`를 기본 경로로 추가했습니다.
- dispatcher는 PG mode에서 `FOR UPDATE SKIP LOCKED` 기반 claim을 사용합니다.
- PG mode에서는 Iris send failure를 retry가 아니라 quarantine으로 처리합니다.
- `MarkSending()`과 `MarkSent()`는 worker ownership 조건을 사용합니다.

## 현재 위험한 부분

### 1. InsertBatch가 아직 진짜 batch가 아닙니다

repository는 batch input을 받지만 transaction 안에서 event별 insert, delivery별 insert를 반복합니다. 즉 1개 event가 1,000개 room으로 fan-out되면 delivery insert statement가 1,000번 실행될 수 있습니다.

이것이 현재 가장 큰 성능 리스크입니다.

### 2. Valkey가 PG fallback mode의 hard dependency가 될 수 있습니다

PG ledger가 source of truth라면 Valkey wakeup은 latency helper여야 합니다. 그런데 dispatcher startup/readiness가 Valkey 연결에 hard fail하면, Valkey 장애 시 PG fallback scan으로 복구한다는 불변식이 깨질 수 있습니다.

### 3. reconciliation이 매 batch마다 실행됩니다

`DrainBatch()` 시작마다 stale leased 복구와 stale sending quarantine을 수행하면 backlog 처리 중 PG에 불필요한 update/select 압력이 생깁니다. reconciliation은 필요하지만 hot path에서 매번 실행하면 안 됩니다.

### 4. 실패 경로 update가 row-by-row입니다

정상 경로의 `MarkSending`/`MarkSent`는 batch update이지만, `ScheduleRetry`, `MoveToDLQ`, `Quarantine`은 개별 update 반복 성격입니다. Iris 장애나 renderer 장애 때 DB write amplification이 커집니다.

### 5. 동일 batch 안의 event hash conflict를 놓칠 수 있습니다

동일 `event_key`가 같은 batch 안에서 서로 다른 payload hash로 들어오면, 현재 map dedupe 로직이 먼저 본 event만 보존하고 conflict를 못 잡을 수 있습니다. 이 경우 조용한 payload mismatch가 생길 수 있습니다.

### 6. dispatch group error가 sibling group을 취소할 수 있습니다

`errgroup.WithContext`를 쓰면 한 group의 persistence error가 같은 batch의 다른 group context를 취소합니다. 독립 room group 처리에서는 한 group 오류가 다른 group send/mark를 취소하면 안 됩니다.

## 판정

- 소규모 canary: 가능.
- 전체 production 전환: P0~P4 hardening 후 권장.
- 가장 먼저 고쳐야 할 것: set-based `InsertBatch`, Valkey degraded startup/readiness, reconciliation throttle.
