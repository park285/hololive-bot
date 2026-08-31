# YouTube egress lifecycle contract

작성일: 2026-08-31 KST  
적용 결정: `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership`  
적용 런타임: `hololive-alarm-worker`  
상위 아키텍처: [`youtube-egress-lifecycle-transition-ownership-20260831.md`](youtube-egress-lifecycle-transition-ownership-20260831.md)  
Commit 판정 부록: [`youtube-egress-lifecycle-commit-adjudication-20260831.md`](youtube-egress-lifecycle-commit-adjudication-20260831.md)  
구현 선택 근거: [`youtube-egress-lifecycle-library-review-20260831.md`](youtube-egress-lifecycle-library-review-20260831.md)

## 규범 용어

이 문서의 `해야 한다`, `금지한다`, `오직`은 구현과 테스트가 지켜야 하는 규범입니다. `권장한다`는 기본 선택이지만 동등한 안전 속성을 증명하면 대체할 수 있습니다.

## 범위

이 계약은 다음 데이터와 실행 경로를 다룹니다.

- `youtube_notification_outbox`의 pre-fanout intent 처리
- `youtube_notification_delivery`의 claim, preparation, send, retry, terminal 처리
- Community/Shorts alarm-once claim과 per-room 중복 방지
- stale `SENDING` quarantine
- `FAILED` revive
- child delivery 기반 outbox aggregate projection

다음 항목은 이 계약에 포함하지 않습니다.

- provider별 HTTP/SDK payload 계약
- alarm-worker replica>1 승인
- `QUARANTINED` 수동 replay UI 또는 운영 API
- event sourcing 또는 별도 workflow engine

## 모델과 식별자

### Physical delivery row

`youtube_notification_delivery.id`로 식별되는 하나의 DB row입니다. Claim과 fencing의 물리적 단위입니다.

### Logical delivery

사용자 관점에서 “같은 내용을 같은 방에 한 번 전달한다”는 단위입니다.

```text
Community/Shorts:
(kind, canonical_post_id, room_id)

그 밖의 YouTube kind:
(outbox_id, room_id)
```

Community/Shorts의 `canonical_post_id`는 telemetry, claim, sibling 조회에서 **같은 resolver**를 사용해야 합니다. `content_id`와 payload 해석을 각 package가 따로 구현해서는 안 됩니다.

서로 다른 outbox/content ID가 같은 post를 나타낼 수 있으므로 `(outbox_id, room_id)` unique key만으로 Community/Shorts의 논리적 중복을 막았다고 간주하지 않습니다.

### Provider operation

외부 provider 요청 한 번에 포함되는 정확한 logical delivery 집합입니다. 개별 메시지는 logical delivery 한 건, grouped 메시지는 여러 건을 가질 수 있습니다. `BeginSending` 이후 member와 request payload는 변경할 수 없습니다.

### Execution phase

```text
claim       PENDING row의 preparation lease 획득
preparation provider 호출 전 local 작업
sending     exact provider operation을 durable하게 확정한 뒤의 phase
finalize    provider outcome을 DB에 반영하는 phase
```

Claim은 lifecycle status transition이 아니라 concurrency protocol입니다.

## 상태와 저장 필드

### Outbox 상태

```text
PENDING
SENT
FAILED
```

| phase | 상태 의미 | writer |
|---|---|---|
| pre-fanout | subscriber lookup과 delivery materialization을 기다리는 intent | `OutboxFanoutService` |
| post-fanout | child delivery 집계 결과 | aggregate projector |

### Delivery 상태

```text
PENDING
SENDING
SENT
FAILED
QUARANTINED
```

