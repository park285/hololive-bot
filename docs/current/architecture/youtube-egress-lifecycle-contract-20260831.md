# YouTube egress lifecycle contract

작성일: 2026-08-31 KST  
적용 결정: `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership`  
적용 런타임: `hololive-alarm-worker`  
상위 아키텍처: [`youtube-egress-lifecycle-transition-ownership-20260831.md`](youtube-egress-lifecycle-transition-ownership-20260831.md)

## 규범 용어

이 문서의 `해야 한다`, `금지한다`, `오직`은 구현과 테스트가 지켜야 하는 규범입니다. `권장한다`는 기본 선택이지만 동등한 안전 속성을 증명하면 대체할 수 있습니다.

## 범위

이 계약은 다음 데이터와 실행 경로를 다룹니다.

- `youtube_notification_outbox`의 pre-fanout intent 처리
- `youtube_notification_delivery`의 claim, preparation, send, retry, terminal 처리
- community/shorts alarm-once claim과 delivery 성공 tracking의 결합
- stale `SENDING` quarantine
- `FAILED` revive
- child delivery 기반 outbox aggregate projection

다음 항목은 이 계약에 포함하지 않습니다.

- provider별 HTTP/SDK payload 계약
- alarm-worker replica>1 승인
- `QUARANTINED` 수동 replay UI 또는 운영 API
- event sourcing 또는 별도 workflow engine

## 핵심 불변식

### I-001. PostgreSQL source of truth

현재 상태, attempt, due 시각, lock 시각, delivery version은 PostgreSQL 값이 정본입니다. process memory의 state object는 정본이 될 수 없습니다.

### I-002. 단일 정책 평가

retry 소진, preparation/send failure 종류, revive 허용 여부는 lifecycle policy에서 한 번만 계산합니다. SQL `CASE`나 repository가 `MaxRetries`를 다시 평가해서는 안 됩니다.

### I-003. fenced transition

claim 이후 모든 delivery mutation은 `id + expected status + expected row_version + expected locked_at`을 검사해야 합니다. expected attempt도 함께 검사합니다.

### I-004. external-effect ordering

provider 호출은 `PENDING -> SENDING` commit 이후에만 시작할 수 있습니다. provider 호출을 시작한 뒤 `PENDING`으로 간주해서는 안 됩니다.

### I-005. outcome unknown hold

provider 처리 여부가 불명확하면 즉시 DB mutation을 하지 않습니다. delivery는 `SENDING`에 남고 stale-sending sweeper만 `QUARANTINED`로 이동시킬 수 있습니다.

### I-006. no automatic quarantine exit

`QUARANTINED`는 이 계약의 자동 경로에서 terminal입니다. `FAILED` revive나 일반 retry가 `QUARANTINED`를 변경해서는 안 됩니다.

### I-007. sent terminal

`SENT`는 자동 경로에서 절대 terminal입니다. stale failure, revive, cleanup 전 단계가 `SENT`를 다른 상태로 덮어쓰면 안 됩니다.

### I-008. tracking follows committed success

community/shorts tracking mutation은 실제 delivery success transaction 안에서, CAS에 성공한 row에 대해서만 실행합니다. transition conflict가 난 row의 tracking을 변경해서는 안 됩니다.

### I-009. aggregate writer ownership

child delivery가 존재하는 outbox의 `status`, aggregate `error`, aggregate `sent_at`은 aggregate projector만 계산합니다. revive는 child를 변경한 뒤 동일 aggregate projector를 같은 transaction에서 호출합니다.

### I-010. no dual write

같은 delivery에 legacy writer와 새 transition writer를 동시에 적용하는 기간을 만들지 않습니다. shadow mode는 decision 비교만 허용하고 DB write는 한 경로만 수행합니다.

## 상태와 저장 필드

### Outbox 상태

Outbox persisted vocabulary는 기존 값을 유지합니다.

```text
PENDING
SENT
FAILED
```

Outbox 상태는 phase에 따라 의미가 다릅니다.

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
| `PENDING` | provider 호출 전이거나, known-not-delivered retry를 기다립니다. `locked_at`이 있으면 preparation lease를 보유 중입니다. |
| `SENDING` | exact send intent가 DB에 commit됐고 provider 호출이 시작될 수 있습니다. 실제 provider 수신을 증명하지는 않습니다. |
| `SENT` | provider 성공 또는 이미 충족된 delivery를 local transaction으로 terminal 처리했습니다. |
| `FAILED` | retry 소진 또는 permanent known failure입니다. guarded revive만 허용합니다. |
| `QUARANTINED` | provider 처리 여부를 증명할 수 없습니다. 자동 retry와 revive를 금지합니다. |

