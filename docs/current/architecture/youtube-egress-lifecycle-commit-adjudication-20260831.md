# YouTube egress lifecycle DB commit adjudication

- 작성일: 2026-08-31 KST
- 적용 결정: `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership`
- 규범 위치: [`youtube-egress-lifecycle-contract-20260831.md`](youtube-egress-lifecycle-contract-20260831.md)의 transaction 오류 해석 부록
- Ledger 계약: [`youtube-egress-logical-delivery-ledger-20260831.md`](youtube-egress-logical-delivery-ledger-20260831.md)

## 목적

PostgreSQL `COMMIT` 또는 connection 응답 오류는 transaction이 rollback됐다는 증거가 아닙니다. 서버가 commit한 뒤 응답이 유실되면 caller는 오류를 받지만 durable state는 이미 변경되어 있습니다.

YouTube egress에서 commit ambiguity를 일반 retry로 처리하면 다음 문제가 생깁니다.

- `BeginSending`이 commit됐는데 provider를 두 번 호출합니다.
- Provider success finalization이 commit됐는데 상태와 tracking을 다시 씁니다.
- Logical owner만 변경되고 follower projection을 누락했다고 오판합니다.
- Post tracking token이 먼저 소비된 정상 상태를 delivery conflict로 오판합니다.
- Fanout transaction을 반복해 partial child 상태를 숨깁니다.

이 문서는 operation별 exact pre/post state와 허용 가능한 DB-only retry를 고정합니다.

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
| `Applied` | Commit 성공 응답을 받았거나 primary exact read-back으로 전체 post-state를 확인했습니다. |
| `Conflict` | Required row는 존재하지만 command의 expected state/version/attempt/lock과 일치하지 않습니다. |
| `Missing` | Required row가 존재하지 않습니다. |
| `Indeterminate` | Exact pre-state, exact post-state, coherent conflict 어느 쪽인지 primary read-back으로 판정할 수 없습니다. |

`Indeterminate`를 `Conflict`, retryable DB error, provider failure로 축약하면 안 됩니다.

## 공통 규칙

### A-001. Primary exact read-back

Commit 판정은 write primary에서 수행합니다. Async replica, cache, in-memory snapshot을 근거로 사용할 수 없습니다.

### A-002. Immutable adjudication envelope

Effect 인접 command는 실행 전에 다음 envelope를 완성해야 합니다.

```text
operation ID
logical owner ID와 follower ID 집합
각 row의 expected pre-state/version/attempt/lock
각 row의 exact expected post-state/version/attempt/lock/timestamp
tracking requirement와 canonical post identity
logical ledger key와 exact expected pre/post state
provider effect started/confirmed 여부
```

Command 실행 뒤 member나 expected timestamp를 다시 계산하지 않습니다.

### A-003. Group-complete evidence

Logical group operation은 owner만 확인해서는 안 됩니다. Command가 변경하기로 한 follower projection, tracking state, logical ledger state까지 exact post-state에 포함합니다.

Outbox aggregate와 `terminal_at` projection은 terminal envelope와 별도 transaction입니다. Aggregate는 provider-adjacent commit 판정의 pre/post state에 포함하지 않습니다.

### A-004. No callback replay

Store는 transition callback, lifecycle policy, tracking planner, provider send를 자동 재실행하지 않습니다. 재시도할 수 있는 것은 immutable DB command 또는 primary read-back뿐입니다.

### A-005. No provider replay

Provider 호출이 시작된 뒤 DB 오류가 발생해도 provider를 다시 호출하지 않습니다.

### A-006. Tracking terminal idempotency

Tracking requirement가 exact claim token을 요구하더라도 primary state가 이미 sent이면 정상 terminal 상태로 수용할 수 있습니다. Token 소비와 already-sent는 둘 다 requirement 충족입니다.

### A-007. Mixed state is not partial success

Provider operation owner, logical follower projection, tracking state, ledger state 중 일부만 exact post-state이면 atomicity breach입니다. 일반 partial success나 conflict로 숨기지 않습니다.

### A-008. No inferred ownership

Claim 또는 Send fence 결과를 받지 못하면 caller가 ID와 현재 DB 값을 보고 “내 lease”라고 추정할 수 없습니다. Exact returned token만 ownership입니다.

## Claim ambiguity