| 상태 | 규범 의미 |
|---|---|
| `PENDING` | Provider 호출 전이거나 known-not-delivered retry를 기다립니다. `locked_at`이 있으면 preparation lease를 보유 중입니다. |
| `SENDING` | Exact provider operation이 DB에 commit됐고 provider 호출이 시작될 수 있습니다. 실제 provider 수신을 증명하지는 않습니다. |
| `SENT` | Provider 성공 또는 durable sibling evidence로 logical delivery가 이미 충족됐음을 local transaction으로 확정했습니다. |
| `FAILED` | Retry 소진 또는 permanent known failure입니다. Guarded revive만 허용합니다. |
| `QUARANTINED` | 현재 row 또는 동일 logical delivery sibling의 provider 처리 여부를 증명할 수 없습니다. 자동 retry와 revive를 금지합니다. |

### `sent_at`

`sent_at`은 physical delivery row가 `SENT`로 local commit된 시각입니다.

- Provider 성공 응답 후 `CompleteSent`가 commit된 시각
- 같은 logical delivery의 `SENT` sibling을 확인해 `AlreadySatisfied`가 commit된 시각

따라서 항상 provider response timestamp라는 뜻은 아닙니다. Transport latency는 별도 telemetry evidence를 사용합니다.

### `attempt_count`

`attempt_count`는 **현재 physical row의 처리 시도가 실패로 종료되거나 결과 불명으로 격리된 횟수**입니다.

증가하지 않는 경우:

- claim
- claim defer
- in-batch duplicate follower defer
- equivalent `SENDING` sibling defer
- equivalent `QUARANTINED` sibling에 따른 quarantine propagation
- `BeginSend`
- provider 성공
- `AlreadySatisfied`
- grouped fallback 자체

1 증가하는 경우:

- retryable/permanent preparation failure
- known-not-delivered retryable/permanent send failure
- 현재 row의 stale `SENDING -> QUARANTINED`

Revive는 `attempt_count=0`으로 재설정합니다.

### `row_version`

```sql
row_version bigint NOT NULL DEFAULT 0 CHECK (row_version >= 0)
```

다음 mutation은 version을 1 증가시킵니다.

- `PENDING` claim
- claim/follower/sibling defer
- preparation failure
- `BeginSend`
- send success/failure
- direct 또는 propagated quarantine
- revive

`row_version`은 인덱싱하지 않습니다.

### 시간 정규화

Application service는 operation 또는 batch 시작 시각을 한 번 읽고 repository 경계에서 UTC microsecond precision으로 정규화합니다. 하나의 atomic operation 안에서 서로 다른 `time.Now()` 값을 혼용하지 않습니다.

## 핵심 불변식

### I-001. PostgreSQL source of truth

현재 상태, attempt, due 시각, lock 시각, version은 PostgreSQL 값이 정본입니다. Process memory의 state object는 정본이 될 수 없습니다.

### I-002. single policy evaluation

Retry 소진, preparation/send failure 종류, revive 허용 여부는 lifecycle policy에서 한 번만 계산합니다. SQL `CASE`나 repository가 `MaxRetries`를 다시 평가해서는 안 됩니다.

### I-003. fenced mutation

Claim 이후 모든 delivery mutation은 `id + expected status + expected row_version + expected attempt_count + expected locked_at`을 검사해야 합니다.

### I-004. external-effect ordering

Provider 호출은 operation-level `PENDING -> SENDING` commit이 확인된 뒤에만 시작할 수 있습니다.

### I-005. logical-delivery coalescing

한 process batch 안에서 같은 logical delivery key를 가진 row는 하나의 send leader만 가질 수 있습니다. 나머지 follower는 attempt를 소비하지 않고 defer합니다. Post-level decision cache 결과가 `Proceed`여도 room-level resolution과 in-batch coalescing을 생략할 수 없습니다.

### I-006. durable sibling gate

Community/Shorts send leader는 같은 logical delivery의 durable sibling 상태를 확인해야 합니다.

- `SENT` sibling: `AlreadySatisfied`
- `SENDING` sibling: attempt 없는 defer
- `QUARANTINED` sibling: 현재 row도 provider 호출 없이 `QUARANTINED`

이 규칙은 alarm claim timeout 이후에도 unresolved logical delivery가 다시 전송되는 것을 막습니다.

### I-007. outcome unknown hold

