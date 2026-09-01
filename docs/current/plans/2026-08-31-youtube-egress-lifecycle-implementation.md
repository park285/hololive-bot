# YouTube egress lifecycle 구현 계획

- 작성일: 2026-08-31 KST
- 아키텍처 정본: [`../architecture/youtube-egress-lifecycle-transition-ownership-20260831.md`](../architecture/youtube-egress-lifecycle-transition-ownership-20260831.md)
- 규범 계약: [`../architecture/youtube-egress-lifecycle-contract-20260831.md`](../architecture/youtube-egress-lifecycle-contract-20260831.md)
- Logical ledger: [`../architecture/youtube-egress-logical-delivery-ledger-20260831.md`](../architecture/youtube-egress-logical-delivery-ledger-20260831.md)
- Commit 판정: [`../architecture/youtube-egress-lifecycle-commit-adjudication-20260831.md`](../architecture/youtube-egress-lifecycle-commit-adjudication-20260831.md)
- Library 검토: [`../architecture/youtube-egress-lifecycle-library-review-20260831.md`](../architecture/youtube-egress-lifecycle-library-review-20260831.md)

**Decisions:** `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership` (governing)

## 목적

현재 YouTube egress의 분산된 SQL 상태 변경을 typed lifecycle policy, deterministic logical owner, version-fenced transition store, durable logical ledger로 교체합니다. 각 단계는 하나의 canonical writer만 남기며 old/new writer가 같은 row를 동시에 수정하는 dual-write 기간을 만들지 않습니다.

목표 흐름:

```text
ClaimPending
    -> PreparationLease
    -> fail-closed canonical logical identity
    -> ledger-first logical group resolution
    -> deterministic owner/follower projection
    -> tracking requirement
    -> immutable provider operation
    -> version-fenced BeginSending
    -> primary commit adjudication
    -> provider call once
    -> typed outcome
    -> delivery/tracking/ledger terminal transaction
    -> touched outbox IDs
    -> separate aggregate/terminal_at projector
```

Outbox writer 경계:

```text
pre-fanout  = alarm-worker OutboxFanoutService
post-fanout = alarm-worker aggregate projector
terminal evidence = PostgreSQL logical ledger
```

## 현재 근거와 깨진 불변조건

- Physical unique key는 outbox `(kind, content_id)`, delivery `(outbox_id, room_id)`입니다. Cleanup 뒤 새 outbox ID가 생길 수 있으므로 `(outbox_id, room_id)`는 logical dedupe key가 아닙니다.
- Community/Shorts canonicalizer는 `hololive/hololive-shared/internal/service/youtube/contentid`에 있어 alarm-worker module로 store를 이동한 뒤 import할 수 없습니다.
- `hololive/hololive-shared/pkg/service/youtube/outbox/store`는 worker lifecycle SQL을 소유하지만 production importer는 alarm-worker입니다.
- Poller batch repository도 다음 query에서 delivery/outbox를 직접 완료하거나 rearm합니다.

```text
hololive/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo/queries/repository_batch_writes_0244_06.sql
hololive/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo/queries/repository_batch_delivery_state_0198_02.sql
hololive/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo/queries/repository_batch_completed_finalize_0088_01.sql
hololive/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo/queries/repository_batch_completed_finalize_0111_02.sql
```

- Current cleanup은 `COALESCE(sent_at, created_at)`을 사용하므로 늦게 `FAILED`가 된 오래된 outbox를 즉시 삭제할 수 있습니다.
- Post tracking `SENT`는 post-level 관측이며 특정 room delivery의 성공 evidence가 아닙니다.
- ID-only `SENDING -> SENT` recovery는 provider success와 exact tracking/room evidence를 증명하지 못합니다.

## 범위와 승인 경계

이 계획은 code/schema/test/document 구현 순서를 정의합니다. 다음 live action은 각각 별도 현재 승인이 있어야 합니다.

