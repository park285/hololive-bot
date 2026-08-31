# YouTube egress lifecycle 전이 소유권

작성일: 2026-08-31 KST  
결정 ID: `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership`  
대상 런타임: `hololive-alarm-worker`  
결정 상태 정본: [`docs/decisions/records/DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership.json`](../../decisions/records/DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership.json)

## 목적

이 문서는 `youtube_notification_outbox`와 `youtube_notification_delivery`의 상태 변경 책임을 어디에 둘지 확정합니다. 상태와 이벤트의 세부 규약은 [`youtube-egress-lifecycle-contract-20260831.md`](youtube-egress-lifecycle-contract-20260831.md), 실제 작업 순서는 [`2026-08-31-youtube-egress-lifecycle-implementation.md`](../plans/2026-08-31-youtube-egress-lifecycle-implementation.md)가 정본입니다.

현재 코드는 PostgreSQL의 행 상태와 잠금 조건을 이용해 중복 발송을 방어하고 있지만, 전이의 의미는 Go 서비스와 repository, SQL 조건식에 분산되어 있습니다. 이 문서의 목표는 DB의 원자성과 복구 능력을 제거하는 것이 아니라, **정책 결정과 전이 집행을 분리하고 각 writer의 권한을 명시하는 것**입니다.

## 결정

### D-001. alarm-worker 내부의 typed lifecycle policy가 전이를 결정한다

YouTube egress delivery의 허용 전이, retry 소진 판단, preparation 실패와 send 실패의 구분, revive 허용 조건은 `hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle`이 소유합니다.

```text
현재 snapshot + typed event + policy + 명시적 시각
    -> typed decision 또는 명시적 거부
```

policy는 다음 제약을 지킵니다.

- DB, network, logger, metric에 접근하지 않습니다.
- `time.Now()`를 직접 호출하지 않습니다.
- 입력 구조체를 변경하지 않습니다.
- 오류 메시지 문자열을 전이 분류 키로 사용하지 않습니다.
- 범용 사내 FSM framework를 만들지 않습니다.

### D-002. PostgreSQL은 durable state와 전이 집행을 소유한다

PostgreSQL은 다음 항목의 원본입니다.

- delivery/outbox의 현재 상태
- attempt와 due 시각
- active claim 시각
- delivery의 fencing version
- 성공 tracking과 aggregate projection

repository는 policy가 만든 decision을 임의로 재해석하지 않고, expected state와 fencing token이 여전히 유효할 때만 적용합니다.

```text
policy: 어디로 가야 하는가
repository: 아직 그 전제를 만족하는가
PostgreSQL: 적용과 transaction을 원자적으로 완료할 수 있는가
```

### D-003. delivery에는 `row_version`을 도입하고 outbox에는 이번 범위에서 도입하지 않는다

`youtube_notification_delivery.row_version BIGINT NOT NULL DEFAULT 0`을 추가합니다. claim과 모든 delivery 상태 변경은 version을 1 증가시키며, 후속 writer는 이전 단계가 반환한 version을 expected value로 사용합니다.

`locked_at`은 다음 책임을 계속 가집니다.

- claim 또는 `SENDING` phase의 시작 시각
- stale 판단과 운영 관측
- 전환 기간의 보조 fencing 조건

`row_version`은 stale writer를 차단하는 주 fencing token입니다. version은 인덱싱하지 않습니다. 모든 상태 변경에서 갱신되는 값을 인덱싱하면 HOT update 가능성을 낮추고 쓰기 증폭만 늘기 때문입니다.

Outbox는 이번 변경에서 `row_version`을 추가하지 않습니다. pre-fanout outbox는 기존 `status + locked_at` claim token과 transaction으로 보호하고, child가 생긴 뒤에는 aggregate projector가 상태를 계산합니다. outbox version까지 추가하는 것은 현재 문제 해결에 필수적이지 않으며 aggregate writer와의 의미만 복잡하게 만듭니다.

### D-004. 외부 발송은 상태머신 effect나 DB transaction callback 안에서 실행하지 않는다

Iris/Kakao 발송은 PostgreSQL rollback으로 취소할 수 없습니다. 따라서 lifecycle policy, repository callback, state entry/exit hook 안에 provider 호출을 넣지 않습니다.