Provider 처리 여부가 불명확하면 즉시 DB mutation을 하지 않습니다. 현재 operation member는 `SENDING`에 남고 stale-sending sweeper만 `QUARANTINED`로 이동시킬 수 있습니다.

### I-008. terminal states

`SENT`는 자동 경로의 절대 terminal입니다. `QUARANTINED`도 audited reconciliation이 도입되기 전까지 자동 terminal입니다.

### I-009. tracking follows committed success

Community/Shorts tracking mutation은 실제 `SENDING -> SENT` CAS와 같은 transaction에서, exact alarm claim token을 만족하는 경우에만 실행합니다. Conflict member의 tracking을 변경해서는 안 됩니다.

### I-010. aggregate writer ownership

Child delivery가 존재하는 outbox의 status, aggregate error, aggregate `sent_at`은 aggregate projector만 계산합니다.

### I-011. operation atomicity

한 provider operation의 `BeginSending`, grouped success finalization, grouped known-failure finalization은 member 전체에 all-or-none입니다. Mixed pre/post state는 partial success가 아니라 atomicity breach입니다.

### I-012. commit ambiguity is first-class

DB `COMMIT` 응답 오류를 rollback 증거로 간주하지 않습니다. Primary exact read-back으로 `Applied`, `Conflict`, `Missing`, `Indeterminate`를 판정하며 callback이나 provider send를 자동 재실행하지 않습니다.

### I-013. no dual write

같은 row를 legacy writer와 새 transition writer가 동시에 변경하는 기간을 만들지 않습니다. Shadow mode는 decision 비교만 허용합니다.

### I-014. no silent liveness over safety

Provider outcome이 불명확하거나 commit 판정이 `Indeterminate`면 자동 복구보다 중복 방지와 stale-write 방지를 우선합니다.

## token 계약

### Preparation lease

```go
type PreparationLease struct {
    DeliveryID int64
    RowVersion int64
    LockedAt   time.Time
}
```

`PENDING + locked_at` row만 나타냅니다.

### Send fence

```go
type SendFence struct {
    DeliveryID int64
    RowVersion int64
    LockedAt   time.Time
}
```

`SENDING + locked_at` row만 나타냅니다. Preparation lease와 별도 Go type이어야 합니다.

### Alarm claim token

```go
type AlarmClaimToken struct {
    Kind         OutboxKind
    PostID       string
    AuthorizedAt time.Time
}
```

- Preparation/known-not-delivered failure는 durable failure commit 뒤 새 claim을 release합니다.
- Success는 exact `authorized_at`을 확인하고 delivery와 tracking을 같은 transaction에서 commit합니다.
- Outcome unknown과 `Indeterminate`에서는 claim을 추정 release하지 않습니다.
- Durable logical sibling gate가 claim timeout 뒤 same-room resend를 차단해야 합니다.

## claim protocol

대상 조건:

```text
status = PENDING
next_attempt_at <= now
locked_at IS NULL OR locked_at < now - LockTimeout
```

Claim SQL은 `FOR UPDATE SKIP LOCKED`로 row를 고르고 다음을 수행합니다.

```sql
SET locked_at = $now,
    row_version = row_version + 1
```

반환된 새 version과 `locked_at`만 유효한 Preparation lease입니다. Statement 결과를 받지 못하면 caller는 lease를 소유하지 않으며 ID만 추정해 preparation을 시작할 수 없습니다.

## logical delivery resolution

### Batch coalescing

Claim된 row를 logical delivery key로 묶습니다. Leader는 `(created_at, delivery_id)` 오름차순의 첫 row입니다. 같은 key의 follower는 provider request에 넣지 않고 `PENDING` unleased로 defer합니다.

서로 다른 room은 다른 logical delivery이므로 같은 post-level claim 결과를 재사용할 수 있습니다. 단, 재사용된 `Proceed` 결과는 room-level sibling 조회를 건너뛰는 권한이 아닙니다.

### Durable sibling resolution

현재 physical row와 현재 batch group을 제외한 같은 logical delivery sibling을 조회합니다. 상태 우선순위는 다음과 같습니다.