`ClaimPending`은 한 statement에서 row를 선택하고 다음을 변경합니다.

```text
PENDING unlocked/stale
    -> PENDING leased
locked_at = claimAt
row_version = pre + 1
attempt 유지
```

Statement/connection 오류로 반환 row를 받지 못하면 caller는 Preparation lease를 소유하지 않습니다. Row가 실제 claim됐더라도 처리하지 않고 timeout 후 재claim에 맡깁니다.

금지:

- ID만 추정해 preparation을 시작합니다.
- Read-back으로 현재 process의 ownership을 새로 만듭니다.
- Claim callback을 자동 반복합니다.

## Logical resolution command ambiguity

Provider 호출 전 logical group command에도 exact read-back을 적용합니다.

### Follower defer

Exact post-state:

```text
PENDING
attempt 유지
lock clear
version + 1
next_attempt_at = canonical defer due
```

Exact pre-state면 같은 DB command를 retry할 수 있습니다. 다른 state면 conflict입니다.

### Fulfilled reconciliation

Command가 지정한 `PENDING/FAILED/QUARANTINED` follower 전체가 다음을 만족해야 합니다.

```text
SENT
attempt 유지
lock clear
version + 1
sent_at = canonical reconciliationAt
```

Envelope의 same-key ledger가 `SENT`인지 같은 transaction과 read-back에서 확인합니다. 일부 follower만 `SENT`거나 ledger evidence가 다르면 atomicity breach입니다. Provider call은 없습니다.

### Unresolved propagation

Command가 지정한 `PENDING/FAILED` follower 전체가 다음을 만족해야 합니다.

```text
QUARANTINED
attempt 유지
lock clear
version + 1
```

Existing quarantined owner의 attempt/version을 command가 변경하지 않았다면 read-back post-set에 포함하지 않습니다.

Envelope의 same-key ledger `QUARANTINED`를 같은 transaction과 read-back에서 확인합니다. Ledger가 `SENT`로 강화됐으면 unresolved propagation을 계속하지 않고 fulfilled reconciliation으로 전환합니다.

### Failed-owner propagation

Owner가 already `FAILED`이고 follower를 `FAILED`로 mirror하는 command는 follower attempt를 증가시키지 않습니다. Exact pre-state면 DB-only retry할 수 있습니다.

## `BeginSending` ambiguity

### Exact pre-state

Provider operation owner 전체가 다음을 만족합니다.

```text
status = PENDING
version = expected preparation version
attempt = expected attempt
locked_at = preparation lock
```

### Exact post-state

Owner 전체가 다음을 만족합니다.

```text
status = SENDING
version = pre + 1
attempt 유지
locked_at = canonical sendStartedAt
```

Follower projection은 `BeginSending`이 변경하지 않으므로 envelope의 member가 아닙니다.

### Commit 오류 후 판정

#### Confirmed committed

Owner 전체가 exact post-state입니다. `Applied`이며 immutable provider request를 한 번 호출할 수 있습니다.

#### Confirmed not committed

Owner 전체가 exact pre-state입니다. 동일 `BeginSending` DB command만 retry할 수 있습니다. Provider call은 아직 0회입니다.

#### Conflict

Owner 전체가 일관된 다른 state/version으로 이동했고 primary read-back이 성공했습니다. Provider를 호출하지 않습니다.

#### Indeterminate

Primary read-back 실패 또는 mixed pre/post state입니다. Provider를 호출하지 않습니다. Actual `SENDING` row는 stale sweeper가 처리할 수 있습니다.

## Provider success finalization ambiguity

### Finalization envelope

Success command는 다음을 포함합니다.

- Provider operation owner와 exact Send fence
- Same-logical follower projection ID와 pre-state
- Canonical `sentAt`
- Canonical post별 deduplicated tracking requirement
- `(kind, logical_id, room_id)` ledger key와 expected pre-state
- Expected latency/tracking identities

### Exact post-state

#### Owner와 follower

```text
status = SENT
version = pre + 1
attempt 유지
locked_at = NULL
sent_at = canonical sentAt
```

Follower가 reconciliation 대상이면 follower pre-state가 `PENDING/FAILED/QUARANTINED` 중 command에 기록된 값과 일치해야 합니다.

#### Tracking

Requirement별로 다음 중 하나를 만족해야 합니다.