- Production worker stop/restart와 schema migration
- Ledger/`terminal_at` backfill을 쓰는 one-shot 실행
- Lifecycle writer cutover와 rollback
- Repair, cleanup, ledger state completion을 포함한 production data 변경

Remote host는 build하지 않습니다. Alarm-worker image와 one-shot binary는 arm64 환경에서 로컬 build하고 runbook의 no-build artifact transfer/deploy 경로를 사용합니다.

## 작업 원칙

1. 각 PR은 독립적으로 build/test 가능하고 다음 단계의 precondition을 명시합니다.
2. Schema migration은 additive DDL만 포함하며 기존 row의 무제한 backfill을 수행하지 않습니다.
3. Canonical identity 오류는 provider 호출 전에 실패하며 raw ID fallback을 만들지 않습니다.
4. Compatibility writer가 live가 되기 전에 fixed high-water를 잡지 않습니다.
5. 모든 terminal kind를 ledger backfill 범위에 포함합니다.
6. Count equality나 sample을 completion 증거로 사용하지 않고 canonical anti-join 0건을 요구합니다.
7. 삭제된 historical row의 coverage가 증명되지 않으면 completion과 writer/cleanup cutover를 차단합니다.
8. Provider effect 이후 DB retry는 immutable DB command만 반복할 수 있으며 provider를 재호출하지 않습니다.
9. Delivery/tracking/ledger terminal envelope와 outbox aggregate transaction을 분리합니다.
10. Alarm-worker replica는 1을 유지합니다.

## 작업 의존성

| 작업 | Delivery 단위 | 선행 조건 | 주요 산출물 |
|---|---|---|---|
| T01 | prework | 없음 | writer/coverage evidence와 characterization |
| T02 | PR 1 | T01 writer inventory | public fail-closed canonical resolver |
| T03 | PR 2 | T02 | alarm-worker internal store |
| T04 | PR 3 | T02, T03 | additive schema, sole compatibility writer, poller direct-writer 제거 |
| T05 | PR 4 + 승인된 one-shot | T04 production compatibility writer | fixed-high-water backfill과 completion state |
| T06 | PR 5 | T02 | typed policy와 inactive preparation seam |
| T07 | PR 6 + 승인된 cutover | T05 completion, T06 | version-fenced transition writer |
| T08 | PR 7 | T07 | fanout/revive/projector/cleanup 단일 ownership |
| T09 | PR 8 | T08 | architecture gate, legacy removal, verification evidence |

T06 code review는 T04/T05와 병행할 수 있지만 production activation은 T05 completion 뒤 T07에서만 수행합니다.

## 구현 작업

### T01 — Characterization, writer inventory, historical coverage audit

목표는 보존할 안전 속성, 제거할 위험, backfill 가능 범위를 구현 전에 고정하는 것입니다.

작업:

- Alarm worker store, dispatcher query, poller batch repository, tracking repository의 outbox/delivery write를 operation별로 목록화합니다.
- 위 네 poller query와 모든 `UPDATE youtube_notification_{outbox,delivery}` caller가 어느 runtime에서 실행되는지 확인합니다.
- Cleanup retention, producer source retention, revive, repair, manual replay, big-bang cutover 경계를 읽고 각 경로의 `replay_floor_at` evidence를 기록합니다.
- 이미 삭제된 terminal row에 대해 완전성을 주장할 수 있는 가장 이른 `legacy_coverage_start_at`을 산출합니다.
- 다음 안전 동작을 characterization test로 고정합니다.

```text
Stale locked_at token은 미세한 차이도 거부
SENDING은 primary claim 대상이 아님
outcome_unknown은 write/release/fallback/resend 없음
stale SENDING은 logical group QUARANTINED로 수렴
SENT는 stale failure writer가 덮어쓰지 않음
delivery success와 tracking은 한 transaction
claim defer는 attempt를 소비하지 않음
later room success는 already-sent tracking을 수용
revive는 SENT/QUARANTINED evidence를 보존
```

중단 조건:

- Writer ownership을 모두 식별하지 못하면 T04 이후로 진행하지 않습니다.
- Replay 경로 하나라도 unbounded/unknown이면 compatibility writer까지는 구현할 수 있지만 T05 completion, T07 writer cutover, T08 cleanup enablement는 진행하지 않습니다.
- Unsafe ID-only recovery를 보존하기 위한 test는 추가하지 않습니다.

검증: V01, V02
완료 기준: AC01, AC02

### T02 — Canonical logical identity를 cross-runtime public contract로 승격

목표는 store 이동과 ledger writer보다 먼저 모든 runtime이 같은 fail-closed logical identity를 사용하게 하는 것입니다.

주요 경로:

```text
from: hololive/hololive-shared/internal/service/youtube/contentid
to:   hololive/hololive-shared/pkg/service/youtube/contentid
```

작업:

- Community/Shorts는 `(kind, canonical_post_id, room_id)`, 나머지는 `(kind, outbox.content_id, room_id)`를 반환하는 pure API를 제공합니다.
- Kind vocabulary와 길이/빈 값 검증을 ledger schema와 일치시킵니다.
- Payload parse/canonicalization 오류를 typed error로 반환하고 raw `content_id` fallback을 제거합니다.
- Poller, tracking, dispatcher, store caller를 한 PR에서 새 package로 전환합니다.
- Old internal package를 삭제하며 compatibility alias나 re-export를 남기지 않습니다.
- Raw logical key 대신 bounded hash만 log field로 허용합니다.

검증: V03
완료 기준: AC03

### T03 — Worker persistence를 alarm-worker internal로 이동

목표는 behavior를 바꾸지 않고 worker lifecycle store의 ownership을 runtime과 일치시키는 것입니다.

주요 경로:

```text
from: hololive/hololive-shared/pkg/service/youtube/outbox/store
to:   hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store
```

작업:

- T02 public canonicalizer가 merge된 뒤 Go/SQL/test files를 `git mv`합니다.
- Worker 전용 row/command/result DTO는 target internal package에 둡니다.
- Shared domain에는 실제 cross-runtime value와 pure contract만 남깁니다.
- Query text, scanner order, retry/finalization behavior를 이 PR에서 변경하지 않습니다.
- Production importer가 alarm-worker 하나인지 architecture test로 확인합니다.
- Old store package와 forwarding wrapper를 삭제합니다.

검증: V04
완료 기준: AC04

### T04 — Additive schema와 compatibility terminal writer

목표는 old lifecycle behavior를 유지하면서 이후 backfill과 cutover에 필요한 durable evidence를 새 terminal event부터 기록하는 것입니다.

Schema:

- `youtube_notification_delivery.row_version bigint NOT NULL DEFAULT 0`와 nonnegative constraint
- `youtube_notification_outbox.terminal_at timestamptz`
- `youtube_notification_delivery_ledger`
- `youtube_notification_delivery_ledger_state`
- Migration manifest와 schema golden snapshot

Migration 번호는 구현 시작 시 current manifest의 `max+1`로 결정합니다. Migration에는 existing row update, ledger population, completion marker 삽입을 넣지 않습니다.

Existing delivery의 `row_version`은 nullable constant-default column 추가, idempotent `NOT VALID` null/nonnegative check, `VALIDATE CONSTRAINT`, `SET NOT NULL` 순서로 적용합니다. 새 table/constraint를 포함한 모든 statement는 migration file 중간 실패 뒤 전체 재실행에 안전해야 합니다.

Compatibility writer:

- Provider 호출 전에 T02 resolver로 logical identity를 검증합니다.
- Existing success transaction에 delivery/tracking/ledger `SENT`를 atomic하게 기록합니다.
- Stale unknown quarantine transaction에 group/ledger `QUARANTINED`를 atomic하게 기록합니다.
- Ledger monotonic upsert SQL은 ledger 계약의 exact `RecordSent`/`RecordQuarantined` semantics를 구현합니다.
- Aggregate writer는 상태가 terminal로 바뀔 때 `terminal_at`을 갱신하고 idempotent same-state에서는 보존합니다.
- Ledger state가 absent/incomplete/unsupported이면 cleanup은 fail closed합니다.
- Store read-back과 commit adjudication에 ledger를 포함합니다.
- Poller batch repository의 direct outbox/delivery `PENDING/SENT` mutation을 제거합니다. Poller는 source observation과 outbox create만 유지합니다.
- Post-level tracking으로 room delivery `SENT`를 추정하지 않습니다.
- Group-safe worker revive가 T08에서 준비될 때까지 제거된 poller rearm liveness를 일시 중단합니다.

Deployment gate:

1. Compatibility image와 schema migration을 먼저 준비합니다.
2. 승인된 maintenance window에 old alarm-worker와 poller batch repository를 실행하는 runtime을 포함한 inventoried lifecycle writer를 모두 중지합니다.
3. Additive migration을 적용합니다.
4. Poller direct lifecycle mutation이 제거된 runtime을 배포합니다.
5. Cleanup이 ledger gate로 frozen인 compatibility worker를 시작합니다.
6. Writer audit가 alarm-worker 밖 lifecycle mutation 0건임을 확인합니다.
7. Old worker와 compatibility worker를 동시에 실행하지 않습니다.

Rollback은 additive schema를 유지한 채 compatibility-aware binary로만 수행합니다. Schema 적용 뒤 ledger를 모르는 old cleanup binary를 다시 시작하지 않습니다.

검증: V05, V06, V07
완료 기준: AC05, AC06

### T05 — Fixed-high-water ledger와 `terminal_at` backfill

목표는 retained terminal evidence를 전부 이관하고, 삭제된 historical evidence의 coverage까지 검증한 뒤 durable completion marker를 기록하는 것입니다.

구현:

- `hololive/hololive-alarm-worker/cmd/` 아래에 dedicated bounded/resumable Go command를 둡니다.
- Alarm-worker Dockerfile이 worker와 backfill binary를 함께 로컬 build해 `/dist/bin`에 포함합니다.
- Remote에서는 image entrypoint override를 사용하는 no-build one-shot으로 실행합니다.
- Compatibility writer가 live이고 alarm-worker 밖 lifecycle mutation이 0건인 상태에서 delivery/outbox `MAX(id)`를 한 번만 캡처해 state row에 저장합니다.
- Delivery pass는 fixed range 전체를 ID 순서 bounded batch로 스캔하고 모든 kind의 `SENT/QUARANTINED`를 ledger에 monotonic upsert합니다.
- Outbox pass는 fixed range 전체를 스캔해 `SENT`에는 known sent evidence, `FAILED`에는 backfill 시작 시각을 보수적으로 기록하고 `PENDING`은 null을 유지합니다.
- Data writes와 cursor advance를 같은 transaction으로 묶습니다.
- Restart는 persisted cursor를 재사용하고 high-water를 재설정하지 않습니다.
- Verification pass는 canonical source key와 ledger를 fixed range 전체에서 anti-join하고 mismatch 0건인 batch만 verify cursor를 전진시킵니다.
- Invalid identity를 skip하거나 repair로 추정하지 않습니다.
- T01의 모든 `replay_floor_at`에 대해 `legacy_coverage_start_at <= replay_floor_at`을 증명한 경우에만 coverage/completion timestamps를 기록합니다.

승인 경계:

- Binary build/test는 local change 범위입니다.
- Production one-shot, ledger write, `terminal_at` write, completion marker는 별도 production data-write 승인이 필요합니다.
- Coverage를 증명하지 못하면 state는 incomplete로 남기고 cleanup과 T07 cutover를 차단합니다.

검증: V08, V09
완료 기준: AC07, AC08

### T06 — Typed lifecycle rules와 preparation coordinator

목표는 DB writer cutover 전 pure policy와 provider operation preparation을 완성하는 것입니다.

Package:

```text
hololive/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle/
hololive/hololive-alarm-worker/internal/egress/youtubedispatch/preparation/
```

작업:

- Typed status/event/failure/reason/rule ID, retry policy, revive policy를 pure function으로 구현합니다.
- Ledger를 먼저 batch-load하고 `SENT > QUARANTINED > retained physical state` 순서로 logical group을 해석합니다.
- Deterministic owner는 최소 `(created_at, id)`이며 follower는 provider call과 attempt budget을 소유하지 않습니다.
- `QUARANTINED + SENDING`, multiple `SENDING`, group scan overflow는 typed invariant breach로 fail closed합니다.
- Tracking requirement는 `NoTracking`, `RequireClaimOrAlreadySent`, `RequireAlreadySent`로 제한합니다.
- Preparation 결과에 exact claim token, versions, owner/follower IDs, ledger keys, expected pre/post state, canonical timestamps를 동결합니다.
- Provider outcome은 confirmed success, known-not-delivered, outcome-unknown으로 분리합니다.
- Cache hit도 room-level ledger/group resolution을 생략하지 않습니다.
- 이 단계의 새 coordinator는 inactive seam에서 검증하고 production writer ownership은 바꾸지 않습니다.

검증: V10
완료 기준: AC09

### T07 — Version-fenced transition store와 runtime writer cutover

목표는 terminal delivery writer를 새 alarm-worker transition owner 하나로 원자적으로 전환하는 것입니다.

Store commands:

```text
ClaimPending
DeferFollower
ReconcileFulfilled
PropagateUnresolved
BeginSending
CompleteSent
ScheduleKnownRetry
CompleteFailed
QuarantineLogicalGroup
LoadAdjudicationState
```

작업:

- 모든 claim/mutation이 exact status, `row_version`, attempt, lock token을 fence하고 성공 시 version을 증가시킵니다.
- Logical group command는 owner/follower 전체를 all-or-none으로 변경합니다.
- `CompleteSent`는 delivery group, tracking requirement, ledger `SENT`를 한 transaction에 commit합니다.
- `QuarantineLogicalGroup`은 delivery group과 ledger `QUARANTINED`를 한 transaction에 commit합니다.
- Terminal command는 touched outbox IDs를 반환할 뿐 aggregate를 같은 transaction에서 변경하지 않습니다.
- Primary exact read-back은 owner/follower/tracking/ledger 전체를 판정합니다.
- `Indeterminate`를 retryable provider event로 바꾸지 않습니다.
- Provider success 후 confirmed non-commit이면 immutable DB finalization만 재시도합니다.
- Commit indeterminate, mixed projection, tracking/ledger mismatch에서는 provider를 재호출하지 않습니다.
- ID-only success recovery와 reason-string transition 분기를 제거합니다.
- Cutover commit에서 old terminal call site를 제거해 writer 하나만 남깁니다.

Deployment precondition:

- T05 state의 supported `schema_version`과 non-null `completed_at`
- T01 writer inventory 100% 분류
- Provider call-count와 commit fault-injection tests 통과
- 승인된 production deploy/restart

Rollback은 ledger-aware T04 compatibility binary로만 수행하며 schema/state/ledger를 보존합니다.

검증: V11, V12
완료 기준: AC10, AC11

### T08 — Outbox fanout, revive, poller writers, cleanup 정렬

목표는 pre/post-fanout ownership과 retention을 alarm-worker lifecycle에 맞추고 cross-runtime direct writer를 제거하는 것입니다.

작업:

- `OutboxFanoutService`가 outbox claim, target snapshot, child materialization, no-target completion을 소유합니다.
- Fanout commit ambiguity는 canonical child set 전체로 판정하며 partial child는 atomicity breach입니다.
- Aggregate projector가 child 상태에서 outbox 상태를 계산하고 별도 transaction에서 `terminal_at`을 유지합니다.
- Immediate aggregate 실패는 terminal envelope를 rollback하지 않고 background projector가 수렴시킵니다.
- Worker revive가 source freshness와 logical group 전체를 검사하고 ledger absent일 때만 `FAILED -> PENDING`을 수행합니다.
- T04에서 source-only로 제한한 poller ownership을 architecture gate로 고정합니다.
- 제거된 poller rearm의 liveness를 ledger-aware worker group revive로 대체하고 compatibility suspension을 제거합니다.
- Cleanup은 supported completed ledger state, non-null `terminal_at`, fixed cutoff, child ledger evidence, active sibling 부재를 한 bounded transaction에서 확인합니다.
- Cleanup retry는 original cutoff를 보존하며 ledger row를 삭제하지 않습니다.