### `sent_at` 의미

`sent_at`은 delivery가 `SENT`로 local commit된 시각입니다. 다음 두 경우를 포함합니다.

1. provider 성공 응답 후 `CompleteSent`가 commit된 경우
2. pre-send alarm-once 확인에서 해당 room delivery가 이미 충족되었다고 판정해 `AlreadySatisfied`가 commit된 경우

따라서 `sent_at`은 항상 provider response timestamp라는 뜻이 아닙니다. transport latency가 필요하면 별도 telemetry evidence를 사용합니다.

### `attempt_count` 의미

`attempt_count`는 **실패로 종료되거나 결과 불명으로 격리된 처리 시도 수**입니다.

다음 작업에서는 증가하지 않습니다.

- claim
- claim defer
- `BeginSend`
- provider 성공
- `AlreadySatisfied`
- grouped send의 known-not-accepted fallback 자체

다음 작업에서는 1 증가합니다.

- retryable preparation failure
- permanent preparation failure
- known-not-delivered retryable send failure
- known-not-delivered permanent send failure
- stale `SENDING -> QUARANTINED`

revive는 기존 동작과 동일하게 `attempt_count=0`으로 재설정합니다.

### `row_version`

`youtube_notification_delivery`에는 다음 컬럼이 있어야 합니다.

```sql
row_version bigint NOT NULL DEFAULT 0 CHECK (row_version >= 0)
```

다음 mutation은 version을 1 증가시킵니다.

- `PENDING` claim
- claim defer 또는 preparation failure 처리
- `BeginSend`
- send success/failure 처리
- quarantine
- revive

`row_version`은 인덱싱하지 않습니다.

### 시간 정규화

application service는 operation 또는 batch 시작 시각을 한 번 읽고 repository 경계에서 UTC microsecond precision으로 정규화합니다. 하나의 atomic operation 안에서는 서로 다른 `time.Now()` 값을 혼용하지 않습니다.

## token 계약

### Preparation lease

```go
type PreparationLease struct {
    DeliveryID  int64
    RowVersion  int64
    LockedAt    time.Time
}
```

Preparation lease는 `PENDING + locked_at` row만 나타냅니다. payload load, formatting, request construction, alarm-once claim이 이 lease 아래에서 수행됩니다.

### Send fence

```go
type SendFence struct {
    DeliveryID  int64
    RowVersion  int64
    LockedAt    time.Time
}
```

Send fence는 `SENDING + locked_at` row만 나타냅니다. `BeginSending`이 성공한 뒤 반환하며 success/failure finalization에서 사용합니다.

Preparation lease와 Send fence는 구조가 비슷하더라도 별도 Go type이어야 합니다. preparation token으로 `SENDING` finalization을 호출하거나 Send fence로 preparation failure를 적용하는 코드는 컴파일되지 않아야 합니다.

### Alarm claim token

Community/shorts alarm-once claim은 delivery fencing과 다른 domain token입니다.

```go
type AlarmClaimToken struct {
    Kind         OutboxKind
    PostID       string
    AuthorizedAt time.Time
}
```

- preparation 실패 또는 provider가 수락하지 않았다고 확정한 실패에서는 새로 획득한 claim을 release합니다.
- success에서는 delivery `SENT`와 tracking mark를 같은 transaction에서 commit합니다.
- outcome unknown에서는 claim을 즉시 release하지 않습니다. release하면 provider가 실제 성공했을 때 다른 실행이 중복 발송할 수 있습니다.
- `QUARANTINED` 처리도 alarm sent를 추정해서 기록하지 않습니다.

## claim protocol

`ClaimPending`은 lifecycle transition이 아니라 concurrency protocol입니다.

대상 조건은 다음과 같습니다.

```text
status = PENDING
next_attempt_at <= now
locked_at IS NULL OR locked_at < now - LockTimeout
```

claim SQL은 `FOR UPDATE SKIP LOCKED`로 row를 고르고 다음을 수행합니다.

```sql
SET locked_at = $now,
    row_version = row_version + 1
```

반환 row의 새 version과 `locked_at`이 Preparation lease가 됩니다.