```text
NoTracking
    -> 검사 없음

RequireClaimOrAlreadySent
    -> exact authorized_at token이 소비되고 sent 상태
       또는 primary tracking이 이미 sent

RequireAlreadySent
    -> primary tracking이 sent
```

여러 room success가 같은 token을 공유할 때 첫 transaction만 token을 소비하고 후속 transaction은 already-sent로 수용할 수 있습니다.

#### Logical ledger

각 logical key가 다음을 만족해야 합니다.

```text
status = SENT
sent_at = canonical sentAt
source_delivery_id = envelope의 owner 또는 지정된 evidence delivery
```

Existing `SENT`의 최초 `sent_at`과 source reference를 보존하는 monotonic upsert인 경우 envelope는 그 보존값을 exact post-state로 기록합니다.

### Commit 오류 후 판정

#### Confirmed committed

Owner/follower 전체, tracking requirement, ledger `SENT`가 exact post-state입니다. `Applied`입니다.

#### Confirmed not committed

Owner/follower/tracking/ledger가 command의 exact pre-state입니다. 같은 immutable DB finalization만 retry할 수 있습니다. Provider send는 재호출하지 않습니다.

Tracking pre-state는 requirement에 따라 다음을 포함합니다.

```text
RequireClaimOrAlreadySent:
    exact active token 또는 already-sent

RequireAlreadySent:
    already-sent
```

Tracking이 already-sent였던 requirement는 pre/post 양쪽에서 sent일 수 있습니다. Delivery/follower state로 transaction 적용 여부를 함께 판정합니다.

#### Tracking mismatch

다음은 atomicity breach입니다.

- Delivery owner/follower는 exact `SENT`인데 requirement가 neither token-consumed nor already-sent
- Delivery는 exact pre-state인데 이 command만 만들 수 있는 tracking transition만 commit
- Same logical group follower 일부만 `SENT`

Unsafe repair update를 실행하지 않고 `Indeterminate`로 보고합니다.

#### Ledger mismatch

다음은 atomicity breach입니다.

- Delivery/tracking은 exact post-state인데 ledger가 absent 또는 `QUARANTINED`
- Delivery는 exact pre-state인데 이 command만 만들 수 있는 ledger `SENT`만 존재
- Ledger key가 envelope의 canonical identity와 다름

Unsafe ledger 보정이나 provider 재호출을 실행하지 않고 `Indeterminate`로 보고합니다.

#### Conflict 또는 Indeterminate

Provider success는 이미 확정됐으므로 provider를 다시 호출하지 않습니다. ID-only `SENDING -> SENT` recovery를 실행하지 않습니다. Durable evidence가 부족하면 owner는 stale `SENDING`으로 남아 quarantine될 수 있습니다.

## Known-not-delivered retry ambiguity

### Exact post-state

Owner:

```text
PENDING
attempt = pre + 1
version = pre + 1
lock clear
next_attempt_at = canonical due
```

Follower projection:

```text
PENDING
attempt 유지
version = pre + 1 when command changes it
lock clear
next_attempt_at = owner due
```

### 판정

- Owner/follower exact post-state: `Applied`
- 전체 exact pre-state: 같은 DB retry command만 재시도
- Mixed owner/follower: atomicity breach
- Provider 재호출 금지
- Alarm claim release는 durable retry commit 확인 뒤 수행

## Terminal failure ambiguity

Owner:

```text
FAILED
attempt = pre + 1
version = pre + 1
lock clear
```

Follower projection:

```text
FAILED
attempt 유지
version = pre + 1 when changed
lock clear
```

Commit 확인 전 alarm claim을 release하지 않습니다. Mixed state는 atomicity breach입니다.

## Outcome unknown

Outcome unknown 경로에는 DB transition command가 없습니다. 다음만 수행합니다.

- warning audit
- bounded metric
- masked operation evidence

금지:

- state write
- tracking/claim mutation
- provider fallback/resend

Owner는 `SENDING`에 남습니다.

## Quarantine ambiguity

### Stale owner quarantine

Owner exact post-state:

```text
QUARANTINED
attempt = pre + 1
version = pre + 1
lock clear
```

Follower projection exact post-state:

```text
QUARANTINED
attempt 유지
version = pre + 1 when changed
lock clear
```

Ledger exact post-state:

```text
(kind, logical_id, room_id) = envelope key
status = QUARANTINED
quarantined_at = canonical quarantineAt
```