```text
SENT
  -> AlreadySatisfied

SENDING
  -> EquivalentDeliveryInFlight
  -> PENDING unleased, attempt 유지, due 이동

QUARANTINED
  -> EquivalentDeliveryUnresolved
  -> PENDING leased -> QUARANTINED, attempt 유지

FAILED/PENDING/없음
  -> post-level alarm claim과 normal preparation 계속
```

`SENT`와 `QUARANTINED`가 함께 있으면 `SENT`가 우선합니다. Logical delivery가 확정 충족됐기 때문입니다.

Sibling query는 Community/Shorts canonical post resolver를 사용하며 raw `content_id` equality만 사용해서는 안 됩니다.

### Cross-process race

현재 alarm-worker는 replica=1입니다. Batch coalescing과 post-level DB claim, delivery CAS가 current concurrency boundary입니다. Replica>1에서는 logical key advisory lock 또는 persisted group-affinity가 추가로 필요하며 이 계약만으로 scale-out을 승인하지 않습니다.

## preparation 결과

Provider 호출 전 결과는 다음 중 하나여야 합니다.

### `ReadyToSend`

Payload load, validation, rendering, route, deterministic client request ID, alarm claim, immutable request가 모두 확정됐습니다.

### `AlreadySatisfied`

Durable `SENT` sibling이 있습니다.

```text
PENDING leased -> SENT
attempt 유지
provider 호출 없음
새 tracking mutation 없음
```

### `ClaimDeferred`

다른 execution이 post-level claim을 보유합니다.

```text
PENDING leased -> PENDING unleased
attempt 유지
next_attempt_at = now + RetryBackoff
```

### `DuplicateFollowerDeferred`

같은 batch에 더 낮은 `(created_at,id)` leader가 있습니다. `ClaimDeferred`와 동일한 저장 형태를 사용하되 rule ID를 구분합니다.

### `EquivalentDeliveryInFlight`

같은 logical delivery의 durable `SENDING` sibling이 있습니다. Attempt 없이 defer합니다.

### `EquivalentDeliveryUnresolved`

같은 logical delivery의 durable `QUARANTINED` sibling이 있습니다.

```text
PENDING leased -> QUARANTINED
attempt 유지
provider 호출 없음
```

### `PreparationRetryableFailure`

```text
nextAttempt = attempt_count + 1
nextAttempt < MaxRetries  -> PENDING unleased, due 설정
nextAttempt >= MaxRetries -> FAILED
```

### `PreparationPermanentFailure`

```text
PENDING leased -> FAILED
attempt_count + 1
```

## send operation

```go
type PreparedSendOperation struct {
    OperationID     string
    ClientRequestID string
    LogicalKeys     []LogicalDeliveryKey
    DeliveryLeases  []PreparationLease
    AlarmClaims     []AlarmClaimToken
    Request         ImmutableSendRequest
}
```

`OperationID`는 process-local trace identity이고 `ClientRequestID`는 provider idempotency identity입니다.

### `BeginSending`

- 모든 member가 expected `PENDING + PreparationLease`를 만족하면 전체를 `SENDING`으로 변경합니다.
- 한 member라도 conflict/missing이면 transaction 전체를 rollback하고 provider를 호출하지 않습니다.
- `locked_at`을 canonical `sendStartedAt`으로 갱신하고 version을 증가시킵니다.
- 성공하면 member별 Send fence를 반환합니다.

```sql
SET status = 'SENDING',
    locked_at = $send_started_at,
    row_version = row_version + 1
WHERE id = $id
  AND status = 'PENDING'
  AND row_version = $expected_version
  AND attempt_count = $expected_attempt
  AND locked_at = $expected_locked_at
```

Commit 응답 오류는 provider 호출 전에 primary exact read-back으로 판정합니다. Exact post-state만 provider 호출 권한을 만듭니다.

### Send lease budget