Claim은 `attempt_count`를 증가시키지 않습니다. claim 자체가 사용자에게 관측되는 외부 시도는 아니기 때문입니다.

## preparation 결과

Preparation은 provider 호출 전에 끝나는 모든 작업입니다.

- outbox/payload load
- payload validation
- message rendering
- final room과 transport route 확정
- deterministic client request ID 확정
- community/shorts alarm-once claim과 per-room sent 확인
- provider request construction

Preparation 결과는 다음 중 하나여야 합니다.

### `ReadyToSend`

provider 호출에 필요한 값이 모두 immutable하게 확정됐습니다. `BeginSending` 대상입니다.

### `AlreadySatisfied`

해당 room이 같은 community/shorts post를 이미 전달받았다는 durable evidence가 있습니다.

```text
PENDING leased -> SENT
attempt_count 유지
provider 호출 없음
새 alarm tracking mutation 없음
```

이 transition은 active Preparation lease를 검사합니다.

### `ClaimDeferred`

다른 실행이 community/shorts post claim을 보유하고 있습니다. 오류가 아니며 attempt를 소비하지 않습니다.

```text
PENDING leased -> PENDING unleased
attempt_count 유지
next_attempt_at = now + RetryBackoff
```

### `PreparationRetryableFailure`

provider 호출 전 일시적인 local failure입니다.

```text
nextAttempt = attempt_count + 1
nextAttempt < MaxRetries  -> PENDING unleased, retry due 설정
nextAttempt >= MaxRetries -> FAILED
```

### `PreparationPermanentFailure`

payload invariant, 지원하지 않는 target 등 동일 입력으로 다시 시도해도 성공할 수 없는 local failure입니다.

```text
PENDING leased -> FAILED
attempt_count + 1
```

## send operation

### Prepared operation

외부 provider 요청 한 건의 최소 원자 단위를 `PreparedSendOperation`이라고 합니다.

```go
type PreparedSendOperation struct {
    OperationID     string
    ClientRequestID string
    DeliveryLeases  []PreparationLease
    AlarmClaims     []AlarmClaimToken
    Request         ImmutableSendRequest
}
```

`OperationID`는 process-local tracing identity이고 `ClientRequestID`는 provider idempotency 계약에 사용하는 stable identity입니다. 둘을 혼용하지 않습니다.

개별 메시지는 delivery 한 건을 포함하고, grouped 메시지는 동일 provider 요청에 포함되는 delivery 집합을 가집니다. operation membership과 request payload는 `BeginSending` 이후 변경할 수 없습니다.

### all-or-none begin

`BeginSending`은 provider operation 단위로 all-or-none이어야 합니다.

- 모든 member가 expected `PENDING + PreparationLease`를 만족하면 전체를 `SENDING`으로 변경합니다.
- 한 member라도 conflict 또는 missing이면 transaction 전체를 rollback하고 provider를 호출하지 않습니다.
- 성공하면 모든 member에 새 Send fence를 반환합니다.

이 규칙은 CAS에서 탈락한 row를 포함한 grouped payload를 발송하는 것을 막습니다.

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

### send lease budget

stale sweeper가 실행 중인 provider call을 quarantine하지 않도록 다음 derived margin을 사용합니다.

```text
SendingFinalizeGrace = max(2 * PollInterval, 5 seconds)
```

설정은 다음을 만족해야 합니다.

```text
LockTimeout >= DeliverySendTimeout + SendingFinalizeGrace
```

새 provider call은 다음 조건을 만족할 때만 시작합니다.

```text
send fence expiry - now >= DeliverySendTimeout + SendingFinalizeGrace
```

시간이 부족하면 provider를 호출하지 않고 해당 row를 known-not-sent retryable failure로 처리합니다.

Provider call context deadline은 configured send timeout과 send-fence deadline 중 더 이른 값으로 제한합니다. deadline 이후 반환된 timeout은 provider 처리 여부를 증명하지 못하므로 outcome unknown입니다.

## provider outcome taxonomy

Transport adapter는 SDK 오류 또는 HTTP response를 다음 typed 결과 중 하나로 분류합니다.

```go
type ProviderOutcomeKind uint8

const (
    ProviderDelivered ProviderOutcomeKind = iota + 1
    ProviderKnownNotDeliveredRetryable
    ProviderKnownNotDeliveredPermanent
    ProviderOutcomeUnknown
)
```

### `ProviderDelivered`

provider가 성공을 확정했습니다.

