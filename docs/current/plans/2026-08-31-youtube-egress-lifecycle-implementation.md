# YouTube egress lifecycle 구현 계획

작성일: 2026-08-31 KST  
**Decisions:** `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership`  
아키텍처 정본: [`../architecture/youtube-egress-lifecycle-transition-ownership-20260831.md`](../architecture/youtube-egress-lifecycle-transition-ownership-20260831.md)  
규범 계약: [`../architecture/youtube-egress-lifecycle-contract-20260831.md`](../architecture/youtube-egress-lifecycle-contract-20260831.md)  
Commit 판정: [`../architecture/youtube-egress-lifecycle-commit-adjudication-20260831.md`](../architecture/youtube-egress-lifecycle-commit-adjudication-20260831.md)  
직접 구현 근거: [`../architecture/youtube-egress-lifecycle-library-review-20260831.md`](../architecture/youtube-egress-lifecycle-library-review-20260831.md)

## 목적

현재 YouTube egress의 상태 변경 경로를 typed lifecycle policy와 version-fenced transition store로 교체합니다. 구현자는 이 문서의 PR 순서와 acceptance를 따르며, 같은 row를 legacy writer와 새 writer가 동시에 변경하는 dual-write 기간을 만들지 않습니다.

이번 작업은 단순한 `status` enum 정리가 아닙니다. 다음 네 경계를 함께 확정합니다.

1. Physical delivery row와 사용자 관점의 logical delivery를 분리합니다.
2. Provider 호출 전 preparation과 `SENDING` 이후 outcome을 분리합니다.
3. DB commit 응답 불명확성을 `Indeterminate`로 처리합니다.
4. Worker 전용 persistence 구현을 alarm-worker `internal`로 회수합니다.

## 목표 상태

```text
ClaimPending
    -> PreparationLease
    -> logical-delivery coalescing / durable sibling gate
    -> typed preparation result
    -> pure lifecycle policy
    -> intent-specific transition command
    -> operation-level BeginSending
    -> provider call
    -> typed provider outcome
    -> operation-level finalization
    -> PostgreSQL exact CAS / tracking transaction / aggregate projection
```

Outbox writer는 다음과 같이 분리합니다.

```text
pre-fanout intent: OutboxFanoutService
post-fanout status: atomic aggregate projector
```

## 작업 원칙

1. 각 PR은 독립적으로 build/test 가능해야 합니다.
2. Schema는 writer cutover 전에 additive하게 배포합니다.
3. Inactive code는 허용하지만 같은 상태의 old/new dual write는 금지합니다.
4. Retry 알고리즘과 제품 동작은 책임 분리 과정에서 변경하지 않습니다.
5. Outcome unknown의 hold/quarantine 의미를 유지합니다.
6. Grouped send와 Community/Shorts alarm-once 동작은 먼저 characterization합니다.
7. Worker 전용 package 이동은 `git mv`를 우선합니다.
8. Generated decision index와 schema snapshot은 canonical tool로 갱신합니다.
9. Commit 오류를 non-commit으로 단정하지 않고 primary exact read-back으로 판정합니다.
10. Provider call 이후 어떤 DB retry도 provider send를 자동 재실행하지 않습니다.
11. Community/Shorts logical identity는 `(kind, canonical_post_id, room_id)` 하나를 모든 계층이 공유합니다.
12. Post-level decision cache는 room-level logical-delivery gate를 생략하는 권한이 아닙니다.
13. Logical key와 raw room/request ID는 metric label에 넣지 않습니다.
14. Alarm-worker replica는 이 작업 전체에서 1을 유지합니다.

## Phase 0. 현재 동작 characterization과 위험 지도

### 목표

현재 안전 속성과 의도된 동작을 테스트로 고정하고, 리팩터링에서 제거할 unsafe recovery와 logical-dedupe gap을 명시합니다.

### 대상

```text
hololive/hololive-alarm-worker/internal/egress/youtubedispatch/
hololive/hololive-shared/pkg/service/youtube/outbox/store/
hololive/hololive-shared/pkg/service/youtube/tracking/observation/
```

### 유지할 동작

- stale `locked_at` token은 500 microsecond 차이도 거부합니다.
- `SENDING` row는 primary claim이 다시 가져가지 않습니다.
- Outcome unknown은 failure/success bucket에 들어가지 않습니다.
- Stale `SENDING`은 `QUARANTINED`, outbox는 `FAILED`로 수렴합니다.
- `SENT` row는 stale failure writer가 덮어쓰지 않습니다.
- Delivery success와 Community/Shorts tracking은 같은 transaction입니다.
- Grouped outcome unknown 뒤 individual fallback을 실행하지 않습니다.
- Claim defer는 attempt를 소비하지 않습니다.
- 이미 받은 room은 provider 호출 없이 terminal 처리합니다.
- Revive는 `FAILED` child만 reset하고 `SENT`/`QUARANTINED`를 보존합니다.
- All-quarantined outbox는 revive하지 않습니다.

