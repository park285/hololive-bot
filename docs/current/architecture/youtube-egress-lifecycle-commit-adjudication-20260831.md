# YouTube egress lifecycle DB commit adjudication

작성일: 2026-08-31 KST  
적용 결정: `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership`  
규범 위치: [`youtube-egress-lifecycle-contract-20260831.md`](youtube-egress-lifecycle-contract-20260831.md)의 transaction 오류 해석 부록

## 목적

PostgreSQL `COMMIT` 또는 connection 응답 오류는 “transaction이 commit되지 않았다”는 뜻이 아닐 수 있습니다. 서버가 commit한 뒤 응답이 유실되면 caller는 오류를 받지만 durable state는 이미 변경되어 있습니다.

YouTube egress는 외부 provider 호출과 DB state를 다루므로 commit ambiguity를 일반 retry로 처리하면 다음 문제가 생깁니다.

- `BeginSending`이 실제 commit됐는데 다시 preparation 또는 send를 시작합니다.
- provider success finalization이 실제 commit됐는데 unsafe recovery가 상태를 다시 씁니다.
- fanout transaction이 실제 commit됐는데 delivery를 중복 materialize하거나 outbox를 잘못 finalization합니다.

이 문서는 각 transaction 경계에서 commit 결과를 어떻게 판정하고 무엇을 금지하는지 고정합니다.

## 판정 vocabulary

```go
type ApplyOutcome uint8

const (
    ApplyApplied ApplyOutcome = iota + 1
    ApplyConflict
    ApplyMissing
    ApplyIndeterminate
)
```

| 결과 | 의미 |
|---|---|
| `Applied` | write와 commit이 확인됐거나 read-back으로 exact post-state를 확인했습니다. |
| `Conflict` | row는 존재하지만 expected state/version/attempt/lock과 다릅니다. |
| `Missing` | row가 존재하지 않습니다. |
| `Indeterminate` | commit 여부를 판정할 primary read-back evidence가 없습니다. |

`Indeterminate`는 `Conflict`나 retryable DB error로 축약하면 안 됩니다.

## 공통 규칙

### A-001. Primary read-back

Commit ambiguity 판정은 write primary에서 읽어야 합니다. 비동기 replica나 cache를 사용해서는 안 됩니다.

### A-002. Exact expected post-state

“상태가 비슷하다”는 근거로 commit을 확정하지 않습니다. 다음 값을 operation command가 알고 있어야 합니다.

- member delivery ID 집합
- pre-state와 pre-version
- expected post-state
- expected post-version
- canonical operation timestamp
- expected attempt
- expected lock value
- success tracking identity가 있는 경우 그 identity

### A-003. No callback replay

Store adapter는 transition callback, provider send, tracking planner를 자동 재실행하지 않습니다. Retry는 immutable command의 DB 적용 또는 read-back만 반복할 수 있습니다.

### A-004. No external resend on ambiguity

Provider 호출이 시작된 뒤 DB 결과가 불명확하면 외부 send를 다시 호출하지 않습니다.

### A-005. Operation atomicity

Grouped operation은 member 전체가 exact post-state이거나 exact pre-state여야 합니다. 일부 member만 바뀐 mixed state는 정상 판정이 아니라 invariant breach입니다.

## Claim ambiguity

`ClaimPending`은 single SQL statement가 row를 선택하고 lock/version을 변경한 뒤 반환합니다.

Connection 오류로 반환 row를 받지 못하면 caller는 Preparation lease를 소유하지 않습니다. 해당 row가 실제 claim됐더라도 caller는 처리하지 않습니다. Row는 lock timeout 후 다시 claim됩니다.

금지:

- ID만 추정해 preparation을 시작합니다.
- claim 결과를 다시 읽어 현재 process 소유라고 간주합니다.

Claim identity는 SQL이 반환한 exact version과 `locked_at`을 받은 경우에만 존재합니다.

## `BeginSending` ambiguity

### 정상 command

Operation member 전체에 다음 변화를 기대합니다.

```text
PENDING + PreparationLease
    -> SENDING
row_version = expected + 1
locked_at = canonical sendStartedAt
attempt_count 유지
```

### commit 오류 후 read-back

Primary에서 operation member 전체를 한 번에 읽습니다.

#### Confirmed committed

모든 member가 다음을 만족합니다.

```text
status = SENDING
row_version = expectedVersion + 1
attempt_count = expectedAttempt
locked_at = sendStartedAt
```

이 경우 `Applied`로 판정하고 exact prepared provider request를 한 번 호출할 수 있습니다.

#### Confirmed not committed

모든 member가 다음 pre-state를 그대로 만족합니다.

```text
status = PENDING
row_version = expectedVersion
attempt_count = expectedAttempt
locked_at = preparationLockedAt
```

