# YouTube egress lifecycle 구현 계획

작성일: 2026-08-31 KST  
**Decisions:** `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership`  
아키텍처 정본: [`../architecture/youtube-egress-lifecycle-transition-ownership-20260831.md`](../architecture/youtube-egress-lifecycle-transition-ownership-20260831.md)  
규범 계약: [`../architecture/youtube-egress-lifecycle-contract-20260831.md`](../architecture/youtube-egress-lifecycle-contract-20260831.md)  
Commit 판정: [`../architecture/youtube-egress-lifecycle-commit-adjudication-20260831.md`](../architecture/youtube-egress-lifecycle-commit-adjudication-20260831.md)  
직접 구현 근거: [`../architecture/youtube-egress-lifecycle-library-review-20260831.md`](../architecture/youtube-egress-lifecycle-library-review-20260831.md)

## 목적

현재 YouTube egress를 typed lifecycle policy, deterministic logical owner, version-fenced transition store로 교체합니다. 구현자는 이 문서의 PR 순서와 acceptance를 따르며, 같은 row를 old/new writer가 함께 수정하는 dual-write 기간을 만들지 않습니다.

이번 작업은 다음 경계를 함께 고정합니다.

1. Physical delivery row와 logical delivery를 분리합니다.
2. Logical group의 provider/attempt owner를 하나로 고정합니다.
3. Provider 호출 전 preparation과 `SENDING` 이후 outcome을 분리합니다.
4. Post-level tracking token을 room delivery success와 idempotent하게 결합합니다.
5. DB commit ambiguity를 `Indeterminate`로 처리합니다.
6. Worker 전용 persistence를 alarm-worker `internal`로 회수합니다.

## 목표 흐름

```text
ClaimPending
    -> PreparationLease
    -> logical key 생성
    -> logical group resolve / owner 선출
    -> follower state projection
    -> post-level tracking requirement 결정
    -> immutable provider operation
    -> operation-level BeginSending
    -> commit adjudication
    -> provider call
    -> typed outcome
    -> logical group finalization
    -> tracking/aggregate transaction
```

Outbox writer:

```text
pre-fanout: OutboxFanoutService
post-fanout: atomic aggregate projector
```

## 작업 원칙

1. 각 PR은 독립적으로 build/test 가능해야 합니다.
2. Schema는 writer cutover 전에 additive하게 배포합니다.
3. Inactive code는 허용하지만 old/new writer dual write는 금지합니다.
4. Retry 알고리즘과 제품 의미를 함께 변경하지 않습니다.
5. Outcome unknown의 hold/quarantine 의미를 유지합니다.
6. Grouped send와 tracking behavior를 먼저 characterization합니다.
7. Package 이동은 `git mv`를 우선합니다.
8. Generated index/snapshot은 canonical tool로 갱신합니다.
9. Commit 오류를 non-commit으로 단정하지 않습니다.
10. Provider call 이후 DB retry는 provider send를 재실행하지 않습니다.
11. Community/Shorts canonical resolver는 하나입니다.
12. Post-level cache는 logical group resolution을 생략하지 않습니다.
13. Follower는 독립 attempt budget을 사용하지 않습니다.
14. Terminal delivery는 dedupe evidence이므로 cleanup retention을 보호합니다.
15. Alarm-worker replica는 1을 유지합니다.

## Phase 0. Characterization과 위험 지도

### 목표

보존할 안전 속성을 테스트로 고정하고 제거할 위험을 명시합니다.

### 유지할 동작

- Stale `locked_at` token은 500 microsecond 차이도 거부합니다.
- `SENDING` row는 primary claim이 다시 가져가지 않습니다.
- Outcome unknown은 failure/success bucket에 들어가지 않습니다.
- Stale `SENDING`은 `QUARANTINED`, outbox는 `FAILED`로 수렴합니다.
- `SENT` row는 stale failure writer가 덮어쓰지 않습니다.
- Delivery success와 tracking은 같은 transaction입니다.
- Grouped outcome unknown 뒤 fallback하지 않습니다.
- Claim defer는 attempt를 소비하지 않습니다.
- 이미 받은 room은 provider 호출 없이 terminal 처리합니다.
- Revive는 `SENT`/`QUARANTINED`를 보존합니다.

