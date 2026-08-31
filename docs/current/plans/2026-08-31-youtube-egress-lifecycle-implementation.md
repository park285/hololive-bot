# YouTube egress lifecycle 구현 계획

작성일: 2026-08-31 KST  
**Decisions:** `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership`  
아키텍처 정본: [`../architecture/youtube-egress-lifecycle-transition-ownership-20260831.md`](../architecture/youtube-egress-lifecycle-transition-ownership-20260831.md)  
규범 계약: [`../architecture/youtube-egress-lifecycle-contract-20260831.md`](../architecture/youtube-egress-lifecycle-contract-20260831.md)

## 목적

이 계획은 현재 YouTube egress 상태 변경 경로를 typed lifecycle policy와 version-fenced transition store로 교체합니다. 구현자는 이 문서의 PR 경계와 acceptance를 순서대로 수행합니다. 같은 row를 old/new writer가 동시에 변경하는 dual-write 기간은 만들지 않습니다.

## 완료 상태

이 계획은 다음 상태를 목표로 합니다.

```text
SendEngine
    -> typed preparation result / provider outcome
    -> lifecycle policy
    -> intent-specific transition command
    -> alarm-worker internal transition store
    -> PostgreSQL state/version CAS
```

Outbox는 다음 writer 경계를 가집니다.

```text
pre-fanout: OutboxFanoutService
post-fanout: atomic aggregate projector
```

## 작업 원칙

1. 각 PR은 독립적으로 build/test 가능해야 합니다.
2. schema는 writer cutover 전에 additive하게 배포합니다.
3. inactive code를 추가하는 것은 허용하지만, 같은 상태를 두 경로가 함께 쓰는 것은 금지합니다.
4. retry 알고리즘과 product semantics는 책임 분리 과정에서 변경하지 않습니다.
5. provider outcome unknown의 hold/quarantine 의미를 바꾸지 않습니다.
6. grouped send와 community/shorts alarm-once 동작은 characterization test로 먼저 고정합니다.
7. worker 전용 store 이동은 `git mv`를 우선하여 history를 보존합니다.
8. generated decision index와 schema snapshot은 해당 canonical tool로 갱신합니다.

## Phase 0. 현재 동작 characterization

### 목표

리팩터링 전에 현재 안전 속성과 의도된 동작을 테스트로 고정합니다. 이미 있는 테스트는 재사용하고 빠진 crash/operation 경계만 추가합니다.

### 대상

```text
hololive/hololive-alarm-worker/internal/egress/youtubedispatch/
hololive/hololive-shared/pkg/service/youtube/outbox/store/
```

### 반드시 고정할 동작

- stale `locked_at` token은 500 microsecond 차이도 거부합니다.
- `SENDING` row는 primary claim이 다시 가져가지 않습니다.
- timeout/outcome unknown은 failure bucket과 success bucket에 들어가지 않습니다.
- stale `SENDING`은 `QUARANTINED`가 되고 aggregate는 `FAILED`로 수렴합니다.
- `SENT` row는 legacy failure writer가 덮어쓰지 않습니다.
- delivery success와 community/shorts tracking은 같은 transaction입니다.
- grouped outcome unknown 뒤 individual fallback을 실행하지 않습니다.
- claim defer는 attempt를 소비하지 않습니다.
- already-satisfied room은 provider 호출 없이 delivery를 terminal 처리합니다.
- partial delivery revive는 `FAILED` child만 reset하고 `SENT` child를 보존합니다.
- all-quarantined outbox는 revive하지 않습니다.

### 추가할 테스트

```text
TestOutcomeUnknownDoesNotReleaseAlarmClaim
TestGroupedOutcomeUnknownDoesNotFallback
TestAlreadySatisfiedDoesNotInvokeProvider
TestClaimDeferredDoesNotConsumeAttempt
TestSuccessfulSendDoesNotUseIDOnlyRecovery
```