```text
local preparation 완료
-> PENDING에서 SENDING으로 fenced CAS
-> 외부 provider 호출
-> typed outcome 분류
-> 확정 가능한 outcome만 후속 fenced CAS
```

`SENDING`은 “provider 호출을 시작할 수 있도록 send intent가 durable하게 확정되었다”는 뜻입니다. 실제 socket write가 일어났다는 증거는 아닙니다. `SENDING` commit 직후 process가 죽으면 실제 호출 전이더라도 보수적으로 `QUARANTINED`가 될 수 있습니다. 자동 중복 발송 방지를 위해 이 좁은 false-positive quarantine을 허용합니다.

### D-005. outcome unknown은 즉시 상태 전이가 아니다

provider가 메시지를 처리했는지 증명할 수 없는 timeout, connection reset, process failure는 `OutcomeUnknown`으로 분류합니다.

`OutcomeUnknown`을 받은 application service는 delivery를 `PENDING`, `FAILED`, `QUARANTINED`로 즉시 쓰지 않습니다. 행은 `SENDING` 상태로 남습니다. 이후 stale-sending sweeper가 `SendingLeaseExpired`를 집행해 `QUARANTINED`로 이동시킵니다.

이 규칙은 다음 두 문제를 피합니다.

1. timeout을 retry로 오판해 이미 전달된 메시지를 다시 보내는 문제
2. 실제 provider 결과를 알 수 없는데도 즉시 terminal 판정을 만드는 문제

### D-006. generic mutation DSL 대신 의도별 transition command를 사용한다

repository에 임의의 `nextStatus`, nullable patch map, generic `ApplyTransition`을 노출하지 않습니다. 그런 API는 상태머신 책임을 repository 호출자에게 다시 확산시킵니다.

application service는 policy decision을 종류별 command로 변환하여 다음과 같은 의도별 API를 호출합니다.

- `BeginSendingBatch`
- `ScheduleRetryBatch`
- `FailBatch`
- `CompleteSentBatch`
- `QuarantineStaleSending`
- `ReviveFailedOutboxes`

각 command constructor가 source state, required token, timestamp shape를 검증합니다. repository SQL은 해당 command의 expected state와 version만 집행합니다.

### D-007. preparation과 send의 실패 의미를 분리한다

`PENDING + active claim` 상태에서 발생한 실패는 provider 호출 전 실패입니다.

- payload/outbox load, formatting, request construction 실패
- provider 호출 전에 확인된 context cancellation
- local dependency 실패

`SENDING` 이후 발생한 실패는 provider 호출 결과입니다.

- provider가 요청을 받지 않았다고 확정한 retryable/permanent 오류
- provider 성공
- 처리 여부 불명

두 phase는 같은 `RetrySafeFailure` 이벤트로 뭉치지 않습니다. 전이 이름과 failure code가 source phase를 드러내야 운영자가 “외부 부수효과 가능성이 있었는가”를 판단할 수 있습니다.

### D-008. delivery 생성 후 outbox 상태 writer는 aggregate projector뿐이다

Outbox에는 두 단계가 있습니다.

```text
pre-fanout intent
    delivery row가 아직 없음

post-fanout aggregate
    delivery row가 하나 이상 있음
```

pre-fanout 단계에서는 `OutboxFanoutService`만 다음 작업을 수행할 수 있습니다.

- 대상 방이 없으면 `PENDING -> SENT`
- fanout 준비 실패면 retry 또는 `FAILED`
- 대상 방이 있으면 delivery 생성과 claim 해제를 한 transaction으로 수행

child delivery가 하나라도 생긴 뒤에는 outbox `status`, aggregate error, aggregate `sent_at`을 atomic aggregate SQL만 변경합니다. 직접 outbox finalization SQL은 active outbox claim token을 검사하고 같은 transaction에서 `NOT EXISTS (child delivery)`를 확인해야 합니다.

### D-009. worker 전용 persistence 구현을 alarm-worker 내부로 회수한다

현재 `hololive-shared/pkg/service/youtube/outbox/store`의 production 소비자는 alarm-worker 하나입니다. `DEC-20260825-hololive-shared-public-path-scoped-retention`에 따라 single-owner 실행 구현은 다음 위치로 단계적으로 이동합니다.

