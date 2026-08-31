# YouTube egress lifecycle contract

작성일: 2026-08-31 KST  
적용 결정: `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership`  
적용 런타임: `hololive-alarm-worker`  
상위 아키텍처: [`youtube-egress-lifecycle-transition-ownership-20260831.md`](youtube-egress-lifecycle-transition-ownership-20260831.md)  
Logical ledger: [`youtube-egress-logical-delivery-ledger-20260831.md`](youtube-egress-logical-delivery-ledger-20260831.md)
Commit 판정: [`youtube-egress-lifecycle-commit-adjudication-20260831.md`](youtube-egress-lifecycle-commit-adjudication-20260831.md)  
구현 선택 근거: [`youtube-egress-lifecycle-library-review-20260831.md`](youtube-egress-lifecycle-library-review-20260831.md)

## 규범 용어

`해야 한다`, `금지한다`, `오직`은 구현과 테스트가 지켜야 하는 규범입니다. `권장한다`는 동등한 안전 속성을 입증한 대안이 없을 때의 기본 선택입니다.

## 범위

이 계약은 다음을 다룹니다.

- `youtube_notification_outbox` pre-fanout intent
- `youtube_notification_delivery` claim, preparation, send, retry, terminal 처리
- 모든 kind의 logical delivery 중복 방지와 Community/Shorts canonical identity
- Post-level alarm claim과 per-room delivery success 결합
- Stale `SENDING` quarantine과 logical group reconciliation
- `FAILED` revive
- Child delivery 기반 outbox aggregate projection
- Logical `SENT|QUARANTINED` ledger와 fixed-high-water backfill
- `terminal_at` 기반 full-row cleanup

다음은 비범위입니다.

- Provider별 HTTP/SDK payload 계약
- Alarm-worker replica>1 승인
- Operator replay UI/API
- Event sourcing 또는 외부 workflow engine

## 모델과 식별자

### Physical delivery row

`youtube_notification_delivery.id`로 식별되는 DB row입니다. Claim, version fencing, outbox aggregate의 물리 단위입니다.

### Logical delivery

사용자 관점에서 같은 내용을 같은 방에 한 번 전달하는 단위입니다.

```text
Community/Shorts:
(kind, canonical_post_id, room_id)

그 밖의 YouTube kind:
(kind, outbox.content_id, room_id)
```

Community/Shorts의 `canonical_post_id`는 claim, telemetry, sibling 조회가 같은 resolver를 사용해야 합니다. Payload와 `content_id` 해석을 package별로 복제해서는 안 됩니다.

`(outbox_id, room_id)` unique index는 physical duplicate만 막습니다. Community/Shorts에서는 서로 다른 outbox/content ID가 같은 canonical post를 표현할 수 있고, 모든 kind에서 cleanup 뒤 같은 content의 outbox ID가 달라질 수 있으므로 logical duplicate 방어를 대체하지 않습니다.

Canonical resolver는 Community/Shorts의 alias를 하나의 `canonical_post_id`로 정규화하고, 그 밖의 kind는 trim된 non-empty `content_id`를 사용합니다. Invalid payload/identity를 raw ID로 대체하지 않고 preparation 또는 backfill을 fail closed합니다.

### Logical delivery ledger

`youtube_notification_delivery_ledger`는 `(kind, logical_id, room_id)`별 terminal evidence의 정본입니다.

```text
SENT > QUARANTINED > absent
```

`QUARANTINED -> SENT`만 강화 전이로 허용하고 ledger row 삭제와 `SENT` downgrade를 금지합니다. Full outbox/delivery cleanup 뒤에도 ledger는 남습니다.

### Logical delivery group

Retained physical row 중 logical key가 같은 집합입니다. 그룹 안에는 정확히 하나의 deterministic retry owner가 있습니다.

```text
owner = 최소 (created_at, delivery_id)
```

Ledger `SENT/QUARANTINED`가 retained physical state보다 우선합니다. Ledger가 absent일 때 retained `SENDING`이 owner의 현재 `PENDING/FAILED`보다 우선합니다. Impossible mixed state는 우선순위로 숨기지 않고 invariant breach로 분류합니다.

### Provider operation

외부 provider 요청 한 번에 포함되는 정확한 logical delivery owner 집합입니다. 개별 메시지는 owner 한 건, grouped 메시지는 서로 다른 logical key의 owner 여러 건을 가질 수 있습니다.

Operation membership과 provider request는 `BeginSending` 이후 변경할 수 없습니다.

### Tracking requirement