마지막 테스트는 현재 unsafe recovery 경로의 존재를 먼저 드러내는 characterization 또는 TODO-failing test로 만들지 않습니다. PR 4에서 새 경로 cutover와 함께 통과시키도록 test name과 acceptance만 먼저 기록합니다.

### 검증

```bash
go test ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
go test ./hololive/hololive-shared/pkg/service/youtube/outbox/store/...
```

### 완료 조건

- 현재 의도된 behavior가 테스트 이름으로 발견 가능합니다.
- outcome unknown과 tracking transaction 테스트가 provider 호출 횟수까지 단언합니다.
- flaky sleep 대신 injected clock, channel, DB state를 사용합니다.

## PR 1. Additive delivery fencing schema

### 목표

Runtime behavior를 바꾸기 전에 `youtube_notification_delivery.row_version`을 추가하고 모든 row scanner가 읽을 수 있게 합니다.

### migration

현재 manifest 기준 제안 파일명은 다음입니다.

```text
hololive/hololive-api/scripts/migrations/190_youtube_delivery_row_version.sql
```

병렬 branch가 먼저 번호를 사용하면 migration 규약에 따라 merge 시점의 `max+1`로 재번호합니다.

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

Constant default를 사용하고 index를 만들지 않습니다.

### 수정 파일

```text
hololive/hololive-api/scripts/migrations/manifest.txt
hololive/hololive-api/scripts/migrations/<new>_youtube_delivery_row_version.sql
hololive/hololive-shared/pkg/domain/youtube_delivery.go
hololive/hololive-shared/pkg/service/youtube/outbox/deliverysql/pgx.go
hololive/hololive-shared/pkg/service/youtube/outbox/store/queries/*.sql
hololive/hololive-dbtest/testdata/schema_snapshot.golden.sql
관련 DB fixtures/tests
```

모든 delivery `SELECT`/`RETURNING` column order를 검색하여 scanner와 일치시킵니다.

```bash
rg -n "RETURNING .*attempt_count|SELECT .*attempt_count" \
  hololive/hololive-shared/pkg/service/youtube/outbox \
  hololive/hololive-alarm-worker/internal/egress/youtubedispatch
```

### runtime behavior

- 기존 writer는 계속 active입니다.
- 기존 writer가 `row_version`을 증가시키지 않아도 이 PR 단계에서는 새 CAS가 active가 아니므로 허용합니다.
- 새 struct field는 조회·fixture compatibility만 제공합니다.

### 검증

```bash
bash scripts/architecture/check-migration-manifest.sh
go test ./hololive/hololive-dbtest -run TestSchemaSnapshotGolden
SCHEMA_SNAPSHOT_UPDATE=1 go test ./hololive/hololive-dbtest -run TestSchemaSnapshotGolden
go test ./hololive/hololive-shared/pkg/domain/...
go test ./hololive/hololive-shared/pkg/service/youtube/outbox/...
```

Snapshot update 명령을 실행한 뒤 update flag 없이 다시 실행해 golden이 안정적인지 확인합니다.

### rollback

Binary rollback만 수행하고 additive column은 유지합니다. migration file을 되돌려 production column을 drop하지 않습니다.

### 완료 조건

- fresh bootstrap과 existing DB migration이 모두 성공합니다.
- row scanner가 version을 반환합니다.
- schema snapshot에 column과 constraint가 있습니다.
- `row_version` index가 없습니다.

## PR 2. Typed lifecycle vocabulary와 pure policy

### 목표

DB write 경로를 바꾸지 않고 state/event/failure/decision policy를 alarm-worker internal에 추가합니다.

### 새 package

```text
hololive/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle/
├── status.go
├── event.go
├── failure.go
├── snapshot.go
├── decision.go
├── delivery_policy.go
├── outbox_policy.go
├── retry_policy.go
├── revive_policy.go
└── *_test.go
```