### 제거할 현재 위험

- `recoverSuccessfulCommunityShortsSentState`의 ID-only `SENDING -> SENT` recovery
- Failure reason 문자열과 SQL `CASE`에 분산된 retry/permanent 정책
- Post-level cached `Proceed`가 room-level sibling resolution을 건너뛸 수 있는 구조
- 같은 batch의 동일 post/room row가 별도 provider operation이 될 수 있는 구조
- Per-room sibling gate가 `SENT`만 보고 `SENDING`/`QUARANTINED`를 보지 않는 구조

### 추가할 characterization test

```text
TestOutcomeUnknownDoesNotReleaseAlarmClaim
TestGroupedOutcomeUnknownDoesNotFallback
TestAlreadySatisfiedDoesNotInvokeProvider
TestClaimDeferredDoesNotConsumeAttempt
TestStaleSendingQuarantinePreservesUnknownOutcomeMeaning
```

Unsafe recovery를 정당화하는 characterization test는 추가하지 않습니다. PR 5에서 recovery 제거와 DB-only finalization adjudication test를 함께 도입합니다.

### 검증

```bash
go test ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
go test ./hololive/hololive-shared/pkg/service/youtube/outbox/store/...
go test ./hololive/hololive-shared/pkg/service/youtube/tracking/observation/...
```

### 완료 조건

- 유지할 behavior가 테스트 이름으로 발견 가능합니다.
- Outcome unknown과 grouped fallback 테스트가 provider 호출 횟수를 단언합니다.
- `time.Sleep` 기반 race test 대신 injected clock, channel, DB state를 사용합니다.
- 제거 대상 함수와 SQL call site가 PR 5/7 작업표에 연결됩니다.

## PR 1. Additive delivery fencing schema

### 목표

Runtime behavior를 바꾸기 전에 `youtube_notification_delivery.row_version`을 추가하고 모든 delivery scanner가 읽을 수 있게 합니다.

### Migration

현재 manifest의 마지막 migration은 `189_youtube_job_lease_failure_diagnostics_reconcile.sql`입니다. 제안 파일은 다음입니다.

```text
hololive/hololive-api/scripts/migrations/190_youtube_delivery_row_version.sql
```

병렬 branch가 번호를 먼저 사용하면 merge 시점의 `max+1`로 재번호합니다. Manifest sequence는 현재 `050` 다음인 `051`을 사용합니다.

```sql
ALTER TABLE youtube_notification_delivery
    ADD COLUMN IF NOT EXISTS row_version bigint NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'chk_youtube_notification_delivery_row_version_nonnegative'
    ) THEN
        ALTER TABLE youtube_notification_delivery
            ADD CONSTRAINT chk_youtube_notification_delivery_row_version_nonnegative
            CHECK (row_version >= 0) NOT VALID;
    END IF;
END
$$;

ALTER TABLE youtube_notification_delivery
    VALIDATE CONSTRAINT chk_youtube_notification_delivery_row_version_nonnegative;
```

Constant default를 사용하고 `row_version` index를 만들지 않습니다.

### 수정 파일

```text
hololive/hololive-api/scripts/migrations/manifest.txt
hololive/hololive-api/scripts/migrations/<new>_youtube_delivery_row_version.sql
hololive/hololive-shared/pkg/domain/youtube_delivery.go
hololive/hololive-shared/pkg/service/youtube/outbox/deliverysql/pgx.go
hololive/hololive-shared/pkg/service/youtube/outbox/store/queries/*.sql
hololive/hololive-dbtest/testdata/schema_snapshot.golden.sql
관련 fixtures/tests
```

모든 `SELECT`/`RETURNING` column order를 scanner와 대조합니다.

```bash
rg -n "RETURNING .*attempt_count|SELECT .*attempt_count" \
  hololive/hololive-shared/pkg/service/youtube/outbox \
  hololive/hololive-alarm-worker/internal/egress/youtubedispatch
```

### Runtime behavior

- 기존 writer가 계속 active입니다.
- 새 CAS는 아직 production에 연결하지 않습니다.
- 기존 writer가 version을 증가시키지 않아도 이 단계에서는 허용합니다.

### 검증

```bash
bash scripts/architecture/check-migration-manifest.sh
SCHEMA_SNAPSHOT_UPDATE=1 go test -run TestSchemaSnapshotGolden ./hololive/hololive-dbtest
go test -run TestSchemaSnapshotGolden ./hololive/hololive-dbtest
go test ./hololive/hololive-shared/pkg/domain/...
go test ./hololive/hololive-shared/pkg/service/youtube/outbox/...
```

### Rollback

Binary만 rollback하고 additive column은 유지합니다. Production column을 drop하지 않습니다.

### 완료 조건

- Fresh bootstrap과 existing DB migration이 성공합니다.
- 모든 scanner와 fixture가 version을 보존합니다.
- Golden snapshot에 column과 constraint가 있습니다.
- `row_version` index가 없습니다.