이 경우 동일 immutable `BeginSending` command의 DB transaction만 재시도할 수 있습니다. Provider는 아직 호출하지 않습니다.

#### Conflict

모든 member가 일관된 다른 version/state로 이동했고, read-back 자체는 성공했습니다. Provider를 호출하지 않고 operation을 중단합니다. 새로 획득한 alarm claim은 안전한 release 조건을 확인해 release합니다.

#### Indeterminate

DB를 읽을 수 없거나 member가 pre/post/mutually consistent conflict 어느 쪽에도 맞지 않습니다. Provider를 호출하지 않습니다. Row가 실제 `SENDING`이면 stale sweeper가 quarantine할 수 있으며, 그렇지 않으면 lock timeout 후 다시 claim됩니다.

## Provider success finalization ambiguity

### 정상 command

Operation member 전체에 다음 변화를 기대합니다.

```text
SENDING + SendFence
    -> SENT
row_version = expected + 1
sent_at = canonical sentAt
locked_at = NULL
attempt_count 유지
tracking transaction commit
```

### commit 오류 후 read-back

#### Confirmed committed

모든 member가 exact `SENT`, post-version, `sent_at`을 만족하고 필요한 tracking row도 success state를 만족합니다. `Applied`로 판정합니다.

#### Confirmed not committed

모든 member가 exact `SENDING + SendFence` pre-state이고 success tracking이 아직 commit되지 않았습니다. 동일 `SentOperation`의 DB finalization만 재시도할 수 있습니다. Provider send는 재호출하지 않습니다.

#### Tracking mismatch

Delivery는 `SENT`인데 required tracking이 없거나, delivery는 `SENDING`인데 tracking만 sent인 상태는 atomic transaction invariant breach입니다.

- unsafe repair update를 실행하지 않습니다.
- critical metric과 error log를 남깁니다.
- operation을 `Indeterminate`로 처리합니다.
- 별도 audited repair가 필요합니다.

#### Conflict 또는 Indeterminate

Provider 성공은 이미 확정됐으므로 외부 send를 다시 호출하지 않습니다. ID-only `SENDING -> SENT` recovery도 금지합니다. Durable evidence가 부족하면 row는 `SENDING`에 남아 quarantine될 수 있습니다.

이 선택은 delivery liveness보다 stale-state overwrite와 중복 발송 방지를 우선합니다.

## Known failure finalization ambiguity

Provider가 요청을 수락하지 않았다고 확정한 뒤 retry/FAILED transaction이 commit 오류를 반환하면 primary read-back으로 판정합니다.

### Confirmed committed

모든 member가 expected `PENDING` retry 또는 `FAILED` post-state, post-version, next attempt, attempt count를 만족합니다.

### Confirmed not committed

모든 member가 exact `SENDING + SendFence` pre-state입니다. 동일 failure command의 DB write만 재시도합니다. Provider를 다시 호출하지 않습니다.

### Conflict 또는 Indeterminate

다른 writer 또는 sweeper가 상태를 변경했을 수 있습니다. 임의 overwrite를 하지 않습니다. Alarm claim release는 durable failure commit이 확인된 경우에만 수행합니다.

## `AlreadySatisfied`와 `ClaimDeferred` ambiguity

두 operation은 provider 호출 전입니다.

### `AlreadySatisfied`

- exact `SENT`, post-version, satisfied timestamp면 committed입니다.
- exact `PENDING + PreparationLease`면 DB command를 재시도할 수 있습니다.
- 다른 상태면 conflict입니다.
- provider 호출은 어느 경우에도 없습니다.

### `ClaimDeferred`

- exact `PENDING`, post-version, lock clear, expected due면 committed입니다.
- exact leased pre-state면 command를 재시도할 수 있습니다.
- attempt count는 pre/post 모두 같아야 합니다.

## Quarantine ambiguity

Quarantine sweeper는 row lock을 가진 transaction에서 batch를 변경합니다.

Commit 오류 후:

- exact `QUARANTINED`, post-version, attempt+1, lock clear면 committed입니다.
- exact stale `SENDING` pre-state면 같은 quarantine batch를 재시도할 수 있습니다.
- mixed state면 operation별 결과를 다시 계산하고 aggregate projector는 idempotent하게 재실행합니다.

Quarantine은 provider send를 실행하지 않으므로 DB command 재시도 자체가 외부 중복을 만들지는 않습니다. 단, `QUARANTINED`를 다시 attempt 증가시키지 않도록 source state predicate를 유지합니다.

## Revive ambiguity

Revive transaction은 FAILED child reset과 aggregate projection을 포함합니다.

Commit 오류 후 primary read-back에서 다음을 확인합니다.