### delivery event

초기 event vocabulary는 다음으로 제한합니다.

```text
AlreadySatisfied
ClaimDeferred
PreparationRetryableFailure
PreparationPermanentFailure
BeginSend
Delivered
KnownNotDeliveredRetryable
KnownNotDeliveredPermanent
SendingLeaseExpired
ReviveFailed
```

`OutcomeUnknown`은 immediate transition event로 등록하지 않습니다. Send application disposition으로 처리하며 DB write가 없습니다.

### policy API

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

실제 decision은 concrete sealed type으로 구현합니다. 하나의 struct에 nullable patch field를 몰아넣지 않습니다.

### adapter

기존 `domain.OutboxStatus` row를 lifecycle `DeliveryStatus`로 변환하는 explicit adapter를 둡니다. 알 수 없는 DB status는 zero value로 조용히 변환하지 않고 오류를 반환합니다.

### contract tests

- state/event 전체 matrix
- retry 경계
- attempt semantics
- `SENT`, `QUARANTINED` terminal
- `ClaimDeferred`, `AlreadySatisfied` no-attempt
- explicit clock purity
- invalid decision construction 방지
- unknown DB vocabulary 거부

### runtime behavior

기존 writer와 dispatcher flow를 그대로 사용합니다. 이 PR에서는 policy를 production DB write에 연결하지 않습니다.

### 검증

```bash
go test ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle/...
go test -race ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle/...
```

### rollback

Inactive package만 제거합니다.

### 완료 조건

- policy test는 DB 없이 실행됩니다.
- policy package가 store, pgx, slog, provider SDK를 import하지 않습니다.
- `time.Now()` 호출이 없습니다.
- `go list -deps`에서 lifecycle package가 alarm-worker internal과 표준 라이브러리 외 실행 구현에 의존하지 않습니다.

## PR 3. Typed preparation result와 provider outcome

### 목표

`DispatchResult.FailureBuckets map[string][]int64`와 reason 문자열 기반 permanent 분류를 typed result로 교체합니다. 상태 write는 아직 legacy repository를 사용하되 application mapping은 typed 결과를 기준으로 합니다.

### 새 타입

```go
type PreparationResultKind uint8

type ProviderOutcomeKind uint8

type DeliveryOutcome struct {
    DeliveryID  int64
    OutboxID    int64
    OperationID string
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
reason 문자열을 map key로 사용하는 metric recorder
```

### transport classifier

Provider SDK/HTTP 오류를 다음으로 분류하는 adapter를 한 위치에 둡니다.

```text
Delivered
KnownNotDeliveredRetryable
KnownNotDeliveredPermanent
OutcomeUnknown
```

Timeout과 connection reset은 provider 계약이 반대로 증명하지 않는 한 `OutcomeUnknown`입니다.

### grouped send

- outcome unknown이면 fallback하지 않습니다.
- known-not-accepted이며 `fallback_allowed`일 때만 individual fallback을 허용합니다.
- fallback 자체는 attempt를 소비하지 않습니다.
- provider call count와 member delivery 집합을 테스트합니다.

### legacy writer bridge

이 PR에서 legacy writer를 호출해야 한다면 typed outcome을 legacy method로 변환하는 adapter는 application service 한 파일에만 둡니다. Repository가 raw provider error를 받지 않게 합니다.

### 검증

```bash
go test ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
go test -race ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
```

### rollback

Typed result commit 전체를 revert합니다. DB schema 변화는 없습니다.

### 완료 조건

- production code에 failure reason map이 없습니다.
- permanent/retry 정책은 raw 문자열을 비교하지 않습니다.
- outcome unknown path는 상태 writer와 claim release를 호출하지 않습니다.
- grouped unknown test에서 provider call은 정확히 1회입니다.

## PR 4. Internal transition store와 operation-level CAS

### 목표