## PR 2. Typed lifecycle vocabulary, logical identity, pure policy

### 목표

DB write를 바꾸지 않고 상태·이벤트·logical identity·failure·decision policy를 alarm-worker internal에 추가합니다.

### 새 package

```text
hololive/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle/
├── status.go
├── event.go
├── failure.go
├── logical_delivery.go
├── snapshot.go
├── decision.go
├── delivery_policy.go
├── outbox_policy.go
├── retry_policy.go
├── revive_policy.go
└── *_test.go
```

### Logical identity

```go
type LogicalDeliveryKey struct {
    Kind            DeliveryKind
    CanonicalPostID string
    RoomID          string
    OutboxID        int64
}
```

Constructor는 kind에 따라 다음 identity를 만듭니다.

```text
Community/Shorts: kind + canonical_post_id + room_id
기타 kind:       outbox_id + room_id
```

Community/Shorts canonical post ID는 telemetry/claim/sibling query와 같은 resolver를 사용합니다. Empty 또는 ambiguous identity는 전송 진행이 아니라 typed preparation failure입니다.

`String()` 또는 log method가 raw room ID를 반환하지 않게 하고, 관측에는 hash를 사용합니다.

### Event vocabulary

```text
AlreadySatisfied
ClaimDeferred
DuplicateFollowerDeferred
EquivalentDeliveryInFlight
EquivalentDeliveryUnresolved
PreparationRetryableFailure
PreparationPermanentFailure
BeginSend
Delivered
KnownNotDeliveredRetryable
KnownNotDeliveredPermanent
SendingLeaseExpired
ReviveFailed
```

`OutcomeUnknown`은 immediate transition event가 아니라 no-write application disposition입니다.

### Decision vocabulary

```text
BeginSendDecision
SentDecision
AlreadySatisfiedDecision
DeferDecision
RetryDecision
FailDecision
QuarantinePropagationDecision
```

Concrete constructor는 invalid field combination을 만들 수 없게 합니다.

### Policy API

```go
type DeliveryPlanner interface {
    Plan(
        context.Context,
        DeliverySnapshot,
        DeliveryEvent,
        DeliveryPolicy,
    ) (DeliveryDecision, error)
}
```

Policy는 DB, pgx, slog, provider SDK, `time.Now()`를 사용하지 않습니다.

### Adapter

- Existing DB row status를 lifecycle status로 explicit 변환합니다.
- Unknown status는 오류입니다.
- Existing post resolver를 lifecycle logical-key constructor에서 재사용하거나 한 semantic owner로 이동합니다.

### Contract tests

```text
TestLogicalDeliveryKeyCommunityUsesCanonicalPostAndRoom
TestLogicalDeliveryKeyNonPostUsesOutboxAndRoom
TestLogicalDeliveryKeyRejectsEmptyCanonicalPost
TestPlannerHandlesEveryStateEventPair
TestPlannerRetryBoundary
TestPlannerTerminalStates
TestPlannerDuplicateFollowerDoesNotConsumeAttempt
TestPlannerEquivalentInFlightDoesNotConsumeAttempt
TestPlannerEquivalentUnknownPropagatesQuarantineWithoutAttempt
TestPlannerUsesExplicitTimeOnly
```

### Runtime behavior

기존 dispatcher/writer는 그대로입니다. 새 policy는 production DB write에 연결하지 않습니다.

### 검증

```bash
go test ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle/...
go test -race ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle/...
```

### Rollback

Inactive package만 제거합니다.

### 완료 조건

- Policy가 순수합니다.
- Community/Shorts logical identity resolver가 하나입니다.
- Outcome unknown이 transition decision을 만들지 않습니다.
- Terminal, attempt, logical-sibling 규칙이 DB 없는 contract test로 고정됩니다.

## PR 3. Typed preparation coordinator와 provider outcome

### 목표

Raw failure bucket을 제거하고, provider 호출 전에 batch coalescing과 durable sibling gate를 수행하는 typed preparation coordinator를 도입합니다. DB status writer는 아직 legacy adapter를 사용할 수 있습니다.

### 새 타입

```go
type PreparationResultKind uint8

type ProviderOutcomeKind uint8

type PreparedCandidate struct {
    Delivery      ClaimedDelivery
    LogicalKey    lifecycle.LogicalDeliveryKey
    Preparation   PreparationLease
}

type DeliveryOutcome struct {
    DeliveryID  int64
    OutboxID    int64
    OperationID string
    LogicalKey  lifecycle.LogicalDeliveryKey
    Kind        ProviderOutcomeKind
    FailureCode lifecycle.FailureCode
    Message     string
    RetryAfter  time.Duration
    AlarmClaims []AlarmClaimToken
}
```

### 제거 대상

```text
DispatchResult.FailureBuckets
DispatchResult.FailureRetryAfter
deliveryFailureReasonIsPermanent
reason 문자열 기반 metric bucket
```