```text
SendingFinalizeGrace = max(2 * PollInterval, 5 seconds)
LockTimeout >= DeliverySendTimeout + SendingFinalizeGrace
```

Provider call 시작 전:

```text
send fence expiry - now >= DeliverySendTimeout + SendingFinalizeGrace
```

부족하면 provider를 호출하지 않고 known-not-delivered retryable failure로 처리합니다.

## provider outcome

```go
type ProviderOutcomeKind uint8

const (
    ProviderDelivered ProviderOutcomeKind = iota + 1
    ProviderKnownNotDeliveredRetryable
    ProviderKnownNotDeliveredPermanent
    ProviderOutcomeUnknown
)
```

Stable client request ID의 존재만으로 retry-safe가 되지는 않습니다. Provider가 동일 ID replay를 deduplicate하거나 요청 미수락을 증명하는 계약이 있어야 합니다.

Raw error message는 진단용이며 policy 분기나 metric label에 사용하지 않습니다.

## grouped send와 fallback

1. Grouped request member는 모두 서로 다른 logical delivery key여야 합니다.
2. `BeginSending`은 grouped member 전체에 all-or-none입니다.
3. Grouped success는 member 전체를 한 success transaction으로 finalize합니다.
4. Outcome unknown이면 member 전체를 `SENDING`에 유지하고 fallback하지 않습니다.
5. Fallback은 provider가 grouped request를 수락하지 않았다고 확정하고 `fallback_allowed=true`인 경우만 허용합니다.
6. Fallback 자체는 attempt를 소비하지 않습니다.
7. 각 individual fallback 전에 remaining send lease budget을 확인합니다.
8. Budget이 부족해 시작하지 않은 call은 known-not-delivered retryable입니다.

## delivery transition matrix

| source/phase | event | next | attempt | provider call |
|---|---|---|---:|---:|
| `PENDING` unclaimed | claim | `PENDING` leased | 유지 | 아니오 |
| `PENDING` leased | `AlreadySatisfied` | `SENT` | 유지 | 아니오 |
| `PENDING` leased | claim/follower/in-flight defer | `PENDING` unleased | 유지 | 아니오 |
| `PENDING` leased | equivalent unresolved sibling | `QUARANTINED` | 유지 | 아니오 |
| `PENDING` leased | prep retryable, retry 잔여 | `PENDING` | +1 | 아니오 |
| `PENDING` leased | prep retryable, retry 소진 | `FAILED` | +1 | 아니오 |
| `PENDING` leased | prep permanent | `FAILED` | +1 | 아니오 |
| `PENDING` leased | `BeginSend` | `SENDING` | 유지 | commit 확인 뒤 |
| `SENDING` | delivered | `SENT` | 유지 | 이미 1회 |
| `SENDING` | known retryable, retry 잔여 | `PENDING` | +1 | 이미 1회 |
| `SENDING` | known retryable, retry 소진 | `FAILED` | +1 | 이미 1회 |
| `SENDING` | known permanent | `FAILED` | +1 | 이미 1회 |
| `SENDING` | outcome unknown | 상태 변경 없음 | 유지 | 이미 1회 |
| stale `SENDING` | lease expired | `QUARANTINED` | +1 | 추가 호출 없음 |
| `FAILED` | eligible revive | `PENDING` | 0 reset | 아니오 |

## retry 계산

```text
nextAttemptCount = currentAttemptCount + 1
retryDelay = max(config.RetryBackoff, providerRetryAfter)
nextAttemptAt = eventAt + retryDelay
```

`nextAttemptCount >= MaxRetries`면 `FAILED`입니다. Exponential backoff나 jitter는 별도 변경입니다.

## decision model

Policy는 nullable patch map이 아니라 concrete sealed decision을 반환합니다.

```go
type DecisionContext struct {
    RuleID               RuleID
    DeliveryID           int64
    ExpectedStatus       DeliveryStatus
    ExpectedVersion      int64
    ExpectedAttemptCount int
    ExpectedLockedAt     time.Time
    At                   time.Time
}

type DeliveryDecision interface {
    Context() DecisionContext
    deliveryDecision()
}
```