Community/Shorts의 post-level tracking은 room별 delivery와 수명이 다릅니다. Provider operation은 다음 requirement 중 하나를 가집니다.

```go
type TrackingRequirement interface {
    trackingRequirement()
}

type NoTracking struct{}

type RequireClaimOrAlreadySent struct {
    Token AlarmClaimToken
}

type RequireAlreadySent struct {
    Kind   OutboxKind
    PostID string
}
```

- `RequireClaimOrAlreadySent`: preparation에서 active claim을 획득했습니다. Finalize 시 exact token이 유효하면 tracking을 sent로 전이하고, 이미 sent면 성공으로 수용합니다.
- `RequireAlreadySent`: preparation 시 post tracking이 이미 sent였습니다. Finalize 시 여전히 sent인지 확인하되 tracking을 변경하지 않습니다.
- `NoTracking`: Community/Shorts가 아닌 delivery입니다.

여러 room operation이 같은 post token을 공유할 수 있습니다. 첫 성공이 token을 소비한 뒤 후속 성공은 `already sent`로 수용되어야 하며 delivery success를 rollback해서는 안 됩니다.

## 상태와 저장 필드

### Outbox 상태

```text
PENDING
SENT
FAILED
```

| Phase | 의미 | Writer |
|---|---|---|
| pre-fanout | subscriber lookup과 delivery materialization 대기 | `OutboxFanoutService` |
| post-fanout | child delivery 집계 | aggregate projector |

`terminal_at`은 outbox가 현재 terminal 상태에 들어간 시각입니다. `PENDING`이면 NULL이고 `SENT/FAILED`이면 non-NULL입니다. Idempotent terminal projection은 최초 값을 보존하고, `FAILED -> SENT`는 새 terminal transition 시각으로 갱신하며, revive로 `PENDING`이 되면 NULL로 지웁니다.

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
| `PENDING` | Provider 호출 전이거나 logical owner의 known-not-delivered retry 대기입니다. `locked_at`이 있으면 preparation lease를 보유합니다. |
| `SENDING` | Exact provider operation이 durable하게 commit됐고 provider 호출이 시작될 수 있습니다. Provider 수신을 증명하지는 않습니다. |
| `SENT` | Logical delivery가 충족됐음을 local transaction으로 확정했습니다. Provider success 또는 same-logical `SENT` evidence가 근거입니다. |
| `FAILED` | Logical owner의 retry가 소진됐거나 permanent known failure입니다. Group revive만 허용합니다. |
| `QUARANTINED` | Logical delivery 처리 여부가 불명확합니다. Automatic resend와 일반 revive를 금지합니다. |

### `sent_at`

Physical row가 logical delivery 충족 상태로 local commit된 시각입니다.

- Provider success finalization 시각
- Same-logical `SENT` evidence reconciliation 시각

항상 provider response timestamp라는 뜻은 아닙니다. Transport latency는 telemetry evidence를 사용합니다.

### `attempt_count`

Logical retry owner의 실패/unknown 시도 수입니다. Follower projection은 owner의 retry budget을 소비하지 않습니다.

증가하지 않는 경우:

- claim
- batch duplicate follower defer
- `PENDING` owner follower defer
- equivalent `SENDING` defer
- mirrored `FAILED`/`QUARANTINED` propagation
- `BeginSend`
- provider success
- same-logical sent reconciliation
- grouped fallback 자체

Owner에서 1 증가하는 경우:

- retryable/permanent preparation failure
- known-not-delivered retryable/permanent send failure
- owner stale `SENDING -> QUARANTINED`

Group revive는 owner와 follower attempt를 0으로 재설정합니다.

### `row_version`

```sql
row_version bigint NOT NULL DEFAULT 0 CHECK (row_version >= 0)
```

Claim과 모든 physical row mutation은 version을 1 증가시킵니다. Version은 인덱싱하지 않습니다.

### 시간 정규화

Application service는 operation 또는 batch 시각을 한 번 읽고 repository 경계에서 UTC microsecond precision으로 정규화합니다. 하나의 atomic operation 안에서 서로 다른 `time.Now()` 값을 혼용하지 않습니다.

## 핵심 불변식

### I-001. PostgreSQL source of truth

Current state, attempt, due, lock, version은 PostgreSQL이 정본입니다. Process memory state는 정본이 될 수 없습니다.

### I-002. Single policy evaluation

Retry 소진, preparation/send failure, revive는 lifecycle policy가 한 번만 계산합니다. SQL `CASE`와 repository가 `MaxRetries`를 다시 평가해서는 안 됩니다.