### Batch coalescing

Claimed candidate를 logical key로 묶습니다.

- Leader는 `(created_at, delivery_id)` 오름차순 첫 row입니다.
- Follower는 provider request에 들어가지 않습니다.
- Follower는 `DuplicateFollowerDeferred`로 attempt 없이 due를 이동합니다.
- 다른 room은 다른 logical key이므로 독립적으로 처리합니다.

```text
claimed rows
-> logical key 생성
-> deterministic leader/follower partition
-> follower defer decisions
-> leader만 durable sibling gate와 alarm claim 수행
```

### Durable sibling gate

Leader마다 same logical delivery sibling을 조회합니다.

```text
SENT        -> AlreadySatisfied
SENDING     -> EquivalentDeliveryInFlight, no-attempt defer
QUARANTINED -> EquivalentDeliveryUnresolved, no-attempt quarantine propagation
없음/FAILED/PENDING -> normal alarm claim/preparation
```

`SENT`와 `QUARANTINED`가 함께 있으면 `SENT`가 우선합니다.

Post-level decision cache가 `Proceed`를 반환한 cache hit여도 room-level sibling gate를 다시 실행합니다. Cache는 post-level claim 계산만 재사용합니다.

### Query 성능

Sibling query는 다음 조건을 만족해야 합니다.

- `room_id`, relevant status, kind를 먼저 제한합니다.
- Candidate outbox의 payload/content를 existing canonical resolver로 비교합니다.
- Unbounded full-table scan을 허용하지 않습니다.
- `EXPLAIN (ANALYZE, BUFFERS)` evidence 없이 새 expression index를 추가하지 않습니다.

### Transport classifier

SDK/HTTP outcome을 다음으로 분류합니다.

```text
Delivered
KnownNotDeliveredRetryable
KnownNotDeliveredPermanent
OutcomeUnknown
```

Timeout과 connection reset은 provider 계약이 반대로 증명하지 않는 한 `OutcomeUnknown`입니다.

### Grouped send

- Group member logical key는 모두 달라야 합니다.
- Outcome unknown이면 fallback하지 않습니다.
- Known-not-accepted + `fallback_allowed=true`일 때만 individual fallback을 허용합니다.
- Fallback 자체는 attempt를 소비하지 않습니다.

### Legacy writer bridge

Typed result를 legacy method로 변환해야 한다면 application service 한 파일에만 둡니다. Repository는 raw provider error를 받지 않습니다.

### 테스트

```text
TestSameBatchSamePostRoomSelectsOneLeader
TestSameBatchSamePostDifferentRoomsRemainIndependent
TestProceedCacheHitStillRunsRoomSiblingGate
TestSentSiblingBecomesAlreadySatisfied
TestSendingSiblingDefersWithoutAttempt
TestQuarantinedSiblingPropagatesQuarantineWithoutAttempt
TestSentSiblingWinsOverQuarantinedSibling
TestGroupedOperationRejectsDuplicateLogicalKeys
TestGroupedOutcomeUnknownDoesNotFallback
TestTransportTimeoutIsOutcomeUnknown
```

### 검증

```bash
go test ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
go test -race ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
```

### Rollback

Typed coordinator/outcome commit을 revert합니다. DB schema 변화는 없습니다.

### 완료 조건

- Production code에 failure reason map이 없습니다.
- Same batch logical delivery의 provider candidate가 하나입니다.
- `Proceed` cache hit가 room-level gate를 생략하지 않습니다.
- `SENDING`/`QUARANTINED` sibling이 same-room resend를 차단합니다.
- Outcome unknown은 writer, claim release, fallback을 호출하지 않습니다.

## PR 4. Internal transition store, sibling query, operation-level CAS

### 목표

Worker 전용 store를 alarm-worker internal로 이동하고, logical sibling 조회와 version-aware intent-specific command를 구현합니다. Production writer cutover 전 contract/integration/fault-injection test를 통과해야 합니다.

### Package 이동

```text
FROM hololive/hololive-shared/pkg/service/youtube/outbox/store
TO   hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store
```

`git mv`와 import cutover를 같은 PR에서 수행합니다. 다른 production runtime importer가 없는지 `go list`와 ownership gate로 확인합니다.

### Worker-owned row model

Internal store는 `DeliveryRow`, `OutboxRow`, `LogicalSibling` 또는 동등한 DTO를 소유합니다. Final cleanup에서 production consumer가 alarm-worker 하나뿐이면 `domain.YouTubeNotificationDelivery`를 삭제합니다. Cross-runtime outbox intent 타입은 별도 검토 없이 옮기지 않습니다.

### Store API

```text
ClaimPending
FindLogicalDeliverySiblings
BeginSending
CompleteAlreadySatisfied
DeferClaim
PropagateQuarantine
ScheduleRetryBatch
FailBatch
CompleteSent
QuarantineStaleSending
ReviveFailedOutboxes
UpdateOutboxAggregateStatuses
```