### 제거할 위험

- ID-only `SENDING -> SENT` recovery
- Reason 문자열과 SQL `CASE`에 분산된 retry 정책
- Cached `Proceed`의 room-level gate 우회
- Same logical delivery의 follower가 새 attempt budget으로 owner를 추월하는 경로
- `SENT`만 보는 sibling gate
- 첫 room success가 tracking token을 소비한 뒤 후속 room success를 conflict로 볼 수 있는 경계
- Terminal cleanup가 same-room dedupe evidence를 조기에 삭제할 가능성

### 추가 test

```text
TestOutcomeUnknownDoesNotReleaseAlarmClaim
TestGroupedOutcomeUnknownDoesNotFallback
TestAlreadySatisfiedDoesNotInvokeProvider
TestClaimDeferredDoesNotConsumeAttempt
TestMultipleRoomSuccessUsesOnePostClaim
TestLaterRoomSuccessAcceptsAlreadySentTracking
```

Unsafe recovery를 정당화하는 characterization test는 추가하지 않습니다.

### 검증

```bash
go test ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
go test ./hololive/hololive-shared/pkg/service/youtube/outbox/store/...
go test ./hololive/hololive-shared/pkg/service/youtube/tracking/observation/...
```

### 완료 조건

- Provider call count와 tracking mutation count를 단언합니다.
- Flaky sleep 대신 injected clock/channel/DB state를 사용합니다.
- 제거 대상 call site가 후속 PR에 연결됩니다.

## PR 1. Additive `row_version` schema

### 목표

Writer cutover 전에 `youtube_notification_delivery.row_version`을 추가하고 scanner/fixture를 갱신합니다.

### Migration

현재 manifest 마지막 파일은 `189_youtube_job_lease_failure_diagnostics_reconcile.sql`입니다.

```text
hololive/hololive-api/scripts/migrations/190_youtube_delivery_row_version.sql
manifest sequence: 051
```

병렬 branch가 번호를 사용하면 merge 시점의 `max+1`로 재번호합니다.