### I-003. Fenced mutation

Claim 이후 mutation은 `id + expected status + expected version + expected attempt + expected locked_at`을 검사해야 합니다.

### I-004. One logical retry owner

같은 logical group에서 provider send와 attempt 증가 권한은 deterministic owner 하나에만 있습니다. Follower가 별도 attempt budget으로 owner를 추월해서는 안 됩니다.

### I-005. In-batch coalescing

같은 batch의 동일 logical key는 owner/leader 하나만 provider candidate가 됩니다. Follower는 attempt 없이 defer하거나 owner state를 mirror합니다.

### I-006. Durable logical gate

Post-level cached `Proceed`를 포함한 모든 candidate는 ledger를 먼저 읽고 retained logical group resolution을 수행해야 합니다.

### I-007. External-effect ordering

Provider call은 operation-level `PENDING -> SENDING` commit이 확인된 뒤에만 시작할 수 있습니다.

### I-008. Outcome unknown hold

Provider 처리 여부가 불명확하면 즉시 state write, claim release, fallback, resend를 하지 않습니다. Operation member는 `SENDING`에 남고 stale sweeper가 group을 quarantine합니다.

### I-009. Monotonic fulfillment reconciliation

Same-logical ledger `SENT` evidence가 생기면 `PENDING`, `FAILED`, `QUARANTINED` follower를 provider 호출 없이 `SENT`로 reconcile할 수 있습니다. 이는 resend가 아니라 logical fulfillment의 단조 수렴입니다.

`SENT -> *`는 존재하지 않습니다. `QUARANTINED -> PENDING/FAILED`와 `FAILED -> PENDING` 일반 전이는 금지합니다.

### I-010. Tracking is idempotent terminal state

Exact claim token이 이미 소비됐더라도 post tracking이 durable sent이면 후속 room delivery success를 수용합니다. Tracking token conflict를 delivery failure로 오판해서는 안 됩니다.

### I-011. Operation atomicity

Provider operation의 `BeginSending`, grouped success, grouped known failure는 owner member 전체에 all-or-none입니다. Mixed pre/post state는 atomicity breach입니다.

### I-012. Aggregate writer ownership

Child가 존재하는 outbox status와 `terminal_at`은 aggregate projector만 계산합니다.

### I-013. Commit ambiguity is first-class

DB commit 오류를 rollback 증거로 간주하지 않습니다. Primary exact read-back으로 `Applied`, `Conflict`, `Missing`, `Indeterminate`를 판정합니다.

### I-014. No dual write

Legacy writer와 새 transition writer가 같은 row를 동시에 변경하는 기간을 만들지 않습니다.

Compatibility/backfill 구간에도 poller가 delivery/outbox lifecycle field를 직접 `PENDING/SENT`로 바꾸지 않습니다. High-water 이후 terminal event는 compatibility alarm-worker가 ledger와 같은 transaction에 기록해야 합니다.

### I-015. Safety before automatic liveness

Outcome unknown, logical unresolved, commit indeterminate에서는 자동 resend와 unsafe overwrite보다 중복 방지와 stale-write 방지를 우선합니다.

### I-016. Terminal evidence atomicity

Provider success의 delivery/tracking과 ledger `SENT`, stale outcome unknown의 delivery group과 ledger `QUARANTINED`는 각각 같은 transaction입니다. Aggregate projection은 이 transaction 밖의 별도 recoverable projection이며 transition commit 판정에 포함하지 않습니다.

## Token 계약

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
- Success finalization은 exact token 또는 durable already-sent state를 요구합니다.
- Outcome unknown과 `Indeterminate`에서는 claim release를 추정하지 않습니다.
- Claim timeout 뒤 same-room resend는 durable logical group gate가 차단합니다.

## Claim protocol

대상 조건:

```text
status = PENDING
next_attempt_at <= now
locked_at IS NULL OR locked_at < now - LockTimeout
```

Claim SQL:

```sql
SET locked_at = $now,
    row_version = row_version + 1
```

반환된 exact version과 `locked_at`만 Preparation lease입니다. Statement 결과를 받지 못하면 caller는 lease를 소유하지 않습니다.

## Logical group resolution

### Candidate loading

Current claimed row의 logical key를 만든 뒤 ledger를 batch load하고, ledger가 absent인 key의 retained physical row를 bounded batch query로 로드합니다.