Generic `UpdateStatus` 또는 nullable patch DSL은 만들지 않습니다.

### Sibling query contract

- Current row와 current batch leader/follower ID를 제외합니다.
- Community/Shorts는 kind, room, candidate status로 DB candidate를 제한하고 canonical resolver로 post ID를 비교합니다.
- Status priority는 `SENT > SENDING > QUARANTINED`입니다.
- Query result는 current snapshot 이후 stale해질 수 있으므로 후속 mutation도 current row fencing을 검사합니다.
- Replica>1의 same-logical-key race를 해결했다고 주장하지 않습니다.

### Version CAS

Claim과 모든 mutation에서 `row_version=row_version+1`을 적용합니다. Expected predicate는 status, version, attempt, `locked_at`을 포함합니다.

`PropagateQuarantine`은 다음 semantics입니다.

```text
PENDING leased -> QUARANTINED
attempt_count 유지
locked_at = NULL
row_version + 1
source sibling evidence를 audit reason으로 보존
```

### Operation atomicity

`BeginSending`, grouped success, grouped known failure는 exact operation member 전체를 transaction에서 검사합니다. 한 member라도 conflict/missing이면 전체 rollback합니다.

### Commit adjudication

Store는 `Applied`, `Conflict`, `Missing`, `Indeterminate`를 구분합니다.

- Begin commit 오류는 primary exact read-back 완료 전 provider 호출 권한을 만들지 않습니다.
- Exact pre-state에서만 같은 immutable DB command를 retry합니다.
- Mixed member는 atomicity breach입니다.
- Store는 transition callback을 자동 반복하지 않습니다.

### Success transaction

`CompleteSent`는 delivery member와 exact alarm claim token, tracking, latency classification을 같은 transaction에서 commit합니다.

### Unsafe recovery

ID-only `SENDING -> SENT` API를 새 store에 구현하지 않습니다. Legacy recovery는 PR 5에서 제거합니다.

### Integration/fault-injection tests

```text
TestClaimPendingIncrementsVersion
TestFindLogicalSiblingUsesCanonicalPostIdentity
TestFindLogicalSiblingStatusPriority
TestPropagateQuarantineDoesNotConsumeAttempt
TestBeginSendingRejectsStalePreparationLease
TestBeginSendingRollsBackWholeOperationOnOneConflict
TestBeginSendingCommitResponseLostConfirmsBeforeSend
TestBeginSendingIndeterminateNeverCallsProvider
TestCompleteSentRollsBackOnAlarmClaimConflict
TestCompleteSentPersistsTrackingForCommittedMembers
TestCompleteSentCommitResponseLostConfirmsTracking
TestCompleteSentConfirmedNotCommittedRetriesDBOnly
TestCompleteSentIndeterminateNeverResends
TestScheduleRetryDoesNotReevaluateMaxRetries
TestQuarantineRejectsFreshSending
TestReviveUsesAggregateProjectorForChildOutbox
TestFanoutCommitResponseLostConfirmsCanonicalChildren
TestFanoutPartialChildrenIsAtomicityBreach
```

### 검증

```bash
go test ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/...
go test -race ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/...
RUN_INTEGRATION_TESTS=true go test -tags=integration \
  ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/...
./scripts/architecture/check-repository-ownership.sh
```

### Rollback

Package/import move를 revert합니다. Additive schema는 유지합니다.

### 완료 조건

- Store의 production importer는 alarm-worker internal뿐입니다.
- Logical sibling query가 canonical identity와 status priority를 지킵니다.
- 모든 mutation이 version을 검사하고 증가시킵니다.
- Repository method가 `MaxRetries`를 받지 않습니다.
- Grouped partial CAS와 commit ambiguity test가 통과합니다.

## PR 5. Runtime writer cutover와 preparation-before-SENDING

### 목표

Dispatcher의 active write path를 새 coordinator, lifecycle policy, transition store로 한 번에 교체합니다.

### 기존 순서

```text
FetchAndLock
-> MarkSendingBatchIfLocked
-> outbox load / format / alarm claim
-> provider send
-> legacy finalizer
```

### 목표 순서

```text
ClaimPending -> PreparationLease
-> outbox load / validation
-> logical key 생성
-> in-batch leader/follower coalescing
-> durable SENT/SENDING/QUARANTINED sibling gate
-> post-level alarm claim
-> rendering / route / ClientRequestID 확정
-> PreparedSendOperation 생성
-> operation-level BeginSending
-> BeginSending commit adjudication
-> provider call
-> typed outcome
-> policy decision
-> operation-level finalization
-> finalization commit adjudication
```

### 주요 변경 파일

```text
claim_manager_pipeline.go
claim_manager_gate.go
claim_manager_acquire.go
send_engine*.go
metrics_recorder*.go
delivery_transition_service.go
preparation_coordinator.go
Dispatcher wiring/tests
```

### Logical coordinator