```text
hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store
```

다음 항목만 shared에 남길 수 있습니다.

- 실제 cross-runtime row 계약
- shared 내부의 진성 다중 소비자 타입
- 범용 pgx/db helper

worker 전용 claim, transition SQL, aggregate sync, tracking transaction 구현은 internal owner가 소유합니다. 이동은 단일 PR에서 package copy와 dual implementation을 만들지 않고, import cutover와 기존 package 삭제를 같은 변경 묶음에서 수행합니다.

### D-010. 범용 FSM 라이브러리를 현재 도입하지 않는다

조사한 후보 중 `open-ships/statemachine`의 immutable `Machine.Next` 모델이 가장 가까웠습니다. 그러나 홀로봇이 필요한 결과는 목적 상태 하나가 아니라 다음을 포함한 complete mutation decision입니다.

- 선택된 rule identity
- expected state/version/attempt
- next attempt와 due 시각
- claim 해제 또는 유지
- success tracking mutation
- failure classification

목적 상태 선택과 mutation plan 생성에 같은 정책을 두 번 표현하거나, guard가 mutable side channel을 사용해야 한다면 라이브러리 도입 이득이 사라집니다. 성숙한 `qmuntal/stateless`와 `looplab/fsm`은 state-owning/callback 실행 모델이 PostgreSQL source-of-truth와 맞지 않았고, structured transition과 PostgreSQL CAS를 가장 잘 제공한 다른 후보는 archived 상태였습니다.

따라서 도메인 전용 policy를 직접 구현하되 다음 조건으로 재검토합니다.

- caller-owned state를 지원합니다.
- 순수 planning 결과에 transition ID와 structured metadata가 포함됩니다.
- persistence가 optimistic conflict를 일급 결과로 표현합니다.
- 다중 maintainer와 production adoption이 확인됩니다.
- adapter를 포함한 net policy code가 직접 구현보다 실질적으로 줄어듭니다.
- 동일 contract test를 변경 없이 통과합니다.

### D-011. 이번 결정은 alarm-worker 수평확장 승인이 아니다

Delivery row fencing을 강화해도 canonical group claim, background loop 조율, local fallback 등 replica>1 게이트는 별개입니다. `hololive-alarm-worker`는 [`alarm-egress-scale-out-decisions-20260730.md`](alarm-egress-scale-out-decisions-20260730.md)의 결정대로 단일 인스턴스를 유지합니다.

## 현재 코드에서 확인된 문제

### 전이 정책이 여러 층에 분산되어 있다

현재 전이 의미는 다음 위치에 나뉘어 있습니다.

- `status_updater.go`: outbox attempt 증가와 retry/FAILED 선택
- `claim_manager_pipeline.go`: failure reason 문자열을 permanent/retry로 재분류
- `delivery_repository_lock.go`: `PENDING`, `SENDING`, `SENT`, `FAILED`, `QUARANTINED` 상태 변경
- `delivery_repository_lock_0190_03.sql`: max retry 판단과 목적 상태 선택
- `claim_manager_revive.go`: freshness, never-sent, child 상태를 이용한 revive 판단

SQL의 `CASE`가 retry policy를 결정하고 Go가 다시 failure 종류를 선택하기 때문에 전체 transition table을 한 파일에서 검토할 수 없습니다.

### 현재 동시성 방어는 보존해야 한다

현재 delivery writer는 `status + locked_at` exact match를 사용합니다. microsecond 단위로 다른 stale token이 후속 상태를 덮어쓰지 못하는 통합 테스트와, 실제 CAS에 성공한 delivery만 tracking을 갱신하는 transaction test가 있습니다.

리팩터링은 이 방어를 없애는 작업이 아닙니다. `row_version`을 추가하여 token identity를 시간값 하나에만 의존하지 않게 하고, 기존 exact lock predicate는 전환 기간의 이중 방어로 유지합니다.

### outcome-unknown 경계는 보존하고 명시화해야 한다

현재 SendEngine은 outcome unknown 오류를 일반 failure bucket에 넣지 않고 `SENDING + locked`로 남겨 stale sweeper가 격리하게 합니다. 이 동작은 중복 방지를 위한 핵심 안전 속성입니다. typed outcome으로 바꾸더라도 의미는 바뀌지 않아야 합니다.