- Community/Shorts는 kind, room, relevant status를 SQL에서 제한하고 canonical resolver로 post ID를 비교합니다.
- 기타 kind는 outbox join으로 `(kind, outbox.content_id, room_id)`를 조회합니다.
- Current batch member는 한 번에 resolve합니다. Per-row N+1 query를 기본 구현으로 사용하지 않습니다.
- Overflow는 fail-closed preparation error이며 provider를 호출하지 않습니다.
- Ledger schema version과 backfill completion marker가 지원되지 않거나 미완료이면 새 writer는 시작하지 않습니다.

### Impossible mixed states

다음은 정상 priority로 수용하지 않고 `LogicalInvariantBreach`입니다.

```text
SENT 없이 QUARANTINED와 SENDING 동시 존재
SENT 없이 SENDING 두 건 이상 존재
동일 physical row가 서로 다른 canonical logical key로 해석됨
같은 operation에 동일 logical key owner 중복
ledger QUARANTINED와 retained SENDING 동시 존재
cutover 뒤 retained physical SENT/QUARANTINED에 대응하는 ledger evidence 누락
```

Invariant breach에서는 provider를 호출하거나 follower를 임의 상태로 mirror하지 않습니다. Critical metric과 audit evidence를 남기고 operator 조사에 맡깁니다.

Ledger `SENT` evidence가 있으면 confirmed fulfillment가 unknown/in-flight를 해소하므로 `LogicalFulfilled`가 우선합니다.

### Resolution priority

Impossible mixed state가 없을 때 다음 순서로 판정합니다.

```text
1. Ledger SENT
   -> LogicalFulfilled

2. Ledger QUARANTINED
   -> LogicalUnresolved

3. Ledger absent + retained SENDING 1건
   -> LogicalInFlight

4. Ledger absent + PENDING/FAILED 중 최소 (created_at,id) owner 선택
   owner=PENDING -> LogicalActive
   owner=FAILED  -> LogicalFailed
```

### Resolution actions

#### `LogicalFulfilled`

- Provider를 호출하지 않습니다.
- Ledger `SENT`를 transaction 안에서 다시 확인하고 current/follower `PENDING`, `FAILED`, `QUARANTINED`를 `SENT`로 reconcile합니다.
- Attempt는 유지합니다.
- `sent_at`은 reconciliation 시각입니다.
- Tracking을 새로 추정하지 않습니다.

#### `LogicalUnresolved`

- Provider를 호출하지 않습니다.
- Ledger `QUARANTINED`를 transaction 안에서 다시 확인하고 current/follower `PENDING`, `FAILED`를 `QUARANTINED`로 mirror합니다.
- Follower attempt는 유지합니다.
- Existing quarantined owner attempt는 다시 증가시키지 않습니다.

#### `LogicalInFlight`

- Provider를 호출하지 않습니다.
- Current/follower `PENDING`을 attempt 없이 defer합니다.
- Due는 `max(now + RetryBackoff, inFlight.locked_at + LockTimeout)`입니다.

#### `LogicalActive`

- Deterministic owner만 preparation/send를 계속합니다.
- Follower `PENDING`은 attempt 없이 owner due에 맞춰 defer합니다.
- Owner가 current batch에 없더라도 follower가 owner를 대체하지 않습니다.

#### `LogicalFailed`

- Provider를 호출하지 않습니다.
- Follower `PENDING`을 `FAILED`로 mirror합니다.
- Follower attempt는 유지합니다.
- Revive는 group 단위로만 수행합니다.

### Batch leader

같은 batch의 logical key가 같은 row는 resolution 전에 `(created_at,id)`로 정렬합니다. Deterministic owner가 batch 안에 있으면 그 row만 candidate입니다. Owner가 batch 밖에 있으면 batch row 전체가 owner state를 따릅니다.

### Replica 경계

현재 replica=1과 synchronous dispatch round를 전제로 합니다. Replica>1에서는 computed owner 조회와 claim 사이 race를 막는 persisted key/group-affinity가 추가로 필요합니다. 이 계약은 scale-out 승인이 아닙니다.

## Preparation 결과

```text
ReadyToSend
LogicalFulfilled
LogicalUnresolved
LogicalInFlight
LogicalOwnerPendingElsewhere
LogicalFailed
LogicalInvariantBreach
ClaimDeferred
PreparationRetryableFailure
PreparationPermanentFailure
```

### `ReadyToSend`

Owner의 payload, rendering, route, stable client request ID, tracking requirement, immutable request가 확정됐습니다.

### `ClaimDeferred`

Post-level claim을 다른 execution이 보유합니다. Attempt 없이 defer합니다.