```sql
ALTER TABLE youtube_notification_delivery
    ADD COLUMN IF NOT EXISTS row_version bigint NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
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

Constant default를 사용하고 version index는 만들지 않습니다.

### 수정 범위

```text
hololive/hololive-api/scripts/migrations/manifest.txt
hololive/hololive-api/scripts/migrations/<new>.sql
hololive/hololive-shared/pkg/domain/youtube_delivery.go
hololive/hololive-shared/pkg/service/youtube/outbox/deliverysql/pgx.go
hololive/hololive-shared/pkg/service/youtube/outbox/store/queries/*.sql
hololive/hololive-dbtest/testdata/schema_snapshot.golden.sql
fixtures/tests
```

### 검증

```bash
bash scripts/architecture/check-migration-manifest.sh
SCHEMA_SNAPSHOT_UPDATE=1 go test -run TestSchemaSnapshotGolden ./hololive/hololive-dbtest
go test -run TestSchemaSnapshotGolden ./hololive/hololive-dbtest
go test ./hololive/hololive-shared/pkg/domain/...
go test ./hololive/hololive-shared/pkg/service/youtube/outbox/...
```

### Rollback

Binary만 rollback하고 additive column은 유지합니다.

### 완료 조건

- Fresh bootstrap/existing migration이 성공합니다.
- 모든 scanner가 version을 보존합니다.
- Snapshot에 column/constraint가 있습니다.
- Version index가 없습니다.

## PR 2. Typed vocabulary, logical owner, pure policy

### 목표

DB write를 바꾸지 않고 lifecycle state/event/failure/logical group/tracking requirement를 internal pure package에 추가합니다.

### Package

```text
hololive/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle/
├── status.go
├── event.go
├── failure.go
├── logical_delivery.go
├── logical_group.go
├── tracking_requirement.go
├── snapshot.go
├── decision.go
├── delivery_policy.go
├── retry_policy.go
├── revive_policy.go
└── *_test.go
```

### Logical key

```text
Community/Shorts: kind + canonical_post_id + room_id
기타 kind:       outbox_id + room_id
```

Raw key를 log/metric에 노출하지 않습니다.

### Group resolution

```text
SENT evidence       -> Fulfilled
QUARANTINED evidence-> Unresolved
SENDING evidence    -> InFlight
그 외 최소(created_at,id) owner:
  PENDING -> Active
  FAILED  -> Failed
```

`SENT`는 `QUARANTINED`보다 우선합니다. `QUARANTINED + SENDING` 또는 다중 `SENDING`은 invariant breach로 별도 결과를 반환합니다.

### Tracking requirement

```text
NoTracking
RequireClaimOrAlreadySent
RequireAlreadySent
```

첫 room이 exact token을 소비하고 후속 room은 already-sent를 수용할 수 있어야 합니다.

### Decision

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

### Policy API

```go
type DeliveryPlanner interface {
    Plan(context.Context, DeliverySnapshot, DeliveryEvent, DeliveryPolicy) (DeliveryDecision, error)
}
```

Policy는 DB, pgx, slog, provider SDK, `time.Now()`에 의존하지 않습니다.

### Test

```text
TestLogicalKeyCommunityUsesCanonicalPostAndRoom
TestLogicalKeyNonPostUsesOutboxAndRoom
TestLogicalGroupSentWinsOverQuarantine
TestLogicalGroupRejectsQuarantineAndSendingMixedState
TestLogicalGroupSelectsDeterministicOwner
TestFollowerCannotConsumeAttempt
TestRetryBoundary
TestTerminalAndReconciliationTransitions
TestTrackingRequirementClaimOrAlreadySent
TestPlannerUsesExplicitTimeOnly
```

### 검증

```bash
go test ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle/...
go test -race ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle/...
```

### 완료 조건

- Canonical resolver가 하나입니다.
- Group owner와 follower attempt 규칙이 pure test로 고정됩니다.
- Tracking already-sent가 typed requirement입니다.
- Outcome unknown은 transition decision이 아닙니다.

## PR 3. Typed preparation coordinator와 provider outcome

### 목표

Raw failure bucket을 제거하고, provider 호출 전에 logical group resolution과 tracking requirement를 확정합니다. Writer는 아직 legacy adapter를 사용할 수 있습니다.

### Coordinator flow

```text
claimed physical rows
-> outbox/payload load
-> logical key batch 계산
-> logical group rows batch 조회
-> resolution priority 적용
-> owner/follower partition
-> follower projection decision
-> owner post-level claim/tracking requirement
-> rendering/route/request ID
-> PreparedSendOperation
```

### Owner/follower semantics

- Same batch와 retained sibling 전체에서 deterministic owner를 찾습니다.
- Owner가 batch 밖의 `PENDING`이면 current follower를 owner due에 맞춰 defer합니다.
- Owner가 `FAILED`이면 current follower를 attempt 없이 `FAILED`로 mirror합니다.
- `SENDING` evidence는 defer합니다.
- `QUARANTINED` evidence는 unresolved propagation입니다.
- `SENT` evidence는 failed/quarantined follower도 provider 없이 `SENT`로 reconcile합니다.

### Query bound

- Community/Shorts는 kind, room, relevant status를 SQL에서 제한합니다.
- Current batch logical key를 한 번에 조회하고 N+1을 피합니다.
- `LogicalGroupScanLimit` 초과는 fail-closed이며 provider call은 0회입니다.
- `EXPLAIN (ANALYZE, BUFFERS)` evidence 없이 새 index를 추가하지 않습니다.

### Typed outcome

```text
Delivered
KnownNotDeliveredRetryable
KnownNotDeliveredPermanent
OutcomeUnknown
```

Timeout/connection reset은 provider 계약이 반대로 증명하지 않는 한 unknown입니다.

### Grouped send

- Operation 안 logical key는 중복될 수 없습니다.
- Unknown에서 fallback 금지
- Known-not-accepted + explicit fallback permission에서만 individual fallback
- Fallback 자체는 attempt를 소비하지 않음

### 제거 대상

```text
DispatchResult.FailureBuckets
DispatchResult.FailureRetryAfter
deliveryFailureReasonIsPermanent
reason 문자열 metric bucket
```

### Test

```text
TestSameBatchSelectsOneLogicalOwner
TestOwnerOutsideBatchDefersFollower
TestFailedOwnerMirrorsFollowerFailed
TestProceedCacheHitStillResolvesLogicalGroup
TestSentEvidenceReconcilesQuarantinedFollower
TestQuarantinedEvidenceBlocksProvider
TestLogicalGroupOverflowFailsClosed
TestGroupedOperationRejectsDuplicateLogicalKeys
TestGroupedUnknownNeverFallsBack
TestTimeoutIsOutcomeUnknown
```

### 검증

```bash
go test ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
go test -race ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
```

### 완료 조건

- Raw reason map이 production code에 없습니다.
- Follower가 독립 provider/attempt owner가 되지 않습니다.
- Post cache hit가 logical resolution을 생략하지 않습니다.
- Outcome unknown이 writer/claim release/fallback을 호출하지 않습니다.

## PR 4. Internal transition store와 group-level CAS

### 목표

Worker 전용 store를 internal로 이동하고 logical group query/transition, tracking-idempotent success, commit adjudication을 구현합니다.

### Package 이동

```text
FROM hololive/hololive-shared/pkg/service/youtube/outbox/store
TO   hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store
```

`git mv`와 import cutover를 같은 PR에서 수행합니다.

### Worker-owned DTO

```text
DeliveryRow
OutboxRow
LogicalGroupRow
TrackingState
```

Final cleanup에서 production consumer가 alarm-worker 하나뿐이면 `domain.YouTubeNotificationDelivery`를 삭제합니다.

### Store API

```text
ClaimPending
ResolveLogicalGroups
BeginSending
ReconcileFulfilled
PropagateUnresolved
DeferFollowers
ScheduleLogicalRetry
FailLogicalGroup
CompleteSent
QuarantineStaleSending
ReviveFailedLogicalGroups
UpdateOutboxAggregateStatuses
```

Generic patch/status writer는 금지합니다.

### Group transition

- Retry: owner attempt +1, follower attempt 유지/due 정렬
- Terminal failure: owner attempt +1, follower `FAILED` mirror
- Quarantine: stale owner attempt +1, follower unresolved mirror
- Success: owner/follower 모두 `SENT`, follower attempt 유지
- Fulfilled reconciliation: provider/tracking mutation 없이 followers `SENT`

Command가 변경하기로 한 owner/follower는 all-or-none입니다.

### Tracking transaction

`CompleteSent` requirement별 처리:

```text
NoTracking
    -> delivery/group만 commit

RequireClaimOrAlreadySent
    -> exact token으로 mark sent 시도
       0 rows면 primary tracking already-sent 확인
       neither면 rollback

RequireAlreadySent
    -> primary tracking sent 확인
```

Same post requirement를 transaction 안에서 deduplicate합니다.

### Commit adjudication

- `Applied`, `Conflict`, `Missing`, `Indeterminate`
- Owner/follower/tracking exact read-back
- Begin post-state 확인 전 provider call 권한 없음
- Exact pre-state에서만 DB-only retry
- Mixed projection/tracking은 atomicity breach
- Provider callback 자동 retry 금지

### Unsafe recovery

ID-only `SENDING -> SENT` API를 새 store에 만들지 않습니다.

### Test

```text
TestClaimIncrementsVersion
TestResolveLogicalGroupUsesCanonicalIdentity
TestResolveLogicalGroupStatusPriority
TestRetryUpdatesOwnerAndFollowerAtomically
TestFailedOwnerMirrorsFollowersWithoutAttempt
TestQuarantineUpdatesWholeGroup
TestFulfilledReconciliationMovesFailedAndQuarantinedFollowers
TestBeginSendingAllOrNone
TestBeginResponseLossConfirmsBeforeProviderCall
TestBeginIndeterminateNeverCallsProvider
TestCompleteSentConsumesFirstClaimToken
TestCompleteSentAcceptsAlreadySentForLaterRoom
TestCompleteSentRollsBackWhenTrackingNeitherClaimNorSent
TestCompleteSentResponseLossConfirmsOwnerFollowersTracking
TestCompleteSentConfirmedNonCommitRetriesDBOnly
TestCompleteSentIndeterminateNeverResends
TestFanoutPartialChildrenIsBreach
```

### 검증

```bash
go test ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/...
go test -race ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/...
RUN_INTEGRATION_TESTS=true go test -tags=integration \
  ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/...
./scripts/architecture/check-repository-ownership.sh
```

### 완료 조건

- Store production importer는 alarm-worker internal뿐입니다.
- 모든 mutation이 version을 검사/증가시킵니다.
- Repository가 `MaxRetries`를 받지 않습니다.
- Group/tracking all-or-none와 commit ambiguity test가 통과합니다.

## PR 5. Runtime writer cutover

### 목표

Active dispatcher write path를 새 coordinator/policy/store로 한 번에 교체합니다.

### 목표 순서

```text
ClaimPending
-> outbox/payload load
-> logical group resolve
-> owner/follower projection
-> tracking requirement
-> immutable request
-> BeginSending
-> begin adjudication
-> provider call
-> typed outcome
-> logical group finalization
-> success tracking/aggregate
-> finalization adjudication
```

### Tracking runtime rule

- `Proceed` claim owner: `RequireClaimOrAlreadySent`
- `AlreadySent` post지만 room 미전달: `RequireAlreadySent`
- Non-post: `NoTracking`
- 첫 room success 이후 후속 room success는 already-sent로 정상 commit합니다.

### Commit rule

- Begin `Applied`만 provider call 허용
- Exact pre-state면 DB-only retry
- `Conflict/Missing/Indeterminate` provider call 0회
- Success finalization exact pre-state면 DB-only retry
- Provider는 다시 호출하지 않음
- Mixed group/tracking은 critical alert, unsafe recovery 금지

### 제거할 call site

```text
MarkSendingBatchIfLocked direct call
MarkFailedRetryBatchIfLocked direct call
MarkPermanentFailureBatchIfLocked direct call
failure bucket finalizer
recoverSuccessfulCommunityShortsSentState
markRecoveredSentDeliveryRows
```

### Config validation

```text
LogicalGroupScanLimit > 0
SendingFinalizeGrace = max(2*PollInterval, 5s)
LockTimeout >= DeliverySendTimeout + SendingFinalizeGrace
CleanupSafetyMargin = max(outboxCleanupLoopInterval, 2*AggregateSyncInterval)
CleanupAfter >= ClaimFreshnessWindow + ReviveFreshnessWindow + CleanupSafetyMargin
```

기존 default는 `CleanupAfter=7d`, `ClaimFreshnessWindow=2h`, `ReviveFreshnessWindow=1h`이므로 충분하지만 startup validation으로 회귀를 차단합니다.

### Test

```text
TestSameLogicalGroupCallsProviderOnlyForOwner
TestFollowerCannotUseFreshAttemptBudget
TestProceedCacheHitCannotBypassGroupResolution
TestFirstRoomSuccessConsumesClaim
TestLaterRoomSuccessAcceptsAlreadySent
TestBeginConflictOrIndeterminateCallsProviderZeroTimes
TestSuccessResponseLossCallsProviderOnce
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

```sql
SELECT count(*) AS sending_count,
       min(locked_at) AS oldest_sending,
       max(locked_at) AS newest_sending
FROM youtube_notification_delivery
WHERE status = 'SENDING';
```

0이 아니면 기존 sweeper/drain을 완료한 뒤 교체합니다.

Logical duplicate audit는 canonical resolver를 사용하는 one-shot tool/test fixture로 실행합니다. SQL의 raw `content_id` equality만으로 완료 판정하지 않습니다.

### Deployment

- Alarm-worker single-instance replacement
- Old/new egress owner 동시 실행 금지
- Migration 적용 확인
- Conflict/indeterminate, logical resolution, tracking resolution, quarantine, aggregate lag 관측

### Immediate rollback trigger

- Provider success finalization conflict/`Indeterminate`
- Same logical group provider duplicate
- Follower attempt 증가
- Tracking neither-claim-nor-sent mismatch
- `SENT`/tracking 불일치
- Group mixed state
- Terminal active lock
- Aggregate 불수렴

Additive column은 rollback하지 않습니다.

### 완료 조건

- Active writer가 새 transition service 하나입니다.
- Logical owner만 provider/attempt 권한을 가집니다.
- Tracking token 소비가 multi-room success를 깨지 않습니다.
- Provider 재호출 없는 adjudication이 검증됩니다.

## PR 6. Outbox writer와 logical-group revive 정렬

### 목표

Pre-fanout direct writer, post-fanout aggregate, logical group revive를 정렬합니다.

### OutboxFanoutService

```text
CompleteWithoutTargets
RecordFanoutFailure
MaterializeFanout
```

Active outbox claim을 검사합니다.

### MaterializeFanout

한 transaction에서:

```text
outbox claim 확인
-> room canonicalize/dedupe
-> child insert
-> outbox lock clear
-> commit
```

Commit response loss는 canonical child 전체와 lock clear를 read-back합니다. 일부 child만 존재하면 atomicity breach입니다.

### Direct writer guard

Child가 있으면 pre-fanout writer는 conflict이고 aggregate projector가 소유합니다.

### Logical group revive

허용 조건:

- Same-logical `SENT/QUARANTINED` evidence 없음
- Deterministic owner `FAILED`
- 관련 outbox never-sent/fresh
- Active group lock 없음

적용:

- Owner/follower `PENDING`
- Attempt 0
- Due now/owner aligned
- Lock/error clear
- Version 증가
- Touched outbox aggregate projector 실행

Commit `Indeterminate`면 eligibility selection부터 다시 수행합니다.

### Test

```text
TestMaterializeFanoutIsAtomic
TestFanoutResponseLossConfirmsCanonicalChildren
TestFanoutPartialChildrenIsBreach
TestCompleteWithoutTargetsRejectsChild
TestReviveResetsOwnerAndFollowers
TestReviveRejectsSentOrQuarantinedGroup
TestReviveUsesAggregateProjector
TestReviveUnknownRestartsSelection
```

### 검증

```bash
go test ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
RUN_INTEGRATION_TESTS=true go test -tags=integration \
  ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
```

### 완료 조건

- Child outbox direct writer가 없습니다.
- Fanout insert/lock-clear가 한 transaction입니다.
- Revive가 logical group과 aggregate를 함께 수렴시킵니다.

## PR 7. Cleanup retention, observability, architecture gate, legacy 제거

### 목표

Logical dedupe evidence retention과 우회 writer 방지를 완결합니다.

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

기존 status vocabulary constraint는 중복 생성하지 않습니다.

### Cleanup retention

Cleanup query는 다음을 확인합니다.

- Same-logical nonterminal row가 있으면 terminal evidence 삭제 금지
- `CleanupAfter`가 config freshness envelope보다 큼
- Cutoff retry가 더 최신 방향으로 이동하지 않음

```text
CleanupSafetyMargin = max(outboxCleanupLoopInterval, 2*AggregateSyncInterval)
CleanupAfter >= ClaimFreshnessWindow + ReviveFreshnessWindow + CleanupSafetyMargin
```

### 제거 대상

```text
legacy status writer
failure reason helper
ID-only success recovery
shared store 잔여 구현
unused SQL
status alias facade
중복 canonical resolver
```

### Architecture gate

- Internal store 밖 status update 금지
- Repository `MaxRetries` 금지
- Failure bucket/reason parser 재도입 금지
- `SENT` source transition 금지
- Alarm-worker 밖 store import 금지
- ID-only sent recovery 금지
- Provider callback automatic retry 금지
- Post cache로 logical resolution 생략 금지
- Follower attempt 증가 금지
- Canonical resolver 복제 금지
- Cleanup에서 nonterminal sibling guard 제거 금지

### Metrics

```text
youtube_delivery_transition_total
youtube_delivery_logical_group_total
youtube_delivery_logical_follower_total
youtube_delivery_tracking_resolution_total
youtube_delivery_outcome_unknown_total
youtube_delivery_commit_adjudication_total
youtube_delivery_commit_indeterminate_total
youtube_delivery_atomicity_breach_total
youtube_delivery_quarantine_total
youtube_outbox_aggregate_transition_total
youtube_outbox_aggregate_lag_seconds
```

### Decision evidence

```text
planned -> in_progress -> implemented -> verified
```

Implementation/evidence path를 record에 추가하고 INDEX는 canonical renderer로 생성합니다.

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

- Gate가 우회 writer, cache bypass, follower attempt, cleanup regression을 차단합니다.
- Shared에는 cross-runtime 또는 진성 다중 소비자만 남습니다.
- Dashboard에서 group/tracking/commit/aggregate 상태를 관측할 수 있습니다.
- Decision record가 `verified`입니다.

## 운영 검증

### 배포 전

- Migration ledger와 invalid-state audit 확인
- `SENDING` drain/quarantine
- Replica=1 확인
- Old/new egress 동시 실행 없음
- Logical duplicate/owner 상태 audit
- Cleanup freshness envelope 확인

### 배포 직후

- Provider success conflict/`Indeterminate` 0
- Same-logical provider duplicate 0
- Follower attempt 증가 0
- Tracking mismatch 0
- Group mixed state 0
- Aggregate/quarantine baseline 수렴

### 장기 확인

- Quarantine backlog와 resolution 확인
- Retry exhausted/permanent code 분리
- `SENDING` oldest age 확인
- Revive flap 없음
- Terminal evidence retention 확인
- `Indeterminate` 반복 시 DB/network 장애 조사

## 최종 acceptance

1. Delivery `row_version`이 schema/row model에 있습니다.
2. Claim과 모든 mutation이 version을 증가시킵니다.
3. Lifecycle policy는 alarm-worker internal pure package입니다.
4. Canonical logical resolver가 하나입니다.
5. Logical group의 provider/attempt owner가 하나입니다.
6. Follower가 독립 retry budget을 사용하지 않습니다.
7. `PENDING/FAILED/SENDING/QUARANTINED/SENT` sibling을 규범 priority로 해석합니다.
8. Post cache hit도 logical resolution을 수행합니다.
9. Outcome unknown은 write/release/fallback/resend를 하지 않습니다.
10. Begin/finalization은 operation member all-or-none입니다.
11. Tracking exact token과 already-sent가 multi-room success를 idempotent하게 수용합니다.
12. Begin adjudication 전 provider call은 0회입니다.
13. Provider success 후 DB 오류에서 provider 재호출은 0회입니다.
14. Store가 `Applied/Conflict/Missing/Indeterminate`를 구분합니다.
15. ID-only success recovery가 없습니다.
16. Child outbox status는 aggregate projector만 계산합니다.
17. Logical group revive가 owner/follower를 함께 reset합니다.
18. Cleanup가 terminal evidence를 freshness envelope보다 오래 보존합니다.
19. Worker 전용 store/row model이 alarm-worker internal에 있습니다.
20. Legacy writer/SQL이 제거되고 architecture gate가 회귀를 차단합니다.
21. Contract, logical-owner, tracking, integration, crash, commit fault-injection, race test가 통과합니다.
22. Decision record가 `verified`입니다.

## 비목표

- Alarm-worker replica 확대
- Provider reconciliation API
- Automatic quarantine replay
- Persisted logical key/receipt ledger
- Outbox `row_version`
- Retry algorithm 변경
- Event sourcing
- 외부 workflow engine
- 범용 사내 FSM framework