### aggregate SQL은 원자적으로 유지해야 한다

Outbox aggregate는 child 상태를 한 SQL statement에서 계산하고 갱신합니다. active child가 있으면 `PENDING`, active child가 없고 `FAILED` 또는 `QUARANTINED`가 있으면 `FAILED`, 모두 완료됐으면 `SENT`로 계산합니다.

이를 Go에서 count한 뒤 별도 update로 바꾸면 동시 갱신 사이에 stale aggregate가 상태를 되돌릴 수 있습니다. aggregate 의미는 문서와 테스트로 고정하되 집행은 atomic SQL에 남깁니다.

## 목표 아키텍처

```text
┌─────────────────────────────────────────────────────────────┐
│ Send preparation / transport adapter                        │
│                                                             │
│ local preparation result or typed provider outcome          │
└───────────────────────────┬─────────────────────────────────┘
                            │ typed event
                            ▼
┌─────────────────────────────────────────────────────────────┐
│ alarm-worker/internal/.../youtubedispatch/lifecycle         │
│                                                             │
│ snapshot + event + policy + now -> typed decision           │
│ no DB, no network, no logging, no mutable guard              │
└───────────────────────────┬─────────────────────────────────┘
                            │ intent-specific command
                            ▼
┌─────────────────────────────────────────────────────────────┐
│ DeliveryTransitionService / OutboxFanoutService             │
│                                                             │
│ partitions decisions, emits metrics, owns orchestration      │
└───────────────────────────┬─────────────────────────────────┘
                            │ command batch
                            ▼
┌─────────────────────────────────────────────────────────────┐
│ alarm-worker/internal/.../youtubedispatch/store             │
│                                                             │
│ status + row_version + locked_at CAS                         │
│ success tracking transaction                                │
│ atomic outbox aggregate projection                          │
└───────────────────────────┬─────────────────────────────────┘
                            ▼
                       PostgreSQL
```

## 책임 행렬

| 책임 | 소유자 | 금지 사항 |
|---|---|---|
| state/event/failure vocabulary | lifecycle package | raw error 문자열을 정책 key로 사용하지 않음 |
| transition/retry/revive 결정 | lifecycle policy | DB 조회, I/O, clock 직접 호출 금지 |
| provider error 분류 | transport adapter | planner가 SDK error 문자열을 해석하지 않음 |
| claim 대상 선정 | internal store | planner가 `FOR UPDATE SKIP LOCKED`를 모델링하지 않음 |
| state/version CAS | internal store | unconditional status update 금지 |
| 외부 발송 | SendEngine | DB transaction 안에서 provider 호출 금지 |
| tracking 원자성 | `CompleteSentBatch` transaction | 적용 실패 ID의 tracking 변경 금지 |
| aggregate 계산 | atomic SQL projector | Go count 후 별도 update 금지 |
| transition metric/log | transition service | policy 내부 side effect 금지 |

## 안전성과 가용성 선택

이 설계는 외부 provider와 PostgreSQL 사이에 분산 transaction이 없다는 사실을 숨기지 않습니다.

### 보장하는 안전 속성

1. stale worker는 새 version의 row를 finalize할 수 없습니다.
2. `SENT`는 자동 경로에서 다른 상태로 되돌아가지 않습니다.
3. outcome unknown은 자동 retry되지 않습니다.
4. success tracking은 실제 `SENDING -> SENT` CAS에 성공한 delivery만 변경합니다.
5. child가 생성된 outbox는 직접 writer가 상태를 변경하지 않습니다.
6. retry 소진 판단은 lifecycle policy 한 곳에서만 수행합니다.

### 의도적으로 포기하는 자동 가용성

provider 결과가 불명확하면 중복 방지와 자동 복구를 동시에 보장할 수 없습니다. 이 경우 자동 재발송을 포기하고 `QUARANTINED`에 남깁니다. 수동 replay 또는 provider reconciliation을 추가하려면 duplicate-risk acknowledgement와 immutable audit를 포함한 별도 결정이 필요합니다.

## package 경계

최종 목표 구조는 다음과 같습니다.