### Preparation failure

```text
nextAttempt = owner.attempt_count + 1
nextAttempt < MaxRetries  -> owner PENDING retry + follower due alignment
nextAttempt >= MaxRetries -> owner FAILED + follower FAILED mirror
```

Permanent failure는 즉시 owner/group `FAILED`입니다. Follower attempt는 증가하지 않습니다.

## Provider operation

```go
type PreparedSendOperation struct {
    OperationID     string
    ClientRequestID string
    Owners          []PreparedOwner
    LedgerKeys      []LogicalDeliveryKey
    Tracking        []TrackingRequirement
    Request         ImmutableSendRequest
}
```

- Owner logical key는 operation 안에서 중복될 수 없습니다.
- Tracking requirement는 canonical post identity로 deduplicate합니다.
- Grouped message member는 서로 다른 logical key여야 합니다.

### `BeginSending`

Owner member 전체가 exact `PENDING + PreparationLease`를 만족할 때만 전체를 `SENDING`으로 변경합니다.

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

한 owner라도 conflict/missing이면 전체 rollback하고 provider를 호출하지 않습니다.

Commit response loss는 provider call 전에 primary exact read-back으로 판정합니다. Exact post-state만 provider call 권한을 만듭니다.

### Send lease budget

```text
SendingFinalizeGrace = max(2 * PollInterval, 5 seconds)
LockTimeout >= DeliverySendTimeout + SendingFinalizeGrace
```

Provider call 전 remaining lease budget이 부족하면 provider를 호출하지 않고 known-not-delivered retryable outcome으로 처리합니다.

## Provider outcome taxonomy

```go
type ProviderOutcomeKind uint8

const (
    ProviderDelivered ProviderOutcomeKind = iota + 1
    ProviderKnownNotDeliveredRetryable
    ProviderKnownNotDeliveredPermanent
    ProviderOutcomeUnknown
)
```

Stable request ID의 존재만으로 retry-safe가 되지는 않습니다. Provider의 request rejection, dedupe, result-query 계약이 증거여야 합니다.

### Grouped fallback

- Outcome unknown에서는 fallback을 금지합니다.
- Known-not-accepted + `fallback_allowed=true`에서만 individual fallback을 허용합니다.
- Fallback 자체는 attempt를 소비하지 않습니다.
- Individual fallback 전 lease budget을 다시 확인합니다.

## Group transition semantics

### Owner retry

- Owner: `SENDING/PENDING leased -> PENDING`, attempt +1, due 설정, lock clear, version +1
- Follower `PENDING`: attempt 유지, due owner와 정렬, lock clear, version +1
- Terminal/in-flight mixed follower는 conflict 또는 invariant breach

### Owner terminal failure

- Owner: `FAILED`, attempt +1, lock clear, version +1
- Follower `PENDING`: `FAILED`, attempt 유지, lock clear, version +1
- Provider는 재호출하지 않습니다.

### Owner quarantine

- Stale owner: `QUARANTINED`, attempt +1, lock clear, version +1
- Follower `PENDING/FAILED`: `QUARANTINED`, attempt 유지, lock clear, version +1
- Existing `SENT` evidence가 발견되면 fulfilled reconciliation이 우선합니다.

### Owner success

Success transaction:

1. Operation owner 전체 exact `SENDING + SendFence` 확인
2. Owner를 `SENT`, canonical `sent_at`, lock clear, version +1
3. Same-logical follower `PENDING/FAILED/QUARANTINED`를 `SENT`로 reconcile, attempt 유지, version +1
4. Tracking requirement를 canonical post별로 deduplicate
5. `RequireClaimOrAlreadySent`는 exact token으로 mark-sent를 시도하고, 이미 sent면 수용
6. `RequireAlreadySent`는 durable sent를 확인
7. Owner logical key마다 ledger `RecordSent`
8. Latency classification과 tracking mutation commit

Tracking이 neither exact-token nor already-sent이면 rollback합니다. Provider는 다시 호출하지 않습니다.

Transaction은 touched outbox ID를 반환합니다. Commit이 확인된 뒤 aggregate projector를 즉시 실행하고 background projector가 current child state에서 재수렴합니다. Aggregate 실패는 committed delivery/tracking/ledger를 rollback하지 않습니다.

### Already-fulfilled reconciliation

Provider call 전 ledger `SENT` evidence가 있으면 tracking mutation 없이 follower만 `SENT`로 reconcile합니다. Ledger key/state는 transaction 안에서 다시 확인합니다.

## Decision model