### 판정

- Owner/follower와 ledger 전체 exact post-state: `Applied`
- Owner/follower/ledger 전체 exact stale pre-state: same quarantine DB command retry 가능
- 일부만 changed: atomicity breach
- Ledger `SENT`가 read-back에서 나타나면 quarantine를 계속하지 않고 logical fulfilled reconciliation 후보로 넘깁니다.

Quarantined source row를 다시 attempt 증가시키지 않도록 source predicate를 유지합니다.

## Logical fulfilled reconciliation ambiguity

Same-logical ledger `SENT` evidence를 근거로 `FAILED/QUARANTINED/PENDING` follower를 `SENT`로 바꾸는 operation입니다.

- Ledger key/state/timestamp를 envelope에 기록합니다.
- Ledger `SENT` row는 변경하지 않습니다.
- Ledger가 read-back에서 더 이상 `SENT`가 아니면 불변식 위반입니다. `SENT`는 terminal이므로 일반 conflict로 숨기지 않습니다.
- Follower 일부만 `SENT`면 atomicity breach입니다.
- Provider call과 tracking mutation은 없습니다.

## Revive ambiguity

Revive는 logical group owner와 follower를 함께 reset합니다.

Exact post-state:

```text
owner/follower = PENDING
attempt = 0
version = pre + 1
lock/error clear
next_attempt_at = canonical reviveAt
```

Selection transaction은 같은 logical key의 ledger가 absent임을 확인합니다. Ledger `SENT/QUARANTINED`가 있으면 revive 대상이 아닙니다.

Commit 오류 후:

- Entire group exact post-state와 ledger absent 재확인: `Applied`
- Entire group exact pre-state: stale selected ID set을 바로 replay하지 않고 eligibility selection부터 다시 수행
- Mixed group: atomicity breach
- Ledger `SENT`/`QUARANTINED` 등장: revive conflict, no retry

Touched outbox aggregate는 revive commit 뒤 별도 projector가 계산합니다. Aggregate failure는 revive transaction의 commit 판정에 포함하지 않습니다.

## Fanout ambiguity

### `MaterializeFanout`

Exact post-state:

- Canonical target room 전체의 `(outbox_id,room_id)` child 존재
- Outbox claim lock clear
- Outbox status `PENDING`
- Outbox `terminal_at IS NULL`

Exact pre-state:

- Child 없음
- Exact active outbox claim 유지

일부 child만 존재하면 transaction atomicity breach입니다. `ON CONFLICT DO NOTHING`만으로 commit을 판정하지 않습니다.

### `CompleteWithoutTargets`

Exact post-state:

- Outbox `SENT`
- Canonical `sent_at`
- `terminal_at = canonical sent_at`
- Lock clear
- Child 없음

Child가 생겼으면 direct writer conflict이며 aggregate projector가 소유합니다.

## Aggregate ambiguity

Aggregate projector는 current child state에서 target을 다시 계산하는 idempotent SQL이어야 합니다. Commit 오류 후 stale target 값을 재사용하지 않고 projector SQL 자체를 다시 실행할 수 있습니다.

Projector는 outbox 상태 변경과 `terminal_at` 갱신을 같은 transaction에서 수행합니다. 동일 terminal 상태 replay는 기존 `terminal_at`을 보존하고, `FAILED -> SENT`는 새 terminal 시각을 기록하며, revive로 `PENDING`이 되면 `terminal_at`을 비웁니다.

Projector에는 provider effect나 transition callback이 없으므로 safe replay가 가능합니다. Delivery/tracking/ledger terminal envelope는 aggregate read-back 대상이 아닙니다.

## Cleanup ambiguity

Cleanup은 ledger가 보호할 physical terminal evidence를 삭제하므로 일반 idempotent delete로 취급하지 않습니다. Logical ledger 자체는 삭제하지 않습니다.

- 지원하는 ledger schema version과 completed backfill marker를 먼저 확인합니다.
- Delete candidate마다 non-null expected `terminal_at`, child terminal ledger evidence, same-logical nonterminal follower 부재를 transaction에서 확인합니다.
- Terminal retention envelope를 만족하는 fixed cutoff를 command에 기록합니다.
- Commit response loss 후 row가 없으면 delete committed로 볼 수 있지만, ledger/sibling guard가 같은 transaction에 있었는지 migration/query identity를 audit log에 남깁니다.
- Cleanup retry가 retention cutoff를 새로 앞당기면 안 됩니다.