### `ProviderKnownNotDeliveredRetryable`

provider가 요청을 수락하지 않았고 같은 logical delivery를 다시 시도해도 안전하다는 증거가 있습니다.

### `ProviderKnownNotDeliveredPermanent`

provider가 요청을 수락하지 않았고 같은 입력으로 재시도해도 성공할 수 없습니다.

### `ProviderOutcomeUnknown`

provider가 처리했는지 증명할 수 없습니다. timeout과 connection reset은 provider 계약이 반대로 증명하지 않는 한 이 범주입니다.

Stable client request ID가 존재한다는 사실만으로 retry-safe가 되지는 않습니다. provider가 동일 ID replay를 실제로 deduplicate한다는 계약이나 결과 조회 기능이 있어야 합니다.

`Message`는 진단용이고 policy 입력으로 사용하지 않습니다. 상태 결정은 `Kind`와 stable failure code만 사용합니다.

## delivery transition matrix

| phase/state | event/result | next | attempt | lock | 비고 |
|---|---|---|---:|---|---|
| `PENDING` unclaimed | claim | `PENDING` leased | 유지 | claim 시각 | concurrency protocol |
| `PENDING` leased | `AlreadySatisfied` | `SENT` | 유지 | 해제 | provider 호출 없음 |
| `PENDING` leased | `ClaimDeferred` | `PENDING` | 유지 | 해제 | due를 뒤로 이동 |
| `PENDING` leased | preparation retryable, retry 잔여 | `PENDING` | +1 | 해제 | due 설정 |
| `PENDING` leased | preparation retryable, retry 소진 | `FAILED` | +1 | 해제 | terminal |
| `PENDING` leased | preparation permanent | `FAILED` | +1 | 해제 | terminal |
| `PENDING` leased | `BeginSend` | `SENDING` | 유지 | send 시각으로 갱신 | 새 Send fence 반환 |
| `SENDING` | delivered | `SENT` | 유지 | 해제 | tracking transaction |
| `SENDING` | known-not-delivered retryable, retry 잔여 | `PENDING` | +1 | 해제 | due 설정 |
| `SENDING` | known-not-delivered retryable, retry 소진 | `FAILED` | +1 | 해제 | terminal |
| `SENDING` | known-not-delivered permanent | `FAILED` | +1 | 해제 | terminal |
| `SENDING` | outcome unknown | 상태 변경 없음 | 유지 | 유지 | 즉시 DB write 금지 |
| stale `SENDING` | lease expired | `QUARANTINED` | +1 | 해제 | sweeper만 집행 |
| `FAILED` | eligible revive | `PENDING` | 0으로 reset | 해제 | FAILED child만 reset |

## retry 계산

현재 retry 알고리즘은 책임 분리 과정에서 변경하지 않습니다.

```text
nextAttemptCount = currentAttemptCount + 1
retryDelay = max(config.RetryBackoff, providerRetryAfter)
nextAttemptAt = eventAt + retryDelay
```

`nextAttemptCount >= MaxRetries`면 `FAILED`입니다. Retry algorithm을 exponential backoff나 jitter로 바꾸려면 별도 동작 변경으로 다룹니다.

## grouped send와 fallback

한 grouped provider request는 정확한 delivery member 집합을 가집니다.

1. `BeginSending`은 member 전체에 all-or-none입니다.
2. grouped request가 성공하면 member 전체를 한 success transaction으로 finalize합니다.
3. grouped request가 outcome unknown이면 member 전체를 `SENDING`에 유지하고 fallback하지 않습니다.
4. fallback은 provider가 grouped request를 수락하지 않았다고 확정하고 adapter가 `fallback_allowed=true`를 반환한 경우에만 허용합니다.
5. fallback 자체는 attempt를 소비하지 않습니다.
6. individual fallback call을 시작하기 전에 각 row의 send lease budget을 확인합니다.
7. budget이 부족해 call을 시작하지 않은 row는 known-not-delivered retryable로 처리합니다.
8. individual fallback outcome은 row별 operation으로 finalize합니다.

Outcome unknown 이후 individual fallback을 실행하면 grouped request와 individual request가 모두 전달될 수 있으므로 절대 금지합니다.

## decision model

Policy는 임의 patch map이 아니라 concrete decision을 반환합니다.

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

type BeginSendDecision struct {
    Common DecisionContext
}