- Coalescing은 post-level claim 획득 전에 수행합니다.
- Same logical key follower는 attempt 없이 defer합니다.
- Cache hit `Proceed`도 room-level sibling gate를 실행합니다.
- `SENT` sibling은 provider 없이 current row를 `SENT`로 처리합니다.
- `SENDING` sibling은 attempt 없이 defer합니다.
- `QUARANTINED` sibling은 provider 없이 current row를 `QUARANTINED`로 전이합니다.

### BeginSending adjudication

- `Applied`: exact Send fence로 provider를 한 번 호출합니다.
- Exact pre-state: 동일 DB command만 retry합니다.
- `Conflict`/`Missing`: provider를 호출하지 않습니다.
- `Indeterminate`: provider를 호출하지 않고 critical evidence를 기록합니다.

### Provider success finalization

- Exact post-state와 tracking: 성공으로 수렴합니다.
- Exact `SENDING + SendFence` pre-state: 같은 DB finalization만 bounded retry합니다.
- Conflict, tracking mismatch, `Indeterminate`: provider 재호출과 ID-only recovery를 금지합니다.
- Durable result가 확인되지 않으면 row는 quarantine될 수 있습니다.

### Send lease budget

```text
SendingFinalizeGrace = max(2*PollInterval, 5s)
LockTimeout >= DeliverySendTimeout + SendingFinalizeGrace
```

Provider call 전 remaining lease budget을 검사합니다.

### 제거할 active call site

```text
MarkSendingBatchIfLocked direct call
MarkFailedRetryBatchIfLocked direct call
MarkPermanentFailureBatchIfLocked direct call
markDispatchResult failure bucket loop
recoverSuccessfulCommunityShortsSentState
markRecoveredSentDeliveryRows
```

Compatibility wrapper가 필요하면 test-only 또는 unexported inactive adapter로 제한합니다.

### 테스트

```text
TestSameLogicalDeliveryBatchCallsProviderOnce
TestProceedCacheHitCannotBypassRoomGate
TestSendingSiblingPreventsProviderCallWithoutAttempt
TestQuarantinedSiblingPreventsProviderCallAndPropagatesState
TestPreparationFailureNeverCreatesSending
TestBeginSendingConflictNeverCallsProvider
TestBeginSendingIndeterminateNeverCallsProvider
TestSuccessFinalizationNonCommitRetriesDBOnly
TestSuccessFinalizationResponseLossCallsProviderOnce
TestSuccessConflictNeverCallsUnsafeRecovery
TestOutcomeUnknownWritesNothingAndKeepsClaim
TestGroupedBeginAndFinalizeAreAllOrNone
```

### 검증

```bash
go test ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
go test -race ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
RUN_INTEGRATION_TESTS=true go test -tags=integration \
  ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
go test ./hololive/hololive-shared/pkg/service/youtube/tracking/observation/...
```

### Rollout preflight

Cutover 전에 `SENDING`을 모두 drain 또는 quarantine합니다.

```sql
SELECT count(*) AS sending_count,
       min(locked_at) AS oldest_sending,
       max(locked_at) AS newest_sending
FROM youtube_notification_delivery
WHERE status = 'SENDING';
```

0이 아니면 교체하지 않습니다.

추가 audit:

```sql
SELECT o.kind, d.room_id, count(*)
FROM youtube_notification_delivery d
JOIN youtube_notification_outbox o ON o.id = d.outbox_id
WHERE o.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
  AND d.status IN ('SENDING', 'QUARANTINED')
GROUP BY o.kind, d.room_id
HAVING count(*) > 1;
```

이 query는 canonical post identity를 완전히 표현하지 않으므로 탐색용입니다. 실제 동일-post 여부는 migration 전 audit tool에서 canonical resolver로 판정합니다.

### Deployment

- Alarm-worker 단일 인스턴스 replacement입니다.
- Old/new egress owner를 동시에 실행하지 않습니다.
- Migration 적용을 확인합니다.
- 배포 직후 finalization conflict/indeterminate, logical coalesce, sibling gate, `SENDING` age, quarantine을 확인합니다.

### Immediate rollback trigger

- Provider success finalization conflict 또는 `Indeterminate` 발생
- Same logical key의 provider call 중복
- `SENT`와 tracking 불일치
- Terminal row의 active lock
- Child aggregate 불수렴
- Grouped member mixed state
- Sibling gate가 `SENT`보다 `QUARANTINED`를 우선하는 관측

Additive `row_version`은 rollback하지 않습니다.

### 완료 조건

- Production writer는 새 transition service 하나입니다.
- Preparation과 provider-effect phase가 분리됩니다.
- Same logical delivery의 same-batch send leader가 하나입니다.
- Durable `SENDING`/`QUARANTINED` sibling이 resend를 차단합니다.
- Provider 재호출 없는 commit adjudication이 검증됩니다.

## PR 6. Outbox fanout writer 경계와 revive 정렬

### 목표

Pre-fanout direct writer와 post-fanout aggregate writer를 물리적으로 분리합니다.

