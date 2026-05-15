# Phase 02. Publisher `PublishBatch()` 전환

## 목표

publisher의 기본 API를 `PublishBatch()`로 바꿉니다. 기존 `Publish()`는 compatibility wrapper로만 남깁니다.

이 phase의 핵심은 “notification N개 = DB round-trip N개”가 되지 않도록 하는 것입니다.

## API 권장안

```go
type AlarmPublisher interface {
    PublishBatch(ctx context.Context, notifications []AlarmNotification) (PublishBatchResult, error)
    Publish(ctx context.Context, notification AlarmNotification) error
}
```

`Publish()` 구현은 단순 wrapper입니다.

```go
func (p *Publisher) Publish(ctx context.Context, n AlarmNotification) error {
    result, err := p.PublishBatch(ctx, []AlarmNotification{n})
    if err != nil {
        return err
    }
    return result.SingleErrorOrNil()
}
```

주의: `PublishBatch()` 내부에서 `Publish()`를 반복 호출하면 안 됩니다.

## publish mode

```text
valkey_only : 기존 Valkey legacy publish만 수행
shadow      : Valkey legacy publish 성공 후 PG deliveries(status=shadowed) 기록
pg_first    : PG deliveries(status=pending) commit 후 Valkey wakeup만 best-effort 수행
```

## mode별 순서

### valkey_only

```text
1. legacy envelope 생성
2. Valkey legacy queue LPUSH
3. return
```

기존 동작을 유지합니다.

### shadow

shadow mode에서는 Valkey가 여전히 primary입니다.

```text
1. legacy envelope 생성
2. Valkey legacy queue LPUSH
3. LPUSH 성공 후 PG InsertBatch(status=shadowed)
4. PG 실패는 ALARM_DISPATCH_SHADOW_FATAL에 따라 fatal/non-fatal
```

중요: shadow mode에서 PG를 먼저 쓰면 안 됩니다. PG에 shadow row가 있는데 legacy path로 발송되지 않은 애매한 상태가 생깁니다.

또한 shadow row는 절대 pending이 아닙니다. PG consumer 대상이 아닙니다.

### pg_first

```text
1. notification validate
2. event/delivery batch 생성
3. PG InsertBatch(status=pending)
4. commit 성공 후 Valkey wakeup token 전송
5. legacy active queue에는 LPUSH하지 않음
```

wakeup 실패는 fatal이 아닙니다. fallback scan이 있으므로 wakeup 실패는 지연만 만듭니다.

## Event grouping

`PublishBatch()`는 입력 notifications를 event 단위로 묶어야 합니다.

예시:

```text
notification A: event live:ch1:stream9:t1:10min, room r1
notification B: event live:ch1:stream9:t1:10min, room r2
notification C: event live:ch1:stream9:t1:10min, room r3
```

저장 결과:

```text
alarm_dispatch_events      1 row
alarm_dispatch_deliveries  3 rows
```

이렇게 되려면 event payload 안에는 room 정보가 없어야 합니다.

## Canonical payload와 hash

동일한 logical event는 동일한 canonical JSON을 만들어야 합니다. Go의 map iteration 순서에 의존하면 안 됩니다.

권장:

1. payload struct를 명시적 필드 순서로 정의
2. JSON 직렬화 전 room-specific 필드 검증
3. sha256 hex string으로 payload_hash 계산

검증:

```go
if payload.RoomID != "" { return error }
if payload contains "room_id" or "roomId" { return error }
```

## delivery 생성

각 room delivery는 다음 값을 가져야 합니다.

```text
room_id
dedupe_key
claim_keys
status
next_attempt_at
```

`dedupe_key` 예시:

```text
room:{room_id}:event:{event_key}
```

legacy claim key가 필요하면 `claim_keys`에 저장합니다. 단, claim key는 dedupe의 보조 정보일 뿐 최종 dedupe는 `dedupe_key UNIQUE`입니다.

## Batch limit

publisher는 다음 상한을 지켜야 합니다.