검증: V13, V14
완료 기준: AC12, AC13

### T09 — Constraint validation, observability, architecture gate, legacy 제거

목표는 임시 compatibility 경로를 제거하고 ownership drift를 CI와 운영 증거로 차단하는 것입니다.

작업:

- Migration에서 분리한 constraint validation이 필요하면 bounded 운영 절차와 lock budget을 명시합니다.
- Transition/rule, logical resolution, commit adjudication, ledger/backfill, aggregate lag, cleanup guard metric을 추가합니다.
- Raw room/logical IDs와 error string을 metric label에 넣지 않습니다.
- Architecture test가 shared/poller의 delivery lifecycle direct update와 old store import를 금지합니다.
- Compatibility-only code, old SQL, obsolete reason branch, old package를 제거하며 alias를 남기지 않습니다.
- DB에서 row-version/terminal/ledger shape, impossible mixed state, backfill state를 read-only audit합니다.
- Production observation evidence가 확보된 뒤에만 governing decision status를 `verified`로 전환합니다.

검증: V15, V16
완료 기준: AC14, AC15

## Acceptance criteria

### AC01 — 모든 outbox/delivery writer와 runtime owner가 경로별로 분류되어 있습니다.

### AC02 — 모든 producer/revive/repair/manual replay path에 bounded `replay_floor_at` evidence가 있거나 cutover가 명시적으로 차단됩니다.

### AC03 — 하나의 public canonical resolver가 모든 caller에 사용되고 invalid identity는 provider 호출 전에 실패합니다.

### AC04 — Worker lifecycle store와 DTO/SQL/test가 alarm-worker internal에 있고 old shared store import가 없습니다.

### AC05 — Additive schema에 delivery version, outbox `terminal_at`, ledger, ledger state가 있으며 migration 안에 unbounded data backfill이 없습니다.

### AC06 — Compatibility success/quarantine writer가 ledger를 terminal transaction에 기록하고, poller direct lifecycle writer가 0건이며, incomplete ledger state에서 cleanup이 실행되지 않습니다.

### AC07 — 모든 terminal kind가 fixed-high-water와 durable cursor로 backfill되며 canonical anti-join mismatch와 invalid identity가 0건입니다.

### AC08 — Historical coverage가 모든 replay floor보다 충분할 때만 durable completion marker가 기록됩니다.

### AC09 — Ledger-first logical resolution, deterministic owner, shared attempt budget, typed tracking/outcome 규칙이 pure tests로 고정됩니다.

### AC10 — 모든 lifecycle mutation이 exact fence와 version increment를 사용하며 group transition은 all-or-none입니다.

### AC11 — Provider success 이후 DB 오류에서 provider 재호출은 0회이고 owner/follower/tracking/ledger read-back mismatch는 atomicity breach입니다.

### AC12 — Poller/API source-only 경계가 CI로 고정되고 alarm-worker fanout/revive/aggregate owner만 남습니다.

### AC13 — Cleanup이 completed ledger state와 `terminal_at`을 사용하고 full row 삭제 뒤에도 logical terminal evidence가 남습니다.

### AC14 — CI architecture gate가 old store import와 worker 밖 lifecycle update를 거부합니다.

### AC15 — Focused unit/integration/fault-injection/race/schema tests와 승인된 rollout observation이 governing DEC의 verification evidence로 연결됩니다.

## Validation

### V01 — Writer와 fallback inventory