- reset 대상 child가 `PENDING`, attempt 0, due now, post-version인지
- 보존 대상 `SENT`와 `QUARANTINED`가 변경되지 않았는지
- outbox aggregate가 child 상태와 일치하는지

Transaction이 commit되지 않은 것이 확인되면 같은 selected ID set을 재평가하지 않고 eligibility selection부터 다시 수행합니다. Freshness와 lock 조건이 시간에 따라 바뀔 수 있기 때문입니다.

## Fanout ambiguity

### `MaterializeFanout`

Fanout transaction은 outbox claim 검증, child insert, lock clear를 포함합니다.

Commit 오류 후 primary read-back:

- canonical target 전체의 `(outbox_id, room_id)` child가 존재하고 outbox lock이 clear면 committed입니다.
- child가 없고 exact outbox claim이 유지되면 transaction을 재시도할 수 있습니다.
- child 일부만 존재하면 transaction atomicity breach입니다. 일반 retry로 숨기지 않고 critical error로 처리합니다.

`INSERT ... ON CONFLICT DO NOTHING`은 duplicate request를 idempotent하게 만들지만, expected target 집합 전체가 존재하는지 read-back해야 합니다.

### `CompleteWithoutTargets`

- outbox가 exact `SENT`, sent timestamp, lock clear이고 child가 없으면 committed입니다.
- exact claimed `PENDING`이고 child가 없으면 재시도할 수 있습니다.
- child가 생겼으면 direct finalization conflict이며 aggregate projector가 소유합니다.

## Aggregate ambiguity

Aggregate projector는 child row를 읽어 하나의 atomic update를 수행하며 반복 실행이 idempotent해야 합니다.

Commit 오류 후 별도 transition callback을 재실행하지 않고 projector SQL을 다시 호출할 수 있습니다. Projector는 current child state에서 target을 다시 계산하므로 stale 사전 계산값을 전달해서는 안 됩니다.

## Error API

Store method는 가능하면 read-back adjudication을 내부에서 수행하고 다음 결과를 반환합니다.

```go
type ApplyResult struct {
    Outcome ApplyOutcome
    RuleID  RuleID
    IDs     []int64
    Cause   error
}
```

- `Cause`는 `Applied`에서 nil입니다.
- `Conflict`와 `Missing`은 typed sentinel을 사용할 수 있습니다.
- `Indeterminate`는 원래 commit/read-back 오류를 보존합니다.
- Application service는 `Indeterminate`를 retryable transition refusal로 변환하지 않습니다.

## 관측성

```text
youtube_delivery_commit_adjudication_total{operation,result}
youtube_delivery_commit_indeterminate_total{operation,phase}
youtube_delivery_atomicity_breach_total{operation}
youtube_delivery_tracking_mismatch_total{operation}
```

`operation`은 bounded enum입니다. ID와 raw error는 label에 넣지 않습니다.

Structured log에는 다음을 포함합니다.

```text
operation_id
rule_id
member_count
expected_state
expected_version_range
adjudication_result
provider_effect_started
provider_effect_confirmed
```

## fault-injection tests

각 transaction 경계에 다음 fault를 주입합니다.

1. statement 전 오류
2. statement 후 commit 전 오류
3. server commit 후 client response loss
4. commit 결과 수신 후 read-back 오류
5. operation member 한 건의 외부 concurrent mutation
6. tracking write 오류

필수 테스트:

```text
TestBeginSendingCommitResponseLostConfirmsByReadBackBeforeSend
TestBeginSendingIndeterminateNeverCallsProvider
TestCompleteSentCommitResponseLostConfirmsTracking
TestCompleteSentConfirmedNotCommittedRetriesDBOnly
TestCompleteSentIndeterminateNeverResends
TestFanoutCommitResponseLostConfirmsCanonicalChildren
TestFanoutPartialChildrenIsAtomicityBreach
TestReviveCommitUnknownRestartsEligibilitySelection
```

## 금지 구현

```text
if commitErr != nil { retryWholeHandler() }
if sentWriteErr != nil { sender.Send(...) }
if markSentErr != nil { UPDATE ... WHERE id IN (...) AND status='SENDING' }
context 안에 transition callback을 넣고 store가 자동 반복
read replica로 commit 여부 판정
일부 grouped member만 post-state이면 성공 처리
```

## 완료 조건

- 모든 effect-adjacent transaction이 `Indeterminate`를 표현합니다.
- `BeginSending` ambiguity가 해결되기 전 provider call은 0회입니다.
- provider success 이후 어떤 DB 오류에서도 provider 재호출은 0회입니다.
- operation member와 tracking의 mixed commit을 critical invariant breach로 분류합니다.
- primary read-back fault-injection test가 통과합니다.