## Error API

```go
type ApplyResult struct {
    Outcome     ApplyOutcome
    RuleID      RuleID
    OperationID string
    OwnerIDs    []int64
    FollowerIDs []int64
    Cause       error
}
```

- `Applied`의 `Cause`는 nil입니다.
- `Conflict`, `Missing`은 typed sentinel을 사용할 수 있습니다.
- `Indeterminate`는 original commit/read-back 오류를 보존합니다.
- Application service는 `Indeterminate`를 retryable lifecycle event로 변환하지 않습니다.

## Observability

```text
youtube_delivery_commit_adjudication_total{operation,result}
youtube_delivery_commit_indeterminate_total{operation,phase}
youtube_delivery_atomicity_breach_total{operation}
youtube_delivery_tracking_resolution_total{requirement,result}
youtube_delivery_ledger_operation_total{operation,result}
youtube_delivery_logical_projection_mismatch_total{operation}
```

Raw ID와 error 문자열을 label로 사용하지 않습니다.

Structured log:

```text
operation_id
rule_id
logical_key_hash
owner_count
follower_count
tracking_requirement_count
ledger_key_count
expected_state
expected_version_range
adjudication_result
provider_effect_started
provider_effect_confirmed
```

## Fault-injection tests

각 transaction 경계에서 다음 fault를 주입합니다.

1. Statement 전 오류
2. Statement 후 commit 전 오류
3. Server commit 후 client response loss
4. Commit response 수신 후 read-back 오류
5. Owner/follower 한 건의 concurrent mutation
6. Exact claim token이 다른 room success로 먼저 소비됨
7. Tracking write 오류
8. Ledger write 오류 또는 wrong-key write
9. Logical follower projection 일부 누락
10. Aggregate projection 오류

필수 테스트:

```text
TestBeginSendingResponseLostConfirmsBeforeProviderCall
TestBeginSendingIndeterminateNeverCallsProvider
TestCompleteSentResponseLostConfirmsOwnerFollowersTrackingAndLedger
TestCompleteSentAcceptsTrackingAlreadySentByAnotherRoom
TestCompleteSentConfirmedNonCommitRetriesDBOnly
TestCompleteSentIndeterminateNeverResends
TestCompleteSentLedgerMismatchIsBreach
TestRetryResponseLostConfirmsOwnerAndFollowerDue
TestQuarantineResponseLostConfirmsWholeLogicalGroupAndLedger
TestFulfilledReconciliationPartialFollowersIsBreach
TestFanoutResponseLostConfirmsCanonicalChildren
TestFanoutPartialChildrenIsBreach
TestReviveUnknownRestartsEligibilitySelectionAndChecksLedger
TestAggregateFailureDoesNotChangeTerminalAdjudication
TestCleanupUnknownDoesNotShortenRetentionCutoff
```

## 금지 구현

```text
if commitErr != nil { retryWholeHandler() }
if finalizationErr != nil { sender.Send(...) }
if trackingTokenConflict { failDelivery() }
UPDATE ... WHERE id IN (...) AND status='SENDING' without fence
Store가 callback/provider를 자동 반복
Read replica로 commit 여부 판정
Owner만 post-state이면 group success 처리
일부 follower만 mirror된 상태를 partial success 처리
Delivery terminal인데 ledger mismatch를 best-effort repair
Aggregate failure 때문에 provider operation rollback 또는 resend
```

## 완료 조건

1. Effect 인접 store 결과가 `Indeterminate`를 표현합니다.
2. `BeginSending` exact post-state 확인 전 provider call은 0회입니다.
3. Provider success 이후 어떤 DB 오류에서도 provider 재호출은 0회입니다.
4. Owner, follower, tracking requirement, ledger를 함께 read-back합니다.
5. 다른 room이 claim token을 먼저 소비한 already-sent tracking을 정상 terminal로 수용합니다.
6. Mixed logical projection, tracking mismatch, ledger mismatch를 atomicity breach로 분류합니다.
7. Aggregate projection은 terminal envelope와 분리되며 실패해도 provider를 재호출하지 않습니다.
8. Primary read-back fault-injection test가 통과합니다.