Worker 전용 store를 alarm-worker internal로 이동하고 row-version aware 의도별 API를 추가합니다. 새 API는 아직 production dispatcher에 연결하지 않을 수 있지만 contract integration test를 통과해야 합니다.

### package 이동

```text
FROM hololive/hololive-shared/pkg/service/youtube/outbox/store
TO   hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store
```

`git mv`를 사용하고 alarm-worker import를 같은 PR에서 수정합니다. 다른 production runtime import가 없음을 architecture gate와 `go list`로 확인합니다.

`deliverysql` helper는 이 PR에서 무리하게 모두 옮기지 않아도 되지만, production 소비자가 alarm-worker 하나로 확인된 helper는 최종 cleanup PR에서 internal로 회수합니다.

### transition API

```text
ClaimPending
BeginSending
CompleteAlreadySatisfied
DeferClaim
ScheduleRetryBatch
FailBatch
CompleteSent
QuarantineStaleSending
ReviveFailedOutboxes
UpdateOutboxAggregateStatuses
```

Generic `UpdateStatus`, nullable patch DSL은 만들지 않습니다.

### version CAS

Claim과 모든 mutation에서 `row_version=row_version+1`을 적용합니다. `status`, version, attempt, `locked_at`을 모두 expected predicate에 포함합니다.

### operation-level atomicity

`BeginSending`과 grouped finalization은 exact provider operation member 전체를 transaction에서 검사합니다. 일부 member conflict 시 전체 rollback합니다.

### success transaction

`CompleteSent`는 member delivery와 tracking을 같은 transaction에서 commit합니다. CAS에 성공하지 않은 member를 tracking에 포함하지 않습니다.

### unsafe recovery 제거 준비

ID-only로 `SENDING -> SENT`를 쓰는 recovery API를 새 store에 구현하지 않습니다. Legacy 경로는 PR 5 cutover와 동시에 제거합니다.

### integration tests

```text
TestClaimPendingIncrementsVersion
TestBeginSendingRejectsStalePreparationLease
TestBeginSendingRollsBackWholeOperationOnOneConflict
TestCompleteSentRollsBackWholeOperationOnOneConflict
TestCompleteSentPersistsTrackingForCommittedMembers
TestScheduleRetryDoesNotReevaluateMaxRetries
TestQuarantineRejectsFreshSending
TestReviveUsesAggregateProjectorForChildOutbox
```

### 검증

```bash
go test ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/...
go test -race ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/...
RUN_INTEGRATION_TESTS=true go test -tags=integration \
  ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/...
./scripts/architecture/check-repository-ownership.sh
```

### rollback

Import/package move commit을 revert합니다. Schema column은 유지합니다.

### 완료 조건

- store package의 production importer는 alarm-worker internal뿐입니다.
- 모든 transition SQL은 version을 검사하고 증가시킵니다.
- repository method가 `MaxRetries`를 받지 않습니다.
- grouped operation partial CAS가 transaction rollback됩니다.

## PR 5. Runtime writer cutover와 preparation-before-SENDING

### 목표