```text
hololive/hololive-alarm-worker/internal/egress/youtubedispatch/
├── lifecycle/
│   ├── status.go
│   ├── event.go
│   ├── failure.go
│   ├── delivery_policy.go
│   ├── outbox_policy.go
│   ├── decision.go
│   └── *_test.go
├── store/
│   ├── delivery_claim.go
│   ├── delivery_transition.go
│   ├── delivery_sent_tx.go
│   ├── outbox_fanout.go
│   ├── outbox_aggregate.go
│   ├── revive.go
│   ├── queries/
│   └── *_test.go
├── delivery_transition_service.go
├── outbox_fanout_service.go
├── send_outcome.go
└── transition_metrics.go
```

`hololive-shared/pkg/domain`의 현재 row 구조체와 status 타입을 한 번에 모두 제거하지는 않습니다. 구현 plan은 먼저 typed lifecycle을 도입하고, production 소비자가 하나뿐인 store 구현을 internal로 옮긴 뒤, 실제 cross-runtime 계약만 shared에 남기는 순서로 진행합니다.

## 스키마 범위

이번 결정에서 필수인 schema 변경은 다음 하나입니다.

```sql
ALTER TABLE youtube_notification_delivery
ADD COLUMN IF NOT EXISTS row_version bigint NOT NULL DEFAULT 0;
```

추가 migration은 repository의 migration 규약을 따릅니다.

- 새 migration 번호와 `manifest.txt`를 갱신합니다.
- constant default를 사용하고 `row_version` index를 만들지 않습니다.
- schema snapshot golden을 갱신합니다.
- production writer cutover 전에 모든 scanner와 test fixture가 column을 읽도록 합니다.

다음 컬럼은 이번 책임 분리에 필수적이지 않으므로 추가하지 않습니다.

- outbox `row_version`
- delivery/outbox `state_changed_at`
- 별도 `error_code`
- 별도 `send_started_at`

운영 근거가 생기면 독립 결정과 migration으로 검토합니다.

## 배포 호환성

| 단계 | schema | 실행 binary | 허용 여부 |
|---|---|---|---|
| migration 전 | row_version 없음 | 기존 binary | 허용 |
| migration 후 | row_version 기본 0 | 기존 binary | 허용. 기존 binary는 column을 무시함 |
| cutover 후 | row_version 사용 | 새 binary 단일 인스턴스 | 목표 상태 |
| rollback | row_version 잔존 | 기존 binary 단일 인스턴스 | 허용. column은 additive |
| old/new 동시 writer | 어느 schema든 | 두 binary 동시 egress | 금지 |

현재 Compose는 alarm-worker 단일 인스턴스이므로 배포는 replacement 방식으로 수행합니다. schema rollback을 위해 column을 즉시 삭제하지 않습니다. 새 binary rollback 후에도 additive column은 유지합니다.

## 구현 및 검증 정본

구체적인 state/event/attempt/token/CAS 규약:

- [`youtube-egress-lifecycle-contract-20260831.md`](youtube-egress-lifecycle-contract-20260831.md)

PR 순서, 파일 단위 작업, acceptance와 rollback:

- [`2026-08-31-youtube-egress-lifecycle-implementation.md`](../plans/2026-08-31-youtube-egress-lifecycle-implementation.md)

관련 운영 경계:

- [`alarm-egress-scale-out-decisions-20260730.md`](alarm-egress-scale-out-decisions-20260730.md)
- [`repository-ownership.md`](repository-ownership.md)
- [`../services/alarm-worker.md`](../services/alarm-worker.md)

## 재검토 조건

다음 중 하나가 발생하면 이 결정을 다시 검토합니다.

1. provider가 request ID 기반 결과 조회 또는 명시적 idempotent replay 계약을 제공합니다.
2. `QUARANTINED` 수동 처리 요구가 반복되어 audited reconciliation workflow가 필요해집니다.
3. lifecycle 상태와 이벤트가 크게 늘어 직접 policy보다 검증된 FSM library adapter가 작아집니다.
4. YouTube delivery store에 두 번째 production runtime 소비자가 생깁니다.
5. alarm-worker replica>1 게이트가 별도 결정으로 모두 해소됩니다.