### OutboxFanoutService

```text
CompleteWithoutTargets
RecordFanoutFailure
MaterializeFanout
```

모든 operation은 active outbox claim token을 검사합니다.

### MaterializeFanout transaction

```text
outbox claim 검증
-> target room canonicalize/dedupe
-> delivery INSERT ... ON CONFLICT (outbox_id, room_id) DO NOTHING
-> outbox lock clear
-> commit
```

Commit response loss는 canonical target 전체의 child와 lock clear를 primary read-back합니다. Child 일부만 존재하면 atomicity breach입니다.

### Direct writer guard

Pre-fanout finalization은 transaction 안에서 `NOT EXISTS child delivery`를 검사합니다. Child가 있으면 conflict이고 aggregate projector가 소유합니다.

### Revive

- Child가 있으면 `FAILED`만 reset합니다.
- `SENT`/`QUARANTINED`는 보존합니다.
- 같은 transaction에서 표준 aggregate projector를 실행합니다.
- Child가 없을 때만 outbox를 직접 reset합니다.
- All-quarantined outbox를 제외합니다.
- Commit `Indeterminate`면 stale ID set을 반복하지 않고 eligibility selection부터 다시 수행합니다.

Logical duplicate로 quarantine가 전파된 child도 revive 대상이 아닙니다.

### 테스트

```text
TestMaterializeFanoutIsAtomic
TestMaterializeFanoutCommitResponseLostConfirmsCanonicalChildren
TestMaterializeFanoutPartialChildrenIsAtomicityBreach
TestCompleteWithoutTargetsRejectsExistingChild
TestFanoutFailureRejectsExistingChild
TestReviveChildOutboxUsesAggregateProjection
TestReviveDoesNotResetQuarantinedChild
TestReviveDoesNotResetPropagatedQuarantine
TestReviveCommitUnknownRestartsEligibilitySelection
TestConcurrentFanoutAndDirectFinalizeCannotBothCommit
```

### 검증

```bash
go test ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
RUN_INTEGRATION_TESTS=true go test -tags=integration \
  ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
```

### Rollback

이전 writer-cutover binary로 rollback합니다. Delivery schema는 유지합니다.

### 완료 조건

- Child outbox direct writer call site가 없습니다.
- Fanout materialization과 lock clear가 한 transaction입니다.
- Revive가 aggregate 계산을 복제하지 않습니다.
- Propagated quarantine가 자동 revive되지 않습니다.

## PR 7. Constraints, observability, architecture gate, legacy cleanup

### 목표

우회 writer와 stale shared implementation을 제거하고 운영 검증을 완결합니다.

### DB audit

```sql
SELECT id, status, attempt_count, locked_at, sent_at
FROM youtube_notification_delivery
WHERE status NOT IN ('PENDING', 'SENDING', 'SENT', 'FAILED', 'QUARANTINED')
   OR attempt_count < 0
   OR (status = 'SENDING' AND locked_at IS NULL)
   OR (status = 'SENT' AND sent_at IS NULL)
   OR (status IN ('SENT', 'FAILED', 'QUARANTINED') AND locked_at IS NOT NULL);
```

기존 status vocabulary constraint는 중복 생성하지 않습니다. 새 state-shape constraint가 필요하면 `NOT VALID`, audit/repair, `VALIDATE` 순서를 따릅니다.

### 제거 대상

```text
legacy MarkSent/MarkFailed/MarkFailedRetry methods
failure reason bucket helper
ID-only success recovery
shared store package 잔여 implementation
unused SQL files
status alias facade
중복 canonical post resolver
```

`domain.YouTubeNotificationDelivery`, `deliverysql` 등 후보의 production consumer를 다시 검색합니다. Alarm-worker 하나뿐이면 internal row/helper로 이동합니다. 남기는 shared symbol에는 실제 다중 소비 근거를 ownership 문서에 기록합니다.

### Architecture gate

- 허용된 internal store 밖의 delivery status update 금지
- Repository API의 `MaxRetries` 금지
- `FailureBuckets`, `deliveryFailureReasonIsPermanent` 재도입 금지
- Primary claim/revive의 `QUARANTINED` 포함 금지
- `SENT` source transition 금지
- Alarm-worker 밖 transition store import 금지
- ID-only `SENDING -> SENT` SQL 금지
- Store callback의 provider send/automatic retry 금지
- Post-level cache result만으로 room-level gate를 생략하는 call path 금지
- Community/Shorts canonical post resolver 복제 금지

SQL fixture와 migration은 좁은 allowlist로 구분합니다.

### Metrics

```text
youtube_delivery_transition_total
youtube_delivery_transition_conflict_total
youtube_delivery_logical_coalesce_total
youtube_delivery_sibling_gate_total
youtube_delivery_outcome_unknown_total
youtube_delivery_quarantine_total
youtube_delivery_finalization_retry_total
youtube_delivery_commit_adjudication_total
youtube_delivery_commit_indeterminate_total
youtube_delivery_atomicity_breach_total
youtube_delivery_tracking_mismatch_total
youtube_outbox_aggregate_transition_total
youtube_outbox_aggregate_lag_seconds
```