```bash
rg -n "UPDATE youtube_notification_(outbox|delivery)|DELETE FROM youtube_notification_outbox|ON CONFLICT|status[[:space:]]*=|attempt_count[[:space:]]*=|sent_at[[:space:]]*=" hololive/hololive-alarm-worker/internal/egress/youtubedispatch hololive/hololive-shared/pkg/service/youtube/outbox hololive/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo
rg -n "recoverSuccessfulCommunityShortsSentState|markRecoveredSentDeliveryRows" hololive/hololive-alarm-worker hololive/hololive-shared
```

### V02 — Current behavior characterization

```bash
go -C hololive/hololive-alarm-worker test ./internal/egress/youtubedispatch/...
go -C hololive/hololive-shared test ./pkg/service/youtube/poller/runtime/batchrepo/... ./pkg/service/youtube/tracking/observation/...
```

### V03 — Canonical identity contract

```bash
go -C hololive/hololive-shared test ./pkg/service/youtube/contentid/... ./pkg/service/youtube/poller/runtime/batchrepo/... ./pkg/service/youtube/tracking/observation/...
go -C hololive/hololive-alarm-worker test ./internal/egress/youtubedispatch/...
```

### V04 — Store ownership move

```bash
go -C hololive/hololive-alarm-worker test ./internal/egress/youtubedispatch/store/... ./internal/egress/youtubedispatch/...
go -C hololive/hololive-shared test ./pkg/service/youtube/outbox/...
bash scripts/architecture/check-shared-go-boundary.sh
```

### V05 — Migration manifest and schema snapshot

```bash
bash scripts/architecture/check-migration-manifest.sh
SCHEMA_SNAPSHOT_UPDATE=1 go -C hololive/hololive-dbtest test -run TestSchemaSnapshotGolden ./...
go -C hololive/hololive-dbtest test -run TestSchemaSnapshotGolden ./...
```

### V06 — Ledger schema/store

```bash
go -C hololive/hololive-alarm-worker test ./internal/egress/youtubedispatch/store/... -run 'Ledger|TerminalAt|Compatibility'
go -C hololive/hololive-dbtest test ./... -run 'Ledger|SchemaSnapshot'
```

### V07 — Compatibility cleanup gate

```bash
go -C hololive/hololive-alarm-worker test ./internal/egress/youtubedispatch/... -run 'Compatibility|Cleanup|CommitReadBack'
go -C hololive/hololive-shared test ./pkg/service/youtube/poller/runtime/batchrepo/... -run 'PollerBatchRepositoryDoesNotMutateDeliveryLifecycle|Compatibility'
rg -n "UPDATE youtube_notification_(outbox|delivery)|ON CONFLICT|status[[:space:]]*=|attempt_count[[:space:]]*=|sent_at[[:space:]]*=" hololive/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo
```

마지막 `rg`는 diagnostic inventory입니다. 새 architecture test가 delivery update와 existing outbox의 status/attempt/due/lock/sent/error assignment를 금지하며, high-water capture 전 그 결과를 writer audit evidence로 보존합니다.

### V08 — Backfill unit/integration

```bash
go -C hololive/hololive-alarm-worker test ./cmd/... ./internal/egress/youtubedispatch/... -run 'Backfill|HighWater|Coverage|AntiJoin'
```

### V09 — Backfill artifact

```bash
./build-all.sh alarm-worker
```

Image build script/tag는 구현 시 owning runbook의 current command로 확인하며 production transfer/deploy는 실행하지 않습니다.

### V10 — Lifecycle rules and preparation

```bash
go -C hololive/hololive-alarm-worker test ./internal/egress/youtubedispatch/lifecycle/... ./internal/egress/youtubedispatch/preparation/...
go -C hololive/hololive-alarm-worker test -race ./internal/egress/youtubedispatch/lifecycle/... ./internal/egress/youtubedispatch/preparation/...
```

### V11 — Transition store

```bash
go -C hololive/hololive-alarm-worker test ./internal/egress/youtubedispatch/store/... -run 'Transition|LogicalGroup|Tracking|Ledger|Adjudication'
```