최소 concrete decision:

```text
BeginSendDecision
SentDecision
AlreadySatisfiedDecision
DeferDecision
RetryDecision
FailDecision
QuarantinePropagationDecision
```

Invalid field combination을 public struct literal로 만들 수 없어야 하며 `RuleID`가 metric/audit까지 전달되어야 합니다.

## transition store API

```go
type TransitionStore interface {
    ClaimPending(context.Context, ClaimRequest) ([]ClaimedDelivery, error)
    BeginSending(context.Context, PreparedSendOperation) (StartedSendOperation, ApplyResult, error)
    CompleteAlreadySatisfied(context.Context, AlreadySatisfiedCommand) (ApplyResult, error)
    DeferClaim(context.Context, DeferCommand) (ApplyResult, error)
    PropagateQuarantine(context.Context, QuarantinePropagationCommand) (ApplyResult, error)
    ScheduleRetryBatch(context.Context, []RetryCommand) ([]ApplyResult, error)
    FailBatch(context.Context, []FailCommand) ([]ApplyResult, error)
    CompleteSent(context.Context, SentOperation) (ApplyResult, error)
    QuarantineStaleSending(context.Context, time.Time, int) ([]int64, int, error)
    ReviveFailedOutboxes(context.Context, ReviveRequest) (int64, error)
}
```

금지 API:

```text
UpdateStatus(id, status)
ApplyPatch(map[string]any)
expected state/token을 생략하는 writer
MaxRetries를 받는 repository method
raw failure string으로 branch하는 writer
ID-only SENDING -> SENT recovery
```

## apply outcome과 commit 판정

```go
type ApplyOutcome uint8

const (
    ApplyApplied ApplyOutcome = iota + 1
    ApplyConflict
    ApplyMissing
    ApplyIndeterminate
)
```

- `Applied`: commit 확인 또는 primary exact post-state 확인
- `Conflict`: row는 있으나 expected state/version/attempt/lock과 불일치
- `Missing`: row 없음
- `Indeterminate`: exact pre/post/coherent conflict 어느 쪽도 판정 불가

Operation별 read-back과 retry 규칙은 commit 판정 부록을 따릅니다. `Indeterminate`를 retryable transition refusal로 축약하지 않습니다.

## success transaction

`CompleteSent`는 다음을 한 transaction에서 수행합니다.

1. Operation member 전체의 exact `SENDING + SendFence` 확인
2. Member 전체 `SENT`, canonical `sent_at`, lock clear, version 증가
3. Exact `AlarmClaimToken.authorized_at` 확인
4. 적용된 member에 필요한 tracking mark와 alarm-state 완료 기록
5. Latency classification 기록
6. Commit

한 member 또는 alarm claim token이라도 conflict면 전체 rollback합니다. Provider는 다시 호출하지 않습니다.

Commit 오류 후:

- Exact post-state와 tracking 확인: `Applied`
- Exact pre-state 확인: 같은 DB finalization만 retry
- Tracking mismatch/mixed member: atomicity breach + `Indeterminate`
- 어떤 경우에도 provider 재호출과 ID-only recovery 금지

## failure finalization

Known-not-delivered outcome만 retry/FAILED writer를 호출할 수 있습니다.

- Durable failure commit이 확인된 뒤 새 alarm claim을 release합니다.
- `Indeterminate`에서 claim release 여부를 추정하지 않습니다.
- Provider를 다시 호출하지 않습니다.
- Message는 sanitize하고 raw error를 metric label로 사용하지 않습니다.

## outcome unknown

허용 작업:

- warning audit log
- bounded metric
- masked operation/delivery evidence

금지 작업:

- retry scheduling
- `FAILED` 또는 즉시 `QUARANTINED` write
- claim release
- grouped fallback
- external resend

## quarantine

### Current row stale quarantine

```text
status = SENDING
locked_at < now - LockTimeout
```

```text
SENDING -> QUARANTINED
attempt_count + 1
locked_at = NULL
row_version + 1
```