Raw IDs, logical key, 오류 문자열은 label로 사용하지 않습니다.

### Decision evidence

```text
planned -> in_progress -> implemented -> verified
```

`implementation`에는 package/migration/gate, `evidence`에는 contract/integration/logical-dedupe/crash/fault-injection test 경로를 기록합니다. `INDEX.md`는 iris-stack canonical renderer로 생성합니다.

### 전체 검증

```bash
./scripts/architecture/check-repository-ownership.sh
./scripts/architecture/ci-boundary-gate.sh
bash scripts/architecture/check-migration-manifest.sh
./build-all.sh --no-bump

go build ./ \
  ../shared-go/... \
  ../iris-client-go/... \
  ./admin-dashboard/backend/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-api/... \
  ./hololive/hololive-alarm-worker/... \
  ./hololive/hololive-youtube-collector/...

go test ./ \
  ../shared-go/... \
  ../iris-client-go/... \
  ./admin-dashboard/backend/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-api/... \
  ./hololive/hololive-alarm-worker/... \
  ./hololive/hololive-youtube-collector/...

(cd hololive/hololive-youtube-collector/youtubejs && npm test)

RUN_INTEGRATION_TESTS=true go test -tags=integration \
  ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...

./scripts/ci/pre-push-gate.sh
```

### 완료 조건

- Architecture gate가 우회 writer와 cache-bypass 회귀를 차단합니다.
- Shared에는 cross-runtime 또는 진성 다중 소비자만 남습니다.
- Decision record가 `verified`이고 evidence가 존재합니다.
- Dashboard에서 logical coalesce, sibling gate, unknown, quarantine, conflict, `Indeterminate`, atomicity breach, aggregate lag를 확인할 수 있습니다.

## 운영 검증

### 배포 전

- Row-version migration이 ledger에 있습니다.
- Invalid-state audit가 0건입니다.
- `SENDING`이 모두 drain/quarantine됐습니다.
- Alarm-worker replica가 1입니다.
- Old/new egress 동시 실행 계획이 없습니다.
- Logical duplicate audit 결과와 처리 결정을 기록합니다.

### 배포 직후

- Provider success finalization conflict/`Indeterminate`는 0입니다.
- Same logical key provider duplicate는 0입니다.
- Logical coalesce follower와 sibling gate metric이 예상 범위입니다.
- `SENT`/tracking이 일치합니다.
- Grouped mixed-state와 tracking mismatch는 0입니다.
- Aggregate lag와 quarantine rate가 baseline 범위입니다.

### 장기 확인

- `QUARANTINED` backlog를 운영자가 확인할 수 있습니다.
- Retry exhausted와 permanent failure가 stable code로 분리됩니다.
- `SENDING` oldest age가 `LockTimeout`을 지속 초과하지 않습니다.
- All-quarantined outbox가 revive flap을 만들지 않습니다.
- `Indeterminate` 반복은 DB/network 장애로 조사합니다.

## 최종 acceptance

1. `youtube_notification_delivery.row_version`이 production schema와 row model에 있습니다.
2. Claim과 모든 delivery mutation이 version을 증가시킵니다.
3. Transition policy는 alarm-worker internal pure package입니다.
4. Community/Shorts canonical logical identity resolver가 하나입니다.
5. Same logical key의 same-batch send leader는 하나입니다.
6. Post-level cache hit도 room-level sibling gate를 실행합니다.
7. Durable `SENDING`/`QUARANTINED` sibling이 same-room resend를 차단합니다.
8. Failure policy가 raw 문자열과 SQL `CASE`에 분산되지 않습니다.
9. Outcome unknown은 DB write, claim release, fallback, resend를 하지 않습니다.
10. Provider operation begin/finalization은 all-or-none입니다.
11. Begin commit 판정 전 provider call은 0회입니다.
12. Provider success 후 모든 DB 오류에서 provider 재호출은 0회입니다.
13. Store가 `Applied`, `Conflict`, `Missing`, `Indeterminate`를 구분합니다.
14. ID-only success recovery가 없습니다.
15. Child outbox status는 aggregate projector만 계산합니다.
16. Worker 전용 store와 row model이 alarm-worker internal에 있습니다.
17. Legacy writer/SQL이 제거되고 architecture gate가 재도입을 차단합니다.
18. Contract, logical-dedupe, integration, crash-window, commit fault-injection, race test가 통과합니다.
19. Decision record가 `verified`로 갱신됩니다.

## 비목표

- alarm-worker replica 확대
- provider reconciliation API
- `QUARANTINED` 자동 replay
- outbox `row_version`
- error-code/state-changed-at 신규 schema
- retry exponential backoff 변경
- event sourcing
- 외부 workflow engine
- 범용 사내 FSM framework