type RetryDecision struct {
    Common             DecisionContext
    NextAttemptCount   int
    NextAttemptAt      time.Time
    FailureCode        FailureCode
    SanitizedMessage   string
}

type FailDecision struct {
    Common             DecisionContext
    NextAttemptCount   int
    FailureCode        FailureCode
    SanitizedMessage   string
}

type SentDecision struct {
    Common DecisionContext
    SentAt time.Time
}

type AlreadySatisfiedDecision struct {
    Common DecisionContext
    SatisfiedAt time.Time
}

type DeferDecision struct {
    Common DecisionContext
    NextAttemptAt time.Time
}
```

Go 구현은 concrete type이나 동등한 sealed representation을 사용할 수 있지만 다음 조건을 만족해야 합니다.

- invalid field combination을 public struct literal로 만들 수 없어야 합니다.
- `RetryDecision`에 `NextAttemptAt` 누락이 불가능해야 합니다.
- `SentDecision`에 failure payload가 들어갈 수 없어야 합니다.
- policy가 선택한 `RuleID`가 metric과 audit까지 전달되어야 합니다.

## repository command API

Transition service는 decision을 검증된 command로 변환합니다.

```go
type TransitionStore interface {
    BeginSending(context.Context, PreparedSendOperation) (StartedSendOperation, ApplyOutcome, error)
    CompleteAlreadySatisfied(context.Context, AlreadySatisfiedCommand) (ApplyOutcome, error)
    DeferClaim(context.Context, DeferCommand) (ApplyOutcome, error)
    ScheduleRetryBatch(context.Context, []RetryCommand) ([]ApplyResult, error)
    FailBatch(context.Context, []FailCommand) ([]ApplyResult, error)
    CompleteSent(context.Context, SentOperation) (ApplyOutcome, error)
    QuarantineStaleSending(context.Context, time.Time, int) ([]int64, int, error)
    ReviveFailedOutboxes(context.Context, ReviveRequest) (int64, error)
}
```

이름은 조정할 수 있지만 다음을 금지합니다.

- `UpdateStatus(id, status)`
- `ApplyPatch(map[string]any)`
- caller가 expected status를 생략하는 API
- repository가 `MaxRetries`를 받는 API
- raw failure string으로 SQL branch를 선택하는 API

## apply outcome

CAS 결과는 다음 중 하나입니다.

```text
Applied
Conflict
Missing
```

- `Applied`: expected state와 token을 만족하여 commit됐습니다.
- `Conflict`: row는 존재하지만 state/version/attempt/lock이 달라졌습니다.
- `Missing`: row가 존재하지 않습니다.

`Conflict`는 정상적인 동시성 결과이며 DB 오류와 구분합니다. 그러나 provider 성공 후 finalization conflict는 일반 경쟁이 아니라 안전 불변식 위반 후보이므로 critical metric과 error log를 남깁니다.

## operation atomicity

외부 provider 요청 한 건에 포함된 delivery 집합은 다음 transition에서 all-or-none입니다.

- `BeginSending`
- grouped `CompleteSent`
- grouped known failure finalization

독립적인 provider operation 사이에는 partial success가 허용됩니다. Batch API는 operation 단위 결과를 반환해야 하며, 일부 operation conflict를 전체 성공으로 숨기면 안 됩니다.

## success transaction

`CompleteSent` transaction은 다음 순서를 지킵니다.

1. operation member 전체가 `SENDING + SendFence`를 만족하는지 확인합니다.
2. member 전체를 `SENT`, `sent_at`, `locked_at=NULL`, `row_version+1`로 변경합니다.
3. 적용된 member에서만 alarm tracking mark를 계산합니다.
4. community/shorts `authorized_at`을 해제하고 `alarm_sent_at`을 기록합니다.
5. latency classification을 기록합니다.
6. commit합니다.

한 member라도 conflict면 transaction을 rollback합니다. provider 호출은 다시 하지 않습니다.

Provider success 후 DB error가 발생하면 같은 immutable `SentOperation`과 Send fence로 local finalization만 재시도할 수 있습니다. 외부 send를 재실행해서는 안 됩니다. process가 success를 commit하기 전에 종료되면 row는 stale `SENDING`으로 남아 quarantine됩니다.

기존의 token을 우회해 delivery ID만으로 `SENDING -> SENT`를 복구하는 경로는 제거해야 합니다. 성공을 알고 있다는 application memory만으로 stale DB row를 덮어쓸 수 없습니다.

## failure finalization

Known-not-delivered failure만 `ScheduleRetryBatch` 또는 `FailBatch`를 호출할 수 있습니다.

- 새로 획득한 alarm claim은 failure commit 이후 release합니다.
- claim release 실패는 delivery 상태를 되돌리지 않지만 metric과 warning을 남깁니다.
- failure message는 민감한 payload, token, room 원문을 포함하지 않도록 sanitize합니다.
- raw SDK error 문자열은 metric label로 사용하지 않습니다.

## outcome unknown 처리

Outcome unknown 경로는 다음 작업만 수행합니다.

- warning audit log
- bounded metric 증가
- operation과 delivery ID의 masked/structured evidence 기록

다음 작업은 금지합니다.

- retry scheduling
- `FAILED` 기록
- 즉시 `QUARANTINED` 기록
- claim release
- provider fallback
- external resend

## quarantine

Sweeper는 다음 조건으로 stale `SENDING`을 고릅니다.

```text
status = SENDING
locked_at < now - LockTimeout
```

선택과 변경은 transaction과 row lock으로 수행합니다.

```text
SENDING -> QUARANTINED
attempt_count + 1
locked_at = NULL
row_version + 1
error = stable sanitized unknown-outcome message
```

Quarantine 후 같은 transaction 또는 commit 직후 aggregate projector를 실행합니다. `QUARANTINED` child는 outbox aggregate에서 failure로 투영합니다.

## revive

Revive 대상 outbox는 다음 조건을 모두 만족해야 합니다.

- outbox aggregate status가 `FAILED`
- outbox `sent_at IS NULL`
- `created_at`이 freshness window 안
- active outbox lock이 없음
- `FAILED` delivery가 하나 이상이거나 child delivery가 전혀 없음
- `QUARANTINED`만 존재하는 outbox가 아님

Child가 있는 경우:

1. `FAILED` delivery만 `PENDING`으로 reset합니다.
2. `SENT`와 `QUARANTINED`는 그대로 둡니다.
3. reset row의 attempt를 0으로, due를 now로, lock과 error를 clear하고 version을 증가시킵니다.
4. 같은 transaction에서 표준 aggregate projector를 실행합니다.

Child가 없는 pre-fanout failure인 경우에만 outbox를 직접 `PENDING`으로 reset합니다.

## outbox fanout contract

### `CompleteWithoutTargets`

다음 조건을 transaction 안에서 검사합니다.

```text
outbox.status = PENDING
outbox.locked_at = expected claim timestamp
child delivery가 없음
```

성공하면 `SENT`, canonical `sent_at`, lock clear를 적용합니다.

### `MaterializeFanout`

다음 작업을 한 transaction에서 수행합니다.

1. active outbox claim token을 확인합니다.
2. target room 집합을 canonicalize하고 중복 제거합니다.
3. `(outbox_id, room_id)` unique key로 delivery를 idempotent insert합니다.
4. outbox lock을 해제합니다.
5. outbox status는 `PENDING`으로 유지합니다.

Transaction 중간 실패 시 child 일부만 남아서는 안 됩니다.

### fanout failure

Subscriber lookup 또는 delivery materialization 전 failure는 outbox intent policy가 retry/FAILED를 결정합니다. SQL이 max retry를 계산하지 않습니다.

### post-fanout direct writer 금지

Child가 존재하면 `CompleteWithoutTargets`, pre-fanout failure writer, 일반 `markSent/markFailed`가 outbox status를 변경해서는 안 됩니다.

## aggregate projection

표준 aggregate priority는 다음과 같습니다.

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

계산과 update는 하나의 SQL statement로 유지합니다. `SENT` projection에서 outbox `sent_at`은 NULL일 때만 설정하여 최초 terminal 시각을 보존합니다.

## 금지 전이

다음 전이는 자동 경로에서 존재하지 않습니다.

```text
SENT -> *
QUARANTINED -> PENDING
QUARANTINED -> FAILED
QUARANTINED -> SENT
FAILED -> SENT
PENDING unclaimed -> SENDING
PENDING -> QUARANTINED
SENDING -> SENDING 재claim
```

Provider evidence 기반 reconciliation 또는 manual replay가 필요하면 별도 결정과 audited command를 설계합니다.

## DB constraint와 사전 감사

Cutover 전에 다음 audit가 0건이어야 합니다.

```sql
SELECT id, status, attempt_count, locked_at, sent_at
FROM youtube_notification_delivery
WHERE status NOT IN ('PENDING', 'SENDING', 'SENT', 'FAILED', 'QUARANTINED')
   OR attempt_count < 0
   OR (status = 'SENDING' AND locked_at IS NULL)
   OR (status = 'SENT' AND sent_at IS NULL)
   OR (status IN ('SENT', 'FAILED', 'QUARANTINED') AND locked_at IS NOT NULL);