### V12 — Runtime and fault injection

```bash
go -C hololive/hololive-alarm-worker test ./internal/egress/youtubedispatch/... -run 'Dispatcher|ResponseLost|Indeterminate|Atomicity|OutcomeUnknown'
go -C hololive/hololive-alarm-worker test -race ./internal/egress/youtubedispatch/...
```

### V13 — Fanout, revive, aggregate, cleanup

```bash
go -C hololive/hololive-alarm-worker test ./internal/egress/youtubedispatch/... -run 'Fanout|Revive|Aggregate|Cleanup'
```

### V14 — Poller writer removal

```bash
go -C hololive/hololive-shared test ./pkg/service/youtube/poller/runtime/batchrepo/...
go -C hololive/hololive-shared test ./pkg/service/youtube/poller/runtime/batchrepo/... -run PollerBatchRepositoryDoesNotMutateDeliveryLifecycle
rg -n "UPDATE youtube_notification_(outbox|delivery)|ON CONFLICT|status[[:space:]]*=|attempt_count[[:space:]]*=|sent_at[[:space:]]*=" hololive/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo
```

마지막 `rg`는 diagnostic inventory이며 test allowlist와 대조합니다. Source observation과 새 outbox insert만 허용합니다.

### V15 — Architecture and repository gates

```bash
bash scripts/architecture/ci-notification-egress-gate.sh
bash scripts/architecture/ci-boundary-gate.sh
bash scripts/architecture/check-file-loc.sh
```

### V16 — Publish gate

```bash
bash scripts/ci/local-ci.sh
./build-all.sh --build-only
```

Broad suite와 production build는 publish/deploy gate에서만 실행합니다. Production DB audit는 owning runbook의 guarded read 절차를 사용합니다.

## Rollout과 rollback

1. T02와 T03은 behavior-preserving code change로 먼저 배포할 수 있습니다.
2. T04 schema/compatibility rollout은 worker stop → additive migration → compatibility worker start 순서이며 별도 승인이 필요합니다.
3. T05 one-shot은 fixed high-water를 한 번만 잡고 persisted cursor로 재개합니다. 실패 시 state를 incomplete로 남기고 cleanup을 frozen 상태로 유지합니다.
4. T07 cutover는 completed ledger state와 focused fault-injection evidence가 있을 때만 승인 요청합니다.
5. T07 이후 rollback target은 T04 compatibility writer입니다. Ledger/state/`terminal_at`을 삭제하거나 과거 binary로 되돌리지 않습니다.
6. Immediate rollback trigger는 provider duplicate evidence, ledger/delivery mismatch, mixed group, commit indeterminate 급증, aggregate lag의 bounded threshold 초과입니다.
7. Rollback 뒤에도 outcome-unknown과 quarantine evidence를 success/failure로 추정하지 않습니다.

## 비목표

- Alarm-worker 다중 replica 활성화
- Provider transport fallback 또는 resend 추가
- Ledger 자동 retention
- Tracking row를 room-level delivery receipt로 재해석
- External lifecycle/FSM dependency 도입
- 삭제된 historical row를 추정으로 복원
- Production migration, backfill, deploy, restart를 이 계획 승인으로 간주

## Execution capsule
Goal: YouTube egress를 ledger-first logical lifecycle과 단일 alarm-worker transition owner로 교체합니다.
Context: Governing DEC와 네 architecture 문서, current schema/store/poller writer inventory를 정본으로 사용합니다.
Constraints: Canonical identity는 fail closed이고 dual writer, provider resend, unbounded migration backfill, incomplete-ledger cleanup을 금지합니다.
Evidence: T01 writer/coverage audit, fixed-high-water canonical anti-join, commit fault injection, architecture gate를 남깁니다.
Success: AC01부터 AC15까지 충족하고 지원 schema version의 durable ledger completion marker가 존재합니다.
Output: T02부터 T09까지 순차 PR, 승인된 rollout 기록, governing DEC verification evidence를 제출합니다.