### Logical sibling propagation

Durable `QUARANTINED` sibling을 확인한 current `PENDING` row:

```text
PENDING leased -> QUARANTINED
attempt_count 유지
locked_at = NULL
row_version + 1
```

두 경로는 rule ID와 attempt 의미를 구분합니다. Source predicate가 terminal row를 다시 증가시키지 않게 해야 합니다.

## revive

대상 outbox 조건:

- aggregate `FAILED`
- outbox `sent_at IS NULL`
- freshness window 안
- active outbox lock 없음
- `FAILED` child가 있거나 child가 전혀 없음
- `QUARANTINED`만 존재하지 않음

Child가 있으면 `FAILED`만 reset하고 `SENT`/`QUARANTINED`는 보존합니다. 같은 transaction에서 표준 aggregate projector를 실행합니다. Child가 없을 때만 pre-fanout outbox를 직접 reset합니다.

Commit 결과가 불명확하고 exact non-commit이 확인되지 않으면 stale selected ID set을 반복하지 않고 eligibility selection부터 다시 수행합니다.

## outbox fanout

### `CompleteWithoutTargets`

Transaction 안에서 active outbox claim과 child 부재를 확인한 뒤 `SENT`, canonical `sent_at`, lock clear를 적용합니다.

### `MaterializeFanout`

한 transaction에서:

1. Active outbox claim 확인
2. Target room canonicalize/dedupe
3. `(outbox_id, room_id)` idempotent insert
4. Outbox lock clear
5. Status `PENDING` 유지

Commit response loss는 canonical target 전체 child와 lock clear를 primary read-back합니다. Child 일부만 존재하면 atomicity breach입니다.

### Post-fanout writer

Child가 하나라도 있으면 pre-fanout direct writer는 conflict를 반환하고 aggregate projector가 상태를 소유합니다.

## aggregate projection

```text
PENDING 또는 SENDING child 존재
    -> outbox PENDING

active child 없음 + FAILED 또는 QUARANTINED child 존재
    -> outbox FAILED

active/failure child 없음 + SENT child 존재
    -> outbox SENT

child 없음
    -> outbox PENDING
```

계산과 update는 하나의 SQL statement로 유지합니다. `SENT`의 outbox `sent_at`은 NULL일 때만 설정합니다. Projector는 current child state에서 다시 계산하는 idempotent operation이어야 합니다.

## 금지 전이

```text
SENT -> *
QUARANTINED -> * 자동 전이
FAILED -> SENT
PENDING unclaimed -> SENDING
PENDING -> QUARANTINED without durable equivalent QUARANTINED evidence
SENDING -> SENDING 재claim
```

Manual reconciliation이나 replay는 별도 결정과 immutable audit가 필요합니다.

## DB audit와 constraint

Cutover 전에 다음 query가 0건이어야 합니다.

```sql
SELECT id, status, attempt_count, locked_at, sent_at
FROM youtube_notification_delivery
WHERE status NOT IN ('PENDING', 'SENDING', 'SENT', 'FAILED', 'QUARANTINED')
   OR attempt_count < 0
   OR (status = 'SENDING' AND locked_at IS NULL)
   OR (status = 'SENT' AND sent_at IS NULL)
   OR (status IN ('SENT', 'FAILED', 'QUARANTINED') AND locked_at IS NOT NULL);
```

이미 status vocabulary constraint가 있으므로 중복 생성하지 않습니다. 새 state-shape constraint가 필요하면 `NOT VALID`, audit/repair, `VALIDATE CONSTRAINT` 순서를 따릅니다.

## configuration validation

```text
MaxRetries > 0
RetryBackoff > 0
LockTimeout > 0
DeliverySendTimeout > 0
LockTimeout >= DeliverySendTimeout + max(2*PollInterval, 5s)
ReviveFreshnessWindow > 0 when revive enabled
ClaimFreshnessWindow >= ReviveFreshnessWindow + ReviveInterval
```