Policy는 nullable patch map이 아니라 sealed concrete decision을 반환합니다.

```go
type DecisionContext struct {
    RuleID               RuleID
    LogicalKeyHash       string
    OwnerID              int64
    ExpectedStatus       DeliveryStatus
    ExpectedVersion      int64
    ExpectedAttemptCount int
    ExpectedLockedAt     time.Time
    At                   time.Time
}
```

최소 decision:

```text
BeginSendDecision
RetryLogicalGroupDecision
FailLogicalGroupDecision
SentLogicalGroupDecision
FulfilledReconciliationDecision
UnresolvedPropagationDecision
DeferLogicalFollowerDecision
ReviveLogicalGroupDecision
```

Invalid field combination을 public struct literal로 만들 수 없어야 합니다.

## Transition store API

```go
type TransitionStore interface {
    ClaimPending(context.Context, ClaimRequest) ([]ClaimedDelivery, error)
    ResolveLogicalGroups(context.Context, []ClaimedDelivery) ([]LogicalGroupSnapshot, error)
    BeginSending(context.Context, PreparedSendOperation) (StartedSendOperation, ApplyResult, error)
    ReconcileFulfilled(context.Context, FulfilledCommand) (ApplyResult, error)
    PropagateUnresolved(context.Context, UnresolvedCommand) (ApplyResult, error)
    DeferFollowers(context.Context, []DeferCommand) ([]ApplyResult, error)
    ScheduleLogicalRetry(context.Context, LogicalRetryCommand) (ApplyResult, error)
    FailLogicalGroup(context.Context, LogicalFailCommand) (ApplyResult, error)
    CompleteSent(context.Context, SentOperation) (ApplyResult, error)
    QuarantineStaleSending(context.Context, time.Time, int) ([]LogicalGroupResult, error)
    ReviveFailedLogicalGroups(context.Context, ReviveRequest) (int64, error)
}
```

금지 API:

```text
UpdateStatus(id,status)
ApplyPatch(map[string]any)
Expected token을 생략한 writer
Repository가 MaxRetries를 받는 API
Raw failure string branch
ID-only SENDING -> SENT recovery
```

## Apply outcome과 commit 판정

```text
Applied
Conflict
Missing
Indeterminate
```

- `Applied`: commit 또는 exact post-state 확인
- `Conflict`: row는 존재하지만 expected state/token 불일치
- `Missing`: row 없음
- `Indeterminate`: exact pre/post/coherent conflict 판정 불가

Group member mixed state와 delivery/tracking/ledger mismatch는 일반 conflict가 아니라 atomicity breach입니다.

## Outcome unknown

허용 작업:

- warning audit
- bounded metric
- masked operation evidence

금지 작업:

- state write
- retry/failure finalization
- tracking/claim mutation
- grouped fallback
- external resend

Stale sweeper가 owner와 follower group을 quarantine하고 같은 transaction에서 ledger `QUARANTINED`를 기록합니다.

## Revive

Revive는 logical group 단위로 판정합니다.

허용 조건:

- Ledger row 없음
- deterministic owner가 `FAILED`
- 관련 outbox never-sent이며 freshness window 안
- active group lock 없음
- revive policy와 batch limit 충족

적용:

- Owner/follower: `PENDING`, attempt 0, due 정렬, lock/error clear, version +1
- Commit 뒤 touched outbox: 표준 aggregate projector

Commit `Indeterminate`에서는 stale selected ID set을 반복하지 않고 eligibility selection부터 다시 수행합니다.

## Outbox fanout

### `CompleteWithoutTargets`

Active outbox claim과 child 부재를 같은 transaction에서 확인한 뒤 `SENT`, canonical `sent_at`, 같은 시각의 `terminal_at`, lock clear를 적용합니다.

### `MaterializeFanout`

한 transaction에서:

1. Active outbox claim 확인
2. Target room canonicalize/dedupe
3. `(outbox_id,room_id)` idempotent insert
4. Outbox lock clear
5. Status `PENDING` 유지

Commit response loss는 canonical child 전체와 lock clear를 primary read-back합니다. 일부 child만 존재하면 atomicity breach입니다.

### Post-fanout writer

Child가 하나라도 있으면 direct writer는 conflict를 반환하고 aggregate projector가 상태를 소유합니다.

## Aggregate projection

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

하나의 atomic SQL statement로 유지합니다. Outbox `sent_at`은 NULL일 때만 설정합니다.

Logical group transition은 touched outbox ID 전체를 반환하고 projector가 모두 수렴시켜야 합니다.