```

필요한 constraint는 migration 규약에 따라 `NOT VALID` 추가, audit/repair, `VALIDATE CONSTRAINT` 순서로 적용합니다. 이미 동일 vocabulary constraint가 있으면 중복 생성하지 않습니다.

## 관측성 계약

### metric

```text
youtube_delivery_transition_total{rule,from,to,result}
youtube_delivery_transition_conflict_total{rule,phase}
youtube_delivery_outcome_unknown_total{transport}
youtube_delivery_quarantine_total{reason}
youtube_delivery_finalization_retry_total{result}
youtube_outbox_aggregate_transition_total{from,to}
youtube_outbox_aggregate_lag_seconds
```

Raw error, delivery ID, outbox ID, room ID를 metric label에 넣지 않습니다.

### structured log

```text
rule_id
event
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
```

Room ID와 request ID 원문은 민감도 규칙에 따라 mask/hash합니다.

## configuration validation

Dispatcher config normalization 또는 startup validation은 다음을 확인해야 합니다.

```text
MaxRetries > 0
RetryBackoff > 0
LockTimeout > 0
DeliverySendTimeout > 0
LockTimeout >= DeliverySendTimeout + max(2*PollInterval, 5s)
ReviveFreshnessWindow > 0 when revive enabled
ClaimFreshnessWindow >= ReviveFreshnessWindow + ReviveInterval
```

잘못된 production config를 조용히 default로 바꾸기보다 startup에서 명시적으로 거부하는 경계를 우선합니다. 기존 compatibility normalization을 유지해야 한다면 normalized 결과에 대해 invariant validation을 추가합니다.

## contract tests

동일한 contract suite가 policy와 store adapter에 적용되어야 합니다.

### Policy matrix

- 모든 state/event 조합의 허용 또는 명시적 거부
- retry 경계 `attempt+1 == MaxRetries`
- `AlreadySatisfied`와 `ClaimDeferred`의 attempt 불변
- outcome unknown이 decision을 만들지 않음
- `QUARANTINED`와 `SENT` terminal
- explicit time 외 전역 clock 미사용

### Store CAS

- stale version 거부
- stale `locked_at` 거부
- stale attempt 거부
- operation member 일부 conflict 시 all-or-none rollback
- conflict row의 tracking 미변경
- grouped success의 tracking 원자성
- claim/version 증가 단조성

### Crash windows

- claim 직후 crash: lease 만료 후 재claim
- preparation 중 crash: provider 호출 없음, lease 만료 후 재claim
- `SENDING` commit 후 provider 호출 전 crash: quarantine, 자동 resend 없음
- provider known failure 후 DB write 전 crash: quarantine 가능, 자동 resend 없음
- provider success 후 DB commit 전 crash: quarantine, tracking 추정 금지
- delivery commit 후 aggregate 전 crash: projector가 수렴

### Grouped operation

- member CAS 일부 실패 시 provider 미호출
- outcome unknown 후 fallback 미호출
- fallback budget 부족 row는 provider 미호출 후 retry
- success finalization member conflict 시 전체 rollback

## 완료 판정

이 계약의 구현은 다음 조건을 모두 만족해야 합니다.

1. transition policy와 repository가 `MaxRetries`를 중복 계산하지 않습니다.
2. production delivery status write가 의도별 transition store 한 경로를 통합니다.
3. provider outcome unknown에서 DB mutation과 resend가 없습니다.
4. `row_version`이 claim과 모든 mutation에서 증가합니다.
5. success tracking은 operation-level CAS transaction 안에 있습니다.
6. child가 존재하는 outbox status는 aggregate projector만 계산합니다.
7. stale legacy status writer와 ID-only success recovery 경로가 제거됩니다.
8. worker 전용 store 구현이 alarm-worker `internal` 소유권으로 이동합니다.
9. contract, integration, crash-window 테스트가 통과합니다.
10. 결정 레코드의 delivery status를 `verified`로 올릴 evidence가 저장소에 남습니다.