Dispatcher의 active write path를 새 lifecycle/transition service로 한 번에 교체합니다. 이 PR이 실제 behavior cutover입니다.

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
-> outbox load
-> validation / rendering
-> alarm-once claim / already-satisfied check
-> request와 ClientRequestID 확정
-> PreparedSendOperation 생성
-> operation-level BeginSending
-> provider send
-> typed outcome
-> policy decision
-> operation-level finalization
```

### 주요 변경 파일

```text
claim_manager_pipeline.go
claim_manager_gate.go
send_engine*.go
metrics_recorder*.go
delivery_transition_service.go
outbox 관련 adapter
Dispatcher wiring/tests
```

### AlreadySatisfied

Pre-send check에서 해당 room의 delivery가 이미 충족됐으면 `CompleteAlreadySatisfied`를 호출하고 provider를 호출하지 않습니다. 새 tracking mark를 추정해서 만들지 않습니다.

### ClaimDeferred

다른 실행이 alarm claim을 보유하면 `DeferClaim`으로 due를 이동하고 attempt는 유지합니다.

### provider success finalization retry

Provider 성공 뒤 DB finalization error가 나면 같은 immutable `SentOperation`과 Send fence로 local DB finalization만 bounded retry합니다. Provider를 다시 호출하지 않습니다.

Finalization conflict가 발생하면 critical metric을 기록하고 unsafe ID-only recovery를 실행하지 않습니다. 행은 이후 quarantine될 수 있습니다.

### send lease budget

Config validation에 다음을 추가합니다.

```text
SendingFinalizeGrace = max(2*PollInterval, 5s)
LockTimeout >= DeliverySendTimeout + SendingFinalizeGrace
```

Provider call을 시작하기 전에 remaining lease budget을 검사합니다.

### old writer 제거

새 path를 연결하는 같은 PR에서 다음 production call site를 제거합니다.

```text
MarkSendingBatchIfLocked direct call
MarkFailedRetryBatchIfLocked direct call
MarkPermanentFailureBatchIfLocked direct call
markDispatchResult legacy bucket loop
recoverSuccessfulCommunityShortsSentState ID-only path
```

Compatibility wrapper가 필요하면 test-only 또는 unexported adapter로 한정하고 production call graph에는 남기지 않습니다.

### 테스트

- preparation failure는 `SENDING`을 만들지 않습니다.
- BeginSending conflict operation은 provider call 0회입니다.
- success finalization DB transient error에서 provider call 1회입니다.
- success finalization conflict에서 unsafe recovery call 0회입니다.
- unknown outcome에서 state writer와 claim release 0회입니다.
- stale sending sweeper와 live send가 config invariant 아래에서 경합하지 않습니다.
- grouped begin/finalize all-or-none입니다.

### 검증

```bash
go test ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
go test -race ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
RUN_INTEGRATION_TESTS=true go test -tags=integration \
  ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
go test ./hololive/hololive-shared/pkg/service/youtube/tracking/observation/...
```

### rollout preflight

Cutover 배포 전에 active send 수를 확인합니다.

```sql
SELECT count(*) AS active_sending
FROM youtube_notification_delivery
WHERE status = 'SENDING'
  AND locked_at >= now() - interval '5 minutes';
```

0이 아닐 때는 graceful drain 후 재확인합니다. 강제 종료로 active send를 늘리지 않습니다.

### deployment

- alarm-worker 단일 인스턴스 replacement로 배포합니다.
- old/new egress owner를 동시에 실행하지 않습니다.
- migration이 먼저 적용됐는지 확인합니다.
- 배포 직후 finalization conflict, SENDING age, quarantine 증가를 확인합니다.

### immediate rollback trigger

다음 중 하나면 새 binary를 rollback합니다.

- provider success finalization conflict가 1건이라도 발생합니다.
- provider call count가 contract test/trace와 다르게 중복됩니다.
- `SENT`와 tracking state가 불일치합니다.
- terminal row가 active lock을 보유합니다.
- aggregate가 child 상태와 수렴하지 않습니다.

Additive `row_version` column은 rollback하지 않습니다.

### 완료 조건

- production delivery writer는 새 transition service 하나입니다.
- preparation failure는 provider-effect phase와 분리됩니다.
- outcome unknown safety behavior가 기존과 동일합니다.
- external send 재호출 없는 finalization retry가 검증됩니다.

## PR 6. Outbox fanout writer 경계와 revive 정렬

### 목표

Pre-fanout direct writer와 post-fanout aggregate writer를 물리적으로 분리합니다.

### OutboxFanoutService

다음 operation을 제공합니다.

```text
CompleteWithoutTargets
RecordFanoutFailure
MaterializeFanout
```

모든 operation은 active outbox claim token을 검사합니다.

### MaterializeFanout transaction

```text
outbox claim 검증
-> canonical target room dedupe
-> delivery INSERT ... ON CONFLICT (outbox_id, room_id) DO NOTHING
-> outbox lock clear
-> commit
```

Child 일부 insert 후 outbox lock release가 실패하는 partial state를 허용하지 않습니다.

### direct writer guard

Pre-fanout finalization SQL은 transaction 안에서 `NOT EXISTS child delivery`를 검사합니다. Child가 있으면 conflict로 반환하고 aggregate projector에 맡깁니다.

### revive

- child가 있으면 FAILED child만 reset합니다.
- 같은 transaction에서 표준 aggregate projector를 실행합니다.
- child가 없을 때만 outbox status를 직접 reset합니다.
- all-quarantined outbox는 제외합니다.

### 테스트

```text
TestMaterializeFanoutIsAtomic
TestCompleteWithoutTargetsRejectsExistingChild
TestFanoutFailureRejectsExistingChild
TestReviveChildOutboxUsesAggregateProjection
TestReviveDoesNotResetQuarantinedChild
TestConcurrentFanoutAndDirectFinalizeCannotBothCommit
```

### 검증

```bash
go test ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
RUN_INTEGRATION_TESTS=true go test -tags=integration \
  ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