`terminal_at` projection:

```text
next=PENDING              -> NULL
PENDING -> SENT/FAILED    -> canonical projectionAt
FAILED -> SENT            -> canonical projectionAt
terminal -> same terminal -> existing terminal_at 보존
```

Aggregate transaction과 delivery/tracking/ledger transaction은 분리합니다. Projector failure는 telemetry와 background convergence 대상이며 provider 또는 lifecycle command를 재실행하지 않습니다.

## Cleanup과 retention

Full outbox/delivery row와 compact terminal evidence의 retention을 분리합니다.

- Ledger schema version과 backfill completion marker 없이는 cleanup을 실행하지 않습니다.
- `terminal_at < fixed cutoff`인 terminal outbox만 후보입니다.
- Active outbox lock 또는 `PENDING/SENDING` child가 있으면 삭제하지 않습니다.
- `SENT/QUARANTINED` child마다 ledger에 동일하거나 더 강한 evidence가 있어야 합니다.
- Same-logical nonterminal row가 있으면 terminal owner/follower를 삭제하지 않습니다.
- Cleanup retry가 cutoff를 더 최신 시각으로 앞당기면 안 됩니다.
- Candidate selection, ledger verification, sibling guard, delete는 bounded transaction입니다.
- Ledger는 초기 범위에서 자동 삭제하지 않습니다.

## 금지 전이

```text
SENT -> *
QUARANTINED -> PENDING/FAILED
FAILED -> PENDING outside group revive
PENDING unclaimed -> SENDING
SENDING -> SENDING re-claim
Follower의 독립 provider send/attempt 증가
```

허용되는 monotonic reconciliation:

```text
PENDING/FAILED/QUARANTINED -> SENT
단, exact same-logical SENT evidence가 있을 때만
```

## Configuration validation

```text
MaxRetries > 0
RetryBackoff > 0
LogicalGroupScanLimit > 0
LockTimeout > 0
DeliverySendTimeout > 0
LockTimeout >= DeliverySendTimeout + max(2*PollInterval, 5s)
ReviveFreshnessWindow > 0 when revive enabled
ClaimFreshnessWindow >= ReviveFreshnessWindow + ReviveInterval
CleanupAfter >= ClaimFreshnessWindow + ReviveFreshnessWindow + CleanupSafetyMargin
SupportedLedgerSchemaVersion = persisted schema_version
LedgerBackfillCompleted = true before writer/cleanup cutover
ExternalLifecycleWriterCount = 0 before high-water capture
```

Production invalid config는 startup에서 거부합니다.

## Observability

### Metric

```text
youtube_delivery_transition_total{rule,from,to,result}
youtube_delivery_logical_group_total{resolution,result}
youtube_delivery_logical_follower_total{action}
youtube_delivery_outcome_unknown_total{transport}
youtube_delivery_commit_adjudication_total{operation,result}
youtube_delivery_commit_indeterminate_total{operation,phase}
youtube_delivery_atomicity_breach_total{operation}
youtube_delivery_tracking_resolution_total{requirement,result}
youtube_delivery_quarantine_total{reason}
youtube_delivery_ledger_operation_total{operation,result}
youtube_delivery_ledger_backfill_total{phase,result}
youtube_delivery_cleanup_guard_total{reason,result}
youtube_outbox_aggregate_transition_total{from,to}
youtube_outbox_aggregate_lag_seconds
```

Raw IDs, logical key, room, error 문자열은 label에 넣지 않습니다.

### Structured log

```text
rule_id
logical_key_hash
logical_owner_id
follower_count
operation_id
client_request_id_hash
from_state
to_state
attempt_count
row_version
tracking_requirement
apply_outcome
provider_effect_started
provider_effect_confirmed
ledger_schema_version
ledger_backfill_completed
```

## Contract tests

### Logical group

```text
TestLogicalKeyUsesCanonicalPostAndRoom
TestSameBatchSelectsDeterministicOwner
TestFollowerCannotUseIndependentAttemptBudget
TestPendingOwnerDefersFollowerToOwnerDue
TestFailedOwnerMirrorsFollowerFailed
TestSentEvidenceReconcilesFailedAndQuarantinedFollowers
TestQuarantinedEvidenceBlocksProviderCall
TestSendingEvidenceDefersProviderCall
TestQuarantineAndSendingMixedStateIsBreach
TestMultipleSendingRowsIsBreach
TestProceedCacheHitStillResolvesLogicalGroup
TestLogicalGroupOverflowFailsClosed
TestInvalidLogicalIdentityFailsBeforeProviderCall
TestLedgerSentEvidenceReconcilesCleanedPhysicalRows
TestLedgerQuarantineEvidenceBlocksProviderCall
```