Production에서 잘못된 값을 조용히 default로 바꾸지 않고 normalized config를 startup validation합니다.

## observability

### Metric

```text
youtube_delivery_transition_total{rule,from,to,result}
youtube_delivery_transition_conflict_total{rule,phase}
youtube_delivery_logical_coalesce_total{result}
youtube_delivery_sibling_gate_total{status,result}
youtube_delivery_outcome_unknown_total{transport}
youtube_delivery_quarantine_total{reason}
youtube_delivery_finalization_retry_total{result}
youtube_delivery_commit_adjudication_total{operation,result}
youtube_delivery_commit_indeterminate_total{operation,phase}
youtube_delivery_atomicity_breach_total{operation}
youtube_delivery_tracking_mismatch_total{operation}
youtube_outbox_aggregate_transition_total{from,to}
youtube_outbox_aggregate_lag_seconds
```

Raw error와 ID를 label에 넣지 않습니다.

### Structured log

```text
rule_id
event
logical_delivery_key_hash
from_state
to_state
attempt_count
row_version
delivery_id
outbox_id
operation_id
client_request_id_hash
failure_code
apply_outcome
provider_effect_started
provider_effect_confirmed
```

Room ID, request ID, logical key 원문은 mask/hash합니다.

## contract tests

### Policy

- 전체 state/event matrix
- retry 경계
- attempt semantics
- terminal states
- explicit clock purity
- invalid decision construction 방지

### Logical delivery

- 같은 batch의 same post/room row는 provider call 1회 이하
- `Proceed` cache hit도 room-level gate를 다시 수행
- `SENT` sibling은 `AlreadySatisfied`
- `SENDING` sibling은 no-attempt defer
- `QUARANTINED` sibling은 no-attempt quarantine propagation
- `SENT`와 `QUARANTINED` 동시 존재 시 `SENT` 우선
- 다른 room은 독립 logical delivery

### Store/CAS

- stale version/lock/attempt 거부
- operation member 일부 conflict 시 all-or-none rollback
- exact alarm claim token conflict 시 success rollback
- conflict member tracking 미변경
- claim/version 단조 증가

### Commit adjudication

- Begin commit response loss 판정 전 provider 0회
- Begin `Indeterminate` provider 0회
- Success commit response loss에서 delivery/tracking exact 확인
- Exact pre-state일 때 DB-only retry
- Provider 자동 재호출 0회
- Fanout partial child atomicity breach

### Crash windows

- claim 후 crash: lease expiry 후 재claim
- preparation crash: provider 0회
- `SENDING` commit 후 call 전 crash: quarantine
- success 후 commit 전 crash: quarantine 가능, tracking 추정 금지
- aggregate 전 crash: projector 수렴

### Grouped operation

- duplicate logical key member 거부 또는 사전 coalesce
- member CAS 일부 실패 시 provider 0회
- outcome unknown 후 fallback 0회
- fallback budget 부족 row provider 0회
- finalization member conflict 전체 rollback

## 완료 판정

1. Retry와 목적 상태를 policy와 SQL이 중복 계산하지 않습니다.
2. Production status write가 의도별 transition store 한 경로를 통합니다.
3. Same logical delivery batch의 send leader가 하나입니다.
4. Durable `SENDING`/`QUARANTINED` sibling이 same-room resend를 차단합니다.
5. Outcome unknown에서 DB mutation, claim release, fallback, resend가 없습니다.
6. Claim과 모든 mutation이 version을 증가시킵니다.
7. Success tracking이 operation-level CAS transaction 안에 있습니다.
8. Child outbox status는 aggregate projector만 계산합니다.
9. ID-only success recovery와 legacy writer가 제거됩니다.
10. Worker 전용 store/row model이 alarm-worker internal에 있습니다.
11. Effect 인접 transaction이 `Indeterminate`를 표현합니다.
12. Contract, integration, logical-dedupe, crash-window, commit fault-injection test가 통과합니다.
13. Decision record를 `verified`로 올릴 evidence가 저장소에 남습니다.