```

### rollback

새 binary를 이전 writer-cutover binary로 rollback합니다. Delivery schema는 유지합니다.

### 완료 조건

- child outbox direct writer call site가 없습니다.
- fanout materialization과 lock clear가 한 transaction입니다.
- revive가 aggregate 계산을 복제하지 않습니다.

## PR 7. Constraints, observability, architecture gate, legacy cleanup

### 목표

우회 writer와 stale shared implementation을 제거하고 운영 검증을 완결합니다.

### DB audit

다음 query가 0건인지 확인하고 필요한 data repair를 별도 migration으로 수행합니다.

```sql
SELECT id, status, attempt_count, locked_at, sent_at
FROM youtube_notification_delivery
WHERE status NOT IN ('PENDING', 'SENDING', 'SENT', 'FAILED', 'QUARANTINED')
   OR attempt_count < 0
   OR (status = 'SENDING' AND locked_at IS NULL)
   OR (status = 'SENT' AND sent_at IS NULL)
   OR (status IN ('SENT', 'FAILED', 'QUARANTINED') AND locked_at IS NOT NULL);
```

이미 동일 constraint가 있으면 중복 생성하지 않습니다. 새 constraint는 migration 규약에 따라 `NOT VALID`와 `VALIDATE` 순서를 사용합니다.

### 제거 대상

```text
legacy MarkSent/MarkFailed/MarkFailedRetry methods
reason bucket helpers
ID-only success recovery
shared store package 잔여 implementation
unused SQL files
status alias facade
```

`deliverysql` 등 single-runtime helper의 소비자 집합을 다시 검색하고 alarm-worker 하나뿐이면 internal로 이동합니다.

### architecture gate

새 gate 또는 기존 ownership gate에 다음을 추가합니다.

- 허용된 internal store 밖의 `UPDATE youtube_notification_delivery ... status` 금지
- repository API에서 `MaxRetries` 금지
- `FailureBuckets`, `deliveryFailureReasonIsPermanent` 재도입 금지
- `QUARANTINED`를 primary claim/revive 대상에 넣는 SQL 금지
- `SENT`를 source로 하는 transition SQL 금지
- alarm-worker 밖의 YouTube transition store import 금지

SQL fixtures와 migrations는 allowlist로 구분합니다. 광범위한 디렉터리 제외는 사용하지 않습니다.

### metrics

```text
youtube_delivery_transition_total
youtube_delivery_transition_conflict_total
youtube_delivery_outcome_unknown_total
youtube_delivery_quarantine_total
youtube_delivery_finalization_retry_total
youtube_outbox_aggregate_transition_total
youtube_outbox_aggregate_lag_seconds
```

Raw IDs와 오류 문자열을 label로 사용하지 않습니다.

### 결정 evidence 갱신

구현이 merge되면 decision record를 다음 순서로 갱신합니다.

```text
planned -> in_progress
in_progress -> implemented
implemented -> verified
```

`implementation`에는 실제 package/migration/gate 경로를, `evidence`에는 contract/integration/crash-window test 경로를 넣습니다. `docs/decisions/INDEX.md`는 iris-stack canonical renderer로 다시 생성합니다.

### 전체 검증

```bash
./scripts/architecture/check-repository-ownership.sh
./scripts/architecture/ci-boundary-gate.sh
bash scripts/architecture/check-migration-manifest.sh
./build-all.sh --no-bump