### Tracking

```text
TestFirstRoomSuccessConsumesClaimToken
TestLaterRoomSuccessAcceptsAlreadySentTracking
TestAlreadySentPreparationFinalizesWithoutClaimMutation
TestTrackingNeitherClaimNorSentRollsBackDeliverySuccess
TestTrackingRequirementDeduplicatesSamePostInGroupedBatch
```

### CAS/operation

```text
TestClaimIncrementsVersion
TestBeginSendingAllOrNone
TestBeginCommitUnknownNeverCallsProviderBeforeReadBack
TestCompleteSentUpdatesOwnerFollowersAndTrackingAtomically
TestCompleteSentConfirmedNonCommitRetriesDBOnly
TestCompleteSentIndeterminateNeverResends
TestMixedGroupStateIsAtomicityBreach
TestSuccessEnvelopeCommitsDeliveryTrackingAndLedgerAtomically
TestQuarantineEnvelopeCommitsDeliveryAndLedgerAtomically
TestLedgerReadBackMismatchIsAtomicityBreach
```

### Outcome/crash

```text
TestOutcomeUnknownWritesNothingAndKeepsClaim
TestGroupedUnknownNeverFallsBack
TestCrashAfterSendingCommitQuarantinesLogicalGroup
TestCrashAfterProviderSuccessNeverGuessesTracking
TestAggregateReconcilesAfterDeliveryCommitCrash
TestAggregateFailureDoesNotRollBackTerminalEnvelope
```

### Revive/cleanup

```text
TestReviveResetsLogicalOwnerAndFollowers
TestReviveRejectsSentOrQuarantinedGroup
TestReviveRejectsLogicalKeyPresentInLedger
TestCleanupRetainsTerminalEvidenceWhileFollowerNonterminal
TestCleanupRequiresCompletedLedgerBackfill
TestCleanupRequiresLedgerEvidenceForEverySentOrQuarantinedDelivery
TestTerminalAtUsesLatestTerminalEvidence
TestCleanupAfterExceedsFreshnessEnvelope
```

### Backfill

```text
TestBackfillCapturesFixedDeliveryAndOutboxHighWater
TestBackfillResumeUsesDurableCursor
TestBackfillUpsertIsMonotonic
TestBackfillCompletionRequiresCanonicalAntiJoinZero
TestBackfillCompletionRejectsUnprovenHistoricalCoverage
```

## 완료 판정

1. Policy와 SQL이 retry를 중복 계산하지 않습니다.
2. Community/Shorts canonical logical key resolver가 하나입니다.
3. Logical group의 provider/attempt owner가 하나입니다.
4. Follower가 owner retry budget을 우회하지 않습니다.
5. Durable state와 impossible mixed state를 같은 규칙으로 해석합니다.
6. Post-level cache가 room-level logical resolution을 생략하지 않습니다.
7. Outcome unknown은 write, release, fallback, resend를 하지 않습니다.
8. Claim과 모든 mutation이 version을 증가시킵니다.
9. Begin/finalization operation이 all-or-none입니다.
10. Tracking exact token과 already-sent가 idempotent하게 수용됩니다.
11. Provider success 뒤 모든 DB 오류에서 provider 재호출은 0회입니다.
12. ID-only success recovery가 없습니다.
13. Child outbox status는 aggregate projector만 계산합니다.
14. Worker 전용 store/row model이 alarm-worker internal에 있습니다.
15. 모든 confirmed-success와 outcome-unknown terminal transition이 같은 transaction에서 logical ledger evidence를 남깁니다.
16. 고정 high-water backfill이 durable cursor로 재개되고 canonical anti-join 0건으로 완료됩니다.
17. 삭제된 과거 row까지 포함한 `legacy_coverage_start_at`이 모든 replay/freshness 경계보다 앞선다는 근거가 없으면 cutover를 차단합니다.
18. Cleanup은 `terminal_at`과 completed ledger gate를 사용하고, logical terminal evidence를 freshness envelope보다 오래 보존합니다.
19. Poller와 API를 포함한 모든 lifecycle 직접 writer가 제거되거나 새 transition owner로 전환됩니다.
20. Contract, logical-owner, tracking, ledger backfill, integration, crash, commit fault-injection, race test가 통과합니다.
21. Decision record를 `verified`로 올릴 evidence가 저장소에 남습니다.