```text
ALARM_DISPATCH_MAX_EVENTS_PER_BATCH=100
ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH=1000
```

입력이 이보다 크면 chunk로 나눕니다. 각 chunk는 별도 transaction으로 처리합니다.

chunk 사이에서 일부 성공/일부 실패가 가능하므로 result에는 다음 정보가 필요합니다.

```text
requested_events
inserted_events
duplicate_events
hash_conflict_events
requested_deliveries
inserted_deliveries
duplicate_deliveries
failed_chunks
```

## wakeup 호출 위치

Valkey wakeup은 PG commit 이후에만 호출합니다.

잘못된 순서:

```text
wakeup -> PG commit
```

이 순서에서는 dispatcher가 깨어났는데 row가 아직 보이지 않는 race가 생깁니다.

올바른 순서:

```text
PG commit -> wakeup best effort
```

## metrics

필수 metric:

```text
alarm_dispatch_publish_batch_total{mode}
alarm_dispatch_publish_notifications_total{mode}
alarm_dispatch_publish_events_inserted_total{mode}
alarm_dispatch_publish_deliveries_inserted_total{mode}
alarm_dispatch_publish_duplicate_deliveries_total{mode}
alarm_dispatch_publish_event_hash_conflict_total{mode}
alarm_dispatch_shadow_pg_error_total
alarm_dispatch_wakeup_error_total
alarm_dispatch_wakeup_suppressed_total
```

주의: `event_key`, `room_id`, `dedupe_key`를 metric label로 넣지 않습니다. high-cardinality label입니다.

## 테스트

필수 test:

1. `Publish()`가 `PublishBatch()` size 1 wrapper인지 검증
2. `PublishBatch()`가 notification N개에 대해 repository `InsertBatch`를 chunk 수만큼만 호출
3. 같은 event 여러 room이 event 1개로 group되는지 검증
4. payload에 room_id가 있으면 reject
5. shadow mode 순서가 Valkey success 후 PG shadow insert인지 검증
6. shadow mode에서 Valkey 실패 시 PG insert를 하지 않는지 검증
7. shadow mode에서 PG 실패가 flag에 따라 fatal/non-fatal인지 검증
8. pg_first mode에서 legacy active queue LPUSH가 호출되지 않는지 검증
9. pg_first mode에서 PG commit 후 wakeup이 호출되는지 검증
10. wakeup 실패가 publish error로 전파되지 않는지 검증

## 완료 기준

- 기존 호출자는 `Publish()`로 계속 동작
- 신규 batch path 테스트 통과
- pg_first mode가 legacy queue에 payload를 넣지 않음
- shadow row는 `shadowed`로만 생성
- event payload 중복 저장이 제거됨

## no-go 조건

- `PublishBatch()`가 내부에서 `Publish()`를 반복 호출함
- `Publish()`당 PG insert를 수행함
- shadow row를 `pending`으로 저장함
- pg_first에서 legacy active queue에도 LPUSH함
- wakeup 실패를 publish 실패로 처리함
- event payload에 room_id가 들어감

## LLM 작업 프롬프트

```text
publisher를 PublishBatch() 중심으로 리팩토링하세요.
기존 Publish()는 PublishBatch()에 1건을 넘기는 wrapper로 남깁니다.
PublishBatch() 내부에서 Publish()를 반복 호출하지 마세요.

mode 정책:
- valkey_only: 기존 Valkey legacy queue publish만 수행
- shadow: Valkey publish 성공 후 PG InsertBatch(status=shadowed)
- pg_first: PG InsertBatch(status=pending) commit 후 Valkey wakeup만 best-effort, legacy active queue LPUSH 금지

event payload는 room-agnostic이어야 합니다.
같은 event가 여러 room으로 fan-out되면 event row는 1개, delivery row는 room 수만큼이어야 합니다.
metric에는 event_key/room_id/dedupe_key를 label로 넣지 마세요.
테스트에서 호출 순서와 실패 정책을 검증하세요.
```