go build ./ \
  ./admin-dashboard/backend/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-api/... \
  ./hololive/hololive-alarm-worker/... \
  ./hololive/hololive-youtube-collector/...

go test ./ \
  ./admin-dashboard/backend/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-api/... \
  ./hololive/hololive-alarm-worker/... \
  ./hololive/hololive-youtube-collector/...

RUN_INTEGRATION_TESTS=true go test -tags=integration \
  ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...
```

Local pre-push gate와 race/NilAway 결과도 저장소 규칙에 따라 통과해야 합니다.

### 완료 조건

- 우회 status writer를 architecture gate가 차단합니다.
- shared public path에는 cross-runtime 또는 진성 다중 소비자만 남습니다.
- decision record가 `verified`이고 evidence path가 실제 존재합니다.
- 운영 dashboard에서 unknown, quarantine, conflict, aggregate lag를 확인할 수 있습니다.

## 운영 검증

### 배포 전

- schema migration ledger에 row-version migration이 있습니다.
- invalid-state audit가 0건입니다.
- active `SENDING`이 drain됐습니다.
- alarm-worker replica가 1입니다.
- old/new egress binary 동시 실행 계획이 없습니다.

### 배포 직후

- `BeginSend` conflict가 정상적인 stale claim 범위인지 확인합니다.
- provider success finalization conflict는 0이어야 합니다.
- quarantine rate가 기존 baseline 대비 비정상 증가하지 않아야 합니다.
- aggregate lag가 `AggregateSyncInterval`의 합리적 배수 안에서 수렴해야 합니다.
- community/shorts sent tracking과 delivery `SENT`가 일치해야 합니다.

### 장기 확인

- `QUARANTINED` backlog와 처리 정책을 운영자가 볼 수 있습니다.
- retry exhausted와 permanent failure가 stable failure code로 분리됩니다.
- `SENDING` oldest age가 `LockTimeout`을 지속적으로 넘지 않습니다.
- revive가 all-quarantined outbox를 flap시키지 않습니다.

## 최종 acceptance

이 계획은 다음 조건을 모두 만족할 때 완료됩니다.

1. `youtube_notification_delivery.row_version`이 production schema와 row model에 있습니다.
2. claim과 모든 delivery mutation이 version을 증가시킵니다.
3. transition policy는 alarm-worker internal의 pure package입니다.
4. failure policy가 raw 문자열과 SQL `CASE`에 분산되지 않습니다.
5. provider outcome unknown은 즉시 DB write, claim release, fallback, resend를 하지 않습니다.
6. provider operation member는 begin/finalization에서 all-or-none입니다.
7. provider success 후 DB finalization retry가 외부 send를 재호출하지 않습니다.
8. ID-only success recovery가 없습니다.
9. child outbox status는 aggregate projector만 계산합니다.
10. worker 전용 store가 alarm-worker internal에 있습니다.
11. legacy writer와 SQL이 제거되고 architecture gate가 재도입을 차단합니다.
12. contract, integration, crash-window, race test가 모두 통과합니다.
13. decision record가 `verified`로 갱신됩니다.

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
