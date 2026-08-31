# YouTube egress lifecycle 전이 소유권 및 구현 설계

작성일: 2026-08-31 KST  
결정 ID: `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership`  
대상 런타임: `hololive-alarm-worker`  
대상 저장소: `park285/hololive-bot`  
상태: 결정 승인, 구현 예정

---

## 이 문서의 목적

`youtube_notification_outbox`와 `youtube_notification_delivery`의 상태 변경 규칙이 Go 서비스, repository 메서드, SQL 조건식에 나뉘어 있는 현재 구조를 명시적인 lifecycle 경계로 재구성합니다. 이 문서는 구현자가 추가 설계 결정을 하지 않고 작업을 시작할 수 있도록 다음 사항을 고정합니다.

- 상태와 이벤트의 의미
- Go와 PostgreSQL 사이의 책임 경계
- 허용 전이와 금지 전이
- retry, permanent failure, outcome-unknown, quarantine, revive 정책
- outbox intent와 delivery aggregate의 쓰기 소유권
- target package/API/SQL 구조
- 단계별 구현 순서와 rollback 경계
- 테스트, 관측성, 완료 조건
- 외부 FSM 라이브러리를 사용하지 않는 이유와 재검토 조건

이 문서는 YouTube notification egress lifecycle의 current-layer SSOT입니다. 구현 중 상충하는 기존 주석이나 메서드명이 있으면 이 문서와 결정 레코드를 우선합니다. 단, 기존 외부 알림 계약과 DB 데이터 의미를 임의로 바꾸는 권한은 부여하지 않습니다.

---

## 결정

### D-001. 상태 전이 정책은 alarm-worker 내부의 typed planner가 소유한다

`hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle`에 도메인 전용 planner를 둡니다.

```text
DeliverySnapshot + DeliveryEvent + DeliveryPolicy
    -> DeliveryDecision
```

planner는 순수 함수입니다. DB, network, logger, metric, 전역 clock에 접근하지 않으며 입력을 변경하지 않습니다. 상태와 이벤트를 범용화한 사내 FSM framework를 만들지 않습니다.

### D-002. PostgreSQL은 durable state와 전이 집행을 소유한다

planner는 전이를 **결정**하고 repository는 전이를 **집행**합니다. repository는 다음 조건을 원자적으로 검증합니다.

```text
entity ID
expected status
expected row version
expected attempt count
필요한 경우 active claim token
```

DB 갱신 결과는 `Applied`, `Conflict`, `Missing`으로 구분합니다. 0 rows affected를 성공으로 숨기지 않습니다.

### D-003. 외부 메시지 발송은 planner와 DB transaction callback 밖에 둔다

Iris/Kakao 호출은 PostgreSQL rollback으로 취소할 수 없습니다. 따라서 transition callback이나 state entry action 안에 외부 send를 넣지 않습니다.

```text
local preparation
-> PENDING to SENDING CAS
-> external send
-> typed outcome
-> SENDING to final state CAS
```

외부 발송 결과가 불명확하면 자동 retry하지 않고 `SENDING`을 유지한 뒤 stale-sending sweeper가 `QUARANTINED`로 격리합니다.

### D-004. 범용 FSM 라이브러리는 현재 도입하지 않는다

조사한 라이브러리 중 caller-owned state와 순수 transition selection에 가장 가까운 후보는 `open-ships/statemachine`이었습니다. 그러나 현재 API는 목적 상태를 중심으로 반환하며, 홀로봇에 필요한 attempt, backoff, error code, sent timestamp, tracking mutation, expected version을 포함한 완전한 `DeliveryDecision`은 별도로 작성해야 합니다.

상태 선택과 decision builder에 동일 정책을 중복시키거나 mutable guard를 사용하지 않기 위해 직접 planner를 채택합니다. 라이브러리 교체 가능성은 `DeliveryPlanner` 인터페이스와 contract test로 보존합니다.

### D-005. 하나의 거대 상태머신을 만들지 않는다

세 가지 lifecycle을 분리합니다.

1. **Outbox intent lifecycle**: 방별 delivery가 생성되기 전 fan-out 의도와 실패를 관리합니다.
2. **Per-room delivery lifecycle**: 외부 발송, retry, quarantine, reconciliation을 관리합니다.
3. **Community/Shorts alarm state**: `authorized_at`, `alarm_sent_at` 사실에서 상태를 계산하는 projection으로 유지합니다.

Outbox aggregate는 child delivery 상태에서 계산하는 projection이므로 Go FSM으로 옮기지 않습니다.

### D-006. delivery가 존재한 뒤 outbox status의 writer는 aggregate projector뿐이다

- delivery가 없는 outbox는 `OutboxIntentTransitionService`가 직접 전이할 수 있습니다.
- delivery가 하나라도 존재하면 `UpdateOutboxAggregateStatuses`만 outbox `status`, `sent_at`, aggregate error를 갱신할 수 있습니다.
- 직접 outbox 전이 SQL에는 `NOT EXISTS (delivery ...)` 가드를 둡니다.

### D-007. `SENT`는 자동 경로의 절대 terminal이며 `QUARANTINED`는 자동 revive하지 않는다

- `SENT -> *` 자동 전이는 금지합니다.
- `QUARANTINED -> PENDING` 자동 전이는 금지합니다.
- `QUARANTINED`는 provider 증거를 가진 명시적 reconciliation만 `SENT`, `PENDING`, `FAILED`로 이동시킬 수 있습니다.
- 기존 `FAILED -> PENDING` revive는 freshness와 never-sent 및 reset 가능한 FAILED child 조건을 계속 요구합니다.

### D-008. 이번 작업은 alarm-worker replica 확대 승인이 아니다

행 단위 lifecycle을 더 명확하게 만들어도 canonical group claim, background loop 조율, local fallback 등 replica>1 게이트는 별개입니다. `hololive-alarm-worker`는 `alarm-egress-scale-out-decisions-20260730.md`의 결정대로 단일 인스턴스를 유지합니다.

---

## 현재 구조와 변경 이유

### 전이 정책이 여러 층에 분산되어 있다

- `internal/egress/youtubedispatch/status_updater.go`는 outbox를 읽고 attempt를 증가시키며 retry와 permanent failure를 직접 선택합니다.
- `pkg/service/youtube/outbox/store/delivery_repository_lock.go`는 delivery 전송 상태를 변경합니다.
- `delivery_repository_lock_0190_03.sql`은 `attempt_count + 1 >= maxRetries`를 평가해 `PENDING`과 `FAILED`를 직접 선택합니다.
- `claim_manager_pipeline.go`는 failure reason 문자열을 다시 해석해 permanent/retry repository 메서드를 선택합니다.
- `claim_manager_revive.go`는 freshness, sent 여부, lock 만료, FAILED child 존재 여부를 조합해 revive를 결정합니다.

각 로직은 개별적으로 합리적이지만, 전체 허용 전이를 확인하려면 여러 Go 파일과 SQL을 함께 읽어야 합니다.

### 현재 동시성 불변식은 보존해야 한다

현재 구현은 `status + locked_at` exact match로 stale worker를 막습니다. microsecond 단위로 다른 stale token이 전이를 수행하지 못하는 통합 테스트도 있습니다. 이 방어선은 planner 추출 과정에서 약화하면 안 됩니다.

### outcome-unknown 경계는 이미 올바른 방향이다

`send_engine_support.go`는 timeout 또는 외부 결과 불명 오류를 일반 failure bucket에 넣지 않고 `SENDING + locked` 상태로 남깁니다. 이후 `QuarantineStaleSending`이 `QUARANTINED`로 격리합니다. 이 정책을 typed outcome과 명시적 transition rule로 승격합니다.

### aggregate SQL은 원자적으로 유지해야 한다

`delivery_repository_aggregate_sync.sql`은 active child, failed/quarantined child, sent child 순으로 outbox 상태를 계산합니다. 과거 count 후 update 방식의 lost update를 방지하기 위한 원자적 projection이므로 Go planner로 이전하지 않습니다.

---

## 책임 경계

| 책임 | 소유자 | 금지 사항 |
|---|---|---|
| 상태·이벤트·실패 코드 정의 | lifecycle/domain types | 오류 문자열을 상태 분류 키로 사용하지 않음 |
| 허용 전이와 retry/revive 정책 | `DeliveryPlanner`, `OutboxIntentPlanner` | DB 조회, I/O, `time.Now()` 호출 금지 |
| 현재 상태와 claim 조회 | PostgreSQL repository | 정책 판단 금지 |
| expected state/version CAS | PostgreSQL repository | unconditional status update 금지 |
| 외부 메시지 발송 | `SendEngine` | DB transaction 안에서 provider 호출 금지 |
| success tracking 원자성 | delivery repository transaction | delivery 적용 실패 ID의 tracking 변경 금지 |
| outbox aggregate 계산 | atomic aggregate SQL | Go에서 child count 후 별도 update 금지 |
| stale `SENDING` 격리 | sweeper + planner/store | outcome unknown을 일반 retry로 변경 금지 |
| 운영 로그와 metric | transition service | planner 내부 side effect 금지 |

---

## 상태 타입

DB 문자열 값은 변경하지 않되 Go 타입을 분리합니다.

```go
type YouTubeOutboxStatus string

type YouTubeDeliveryStatus string

const (
    YouTubeOutboxPending YouTubeOutboxStatus = "PENDING"
    YouTubeOutboxSent    YouTubeOutboxStatus = "SENT"
    YouTubeOutboxFailed  YouTubeOutboxStatus = "FAILED"
)

const (
    YouTubeDeliveryPending     YouTubeDeliveryStatus = "PENDING"
    YouTubeDeliverySending     YouTubeDeliveryStatus = "SENDING"
    YouTubeDeliverySent        YouTubeDeliveryStatus = "SENT"
    YouTubeDeliveryFailed      YouTubeDeliveryStatus = "FAILED"
    YouTubeDeliveryQuarantined YouTubeDeliveryStatus = "QUARANTINED"
)
```

`SENDING`과 `QUARANTINED`를 store 로컬 상수로 두지 않습니다. outbox와 delivery가 같은 status 타입을 공유하지 않게 하여 잘못된 상태 대입을 컴파일 단계에서 막습니다.

---

## Per-room delivery lifecycle

### 상태 의미

| 상태 | 의미 | 자동 claim 대상 |
|---|---|---:|
| `PENDING` | 발송 전 또는 안전한 retry 대기. `locked_at`이 있으면 현재 worker가 준비 중 | lock이 없거나 만료되고 due일 때만 |
| `SENDING` | 외부 provider 호출을 시작할 수 있는 준비가 끝났고, 호출 결과가 확정되기 전 | 아니오 |
| `SENT` | provider 성공 결과를 로컬 transaction으로 확정 | 아니오 |
| `FAILED` | permanent failure 또는 retry 소진 | 아니오. guarded revive만 가능 |
| `QUARANTINED` | provider가 처리했는지 로컬에서 증명할 수 없음 | 아니오. 명시적 reconciliation만 가능 |

### claim은 lifecycle status가 아니라 concurrency protocol이다

`FetchAndLock`은 `PENDING -> PENDING` 상태 전이가 아니라 작업 lease 획득입니다. planner가 claim 대상을 고르지 않습니다. repository가 due/lock 조건과 `FOR UPDATE SKIP LOCKED`로 claim하고 `ClaimToken`을 반환합니다.

```go
type DeliveryClaimToken struct {
    DeliveryID int64
    LockedAt   time.Time
    RowVersion int64
}
```

`locked_at`은 TTL과 운영 관측에 사용하고, `row_version`은 stale writer fencing에 사용합니다.

### 이벤트

```go
type DeliveryEventKind uint8

const (
    DeliveryEventBeginSend DeliveryEventKind = iota + 1
    DeliveryEventRetrySafeFailure
    DeliveryEventPermanentFailure
    DeliveryEventDelivered
    DeliveryEventOutcomeUnknown
    DeliveryEventSendingLeaseExpired
    DeliveryEventRevive
    DeliveryEventReconcileDelivered
    DeliveryEventReconcileNotDelivered
)
```

### typed outcome

SendEngine은 문자열 bucket 대신 delivery별 outcome을 반환합니다.

```go
type DeliveryOutcomeKind uint8

const (
    DeliveryOutcomeDelivered DeliveryOutcomeKind = iota + 1
    DeliveryOutcomeRetrySafe
    DeliveryOutcomePermanentFailure
    DeliveryOutcomeUnknown
)

type DeliveryOutcome struct {
    DeliveryID int64
    OutboxID   int64
    Kind       DeliveryOutcomeKind
    Code       DeliveryFailureCode
    Message    string
    RetryAfter time.Duration
    ClaimToken DeliveryClaimToken
    ClaimMarks []dispatchstate.ClaimToken
}
```

`Message`는 로그와 운영 진단용입니다. planner는 `Kind`와 `Code`를 사용하며 문자열 비교를 하지 않습니다.

### retry-safe의 정의

다음 중 하나를 증명할 수 있을 때만 `DeliveryOutcomeRetrySafe`입니다.

1. provider 호출이 시작되기 전에 실패했습니다.
2. provider가 요청을 수락하지 않았다고 명시적으로 응답했습니다.
3. 동일한 stable client request ID 재사용 시 provider가 중복 부수효과를 차단한다는 계약이 있습니다.

timeout, connection reset, 프로세스 종료 등 provider 처리 여부를 증명하지 못하면 `DeliveryOutcomeUnknown`입니다.

### 허용 전이표

| From | Event | To/Action | 핵심 조건 | attempt 변화 |
|---|---|---|---|---:|
| `PENDING` | `BeginSend` | `SENDING` | active claim, request 준비 완료 | 유지 |
| `PENDING` | `RetrySafeFailure` | `PENDING` | 다음 attempt가 max 미만 | +1 |
| `PENDING` | `RetrySafeFailure` | `FAILED` | 다음 attempt가 max 이상 | +1 |
| `PENDING` | `PermanentFailure` | `FAILED` | payload/target 등 재시도 불가능 | +1 |
| `SENDING` | `Delivered` | `SENT` | active send token | 유지 |
| `SENDING` | `RetrySafeFailure` | `PENDING` | 안전한 retry 증거, retry 잔여 | +1 |
| `SENDING` | `RetrySafeFailure` | `FAILED` | 안전한 retry 증거, retry 소진 | +1 |
| `SENDING` | `PermanentFailure` | `FAILED` | provider permanent rejection | +1 |
| `SENDING` | `OutcomeUnknown` | hold `SENDING` | 자동 DB mutation 없음 | 유지 |
| `SENDING` | `SendingLeaseExpired` | `QUARANTINED` | `locked_at < cutoff` | +1 |
| `FAILED` | `Revive` | `PENDING` | revive guard 충족 | 0으로 reset |
| `QUARANTINED` | `ReconcileDelivered` | `SENT` | provider 성공 증거 | 유지 |
| `QUARANTINED` | `ReconcileNotDelivered` | `PENDING` 또는 `FAILED` | provider 미처리 증거와 retry 정책 | 정책에 따름 |

표에 없는 조합은 모두 거부합니다. 특히 다음은 금지합니다.

```text
SENT + any automatic event
QUARANTINED + Revive
PENDING + Delivered
FAILED + BeginSend
```

### outcome unknown의 적용 방식

`OutcomeUnknown`은 성공한 DB 전이가 아닙니다. planner는 `Hold` 결정을 반환하고 transition service는 metric과 warning log만 기록합니다.

```go
type DeliveryDecisionAction uint8

const (
    DeliveryDecisionApply DeliveryDecisionAction = iota + 1
    DeliveryDecisionHold
)
```

`Hold`에서는 status, version, attempt, lock을 바꾸지 않습니다. stale-sending sweeper가 별도의 `SendingLeaseExpired` 이벤트를 만들어 `QUARANTINED`를 적용합니다.

---

## Outbox intent lifecycle

### 쓰기 소유권 분기

```text
child delivery 0개
    -> OutboxIntentTransitionService가 직접 status 전이 가능

child delivery 1개 이상
    -> OutboxAggregateProjector만 status 전이 가능
```

### pre-fanout 이벤트

| From | Event | To | 설명 |
|---|---|---|---|
| `PENDING` | `NoTargets` | `SENT` | 전송 대상이 없으므로 정상 완료 |
| `PENDING` | `FanoutRetrySafeFailure` | `PENDING` | subscriber 조회 또는 enqueue 일시 실패 |
| `PENDING` | `FanoutRetrySafeFailure` | `FAILED` | retry 소진 |
| `PENDING` | `FanoutPermanentFailure` | `FAILED` | 복구 불가능한 intent/payload 오류 |
| `PENDING` | `FanoutMaterialized` | `PENDING` | child delivery 생성 후 lock 해제, 이후 aggregate-owned |

`FanoutMaterialized`는 다음 작업을 한 transaction에 묶습니다.

1. outbox claim token 검증
2. room delivery `INSERT ... ON CONFLICT DO NOTHING`
3. outbox lock 해제
4. outbox row version 증가

### aggregate projection

기존 atomic SQL의 의미를 유지합니다.

| child 상태 | outbox projection |
|---|---|
| `PENDING` 또는 `SENDING`이 하나 이상 | `PENDING` |
| active child가 없고 `FAILED` 또는 `QUARANTINED`가 하나 이상 | `FAILED` |
| active/failed child가 없고 `SENT`가 하나 이상 | `SENT` |
| child가 없음 | `PENDING` |

`QUARANTINED`는 delivery 원본에서는 별도 상태로 보존하지만 outbox aggregate에서는 `FAILED`로 투영합니다.

---

## Community/Shorts alarm state

`DETECTED`, `ENQUEUED`, `SENT`를 독립 writable FSM으로 만들지 않습니다.

```text
alarm_sent_at != nil  -> SENT
authorized_at != nil  -> ENQUEUED
그 외                 -> DETECTED
```

`delivery_status` 컬럼을 유지한다면 timestamp 사실과 일치하는 CHECK 또는 repository invariant를 둡니다. authoritative fact는 `authorized_at`, `alarm_sent_at`입니다.

---

## Planner API

### snapshot

```go
type DeliverySnapshot struct {
    ID            int64
    OutboxID      int64
    Status        domain.YouTubeDeliveryStatus
    AttemptCount  int
    NextAttemptAt time.Time
    LockedAt      *time.Time
    SentAt        *time.Time
    RowVersion    int64
}
```

### event와 failure

```go
type DeliveryFailure struct {
    Code       DeliveryFailureCode
    Message    string
    RetryAfter time.Duration
}

type DeliveryEvent struct {
    Kind    DeliveryEventKind
    At      time.Time
    Failure *DeliveryFailure
}
```

`At`은 transition service가 주입한 canonical UTC time입니다. planner 내부에서 `time.Now()`를 호출하지 않습니다.

### decision

```go
type DeliveryDecision struct {
    RuleID DeliveryRuleID
    Action DeliveryDecisionAction
    Event  DeliveryEventKind

    DeliveryID int64
    OutboxID   int64

    ExpectedStatus       domain.YouTubeDeliveryStatus
    ExpectedRowVersion   int64
    ExpectedAttemptCount int
    ExpectedLockedAt     *time.Time

    NextStatus       domain.YouTubeDeliveryStatus
    NextRowVersion   int64
    NextAttemptCount int
    NextAttemptAt    *time.Time
    SentAt           *time.Time

    ErrorCode DeliveryFailureCode
    ErrorText string
}
```

`RuleID`는 metric과 audit log의 안정적인 분류 키입니다. 예시는 다음과 같습니다.

```text
delivery.begin-send
delivery.delivered
delivery.retry-scheduled
delivery.retry-exhausted
delivery.permanent-failure
delivery.outcome-unknown-held
delivery.sending-quarantined
delivery.revived
delivery.reconciled-delivered
delivery.reconciled-not-delivered
```

### planner interface

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

planner는 context value에 의존하지 않습니다. context는 cancellation을 인식하는 순수 guard가 필요할 경우에만 전달합니다.

### apply result

```go
type ApplyOutcome uint8

const (
    ApplyApplied ApplyOutcome = iota + 1
    ApplyConflict
    ApplyMissing
)

type ApplyResult struct {
    DeliveryID int64
    Outcome    ApplyOutcome
    RowVersion int64
}
```

`ApplyConflict`는 DB 장애가 아니라 정상적인 concurrency 결과입니다. 로그 수준은 상황별로 조절하되 metric은 기록합니다.

---

## target package 구조

```text
hololive/hololive-alarm-worker/internal/egress/youtubedispatch/
├── lifecycle/
│   ├── delivery_types.go
│   ├── delivery_planner.go
│   ├── delivery_planner_test.go
│   ├── outbox_types.go
│   ├── outbox_planner.go
│   ├── outbox_planner_test.go
│   ├── retry_policy.go
│   ├── revive_policy.go
│   └── rule_catalog.go
├── delivery_transition_service.go
├── outbox_intent_transition_service.go
└── transition_observability.go

hololive/hololive-shared/pkg/service/youtube/outbox/store/
├── delivery_repository.go
├── delivery_repository_lock.go
├── delivery_transition_store.go
├── outbox_intent_store.go
└── queries/
```

lifecycle policy는 alarm-worker 단일 runtime 소유입니다. cross-runtime 계약이 아닌 실행 정책을 `hololive-shared` public path로 옮기지 않습니다. shared store는 PostgreSQL 접근과 cross-runtime domain row type이 필요한 범위만 유지합니다.

---

## 실행 흐름

### 정상 발송

```text
1. DeliveryRepository.FetchAndLock
   - PENDING due row claim
   - locked_at 설정
   - row_version 증가
   - DeliveryClaimToken 반환

2. local preparation
   - outbox load
   - payload validation
   - message formatting
   - provider request와 stable ClientRequestID 생성
   - community/shorts alarm-once claim

3. DeliveryPlanner.Plan(BeginSend)
   - PENDING + active claim -> SENDING decision

4. DeliveryTransitionStore.Apply
   - expected PENDING/version/locked_at CAS
   - SENDING, send_started locked_at, version 증가

5. SendEngine provider 호출

6. typed outcome 생성

7. DeliveryPlanner.Plan(Delivered)

8. DeliveryTransitionStore.ApplySentInTx
   - expected SENDING/version CAS
   - delivery SENT
   - 적용된 delivery에 대해서만 tracking/latency state 갱신

9. aggregate projector 실행
```

현재처럼 outbox load 전에 전체 row를 `SENDING`으로 만들지 않습니다. provider request를 만들기 전에 발생한 로컬 오류는 `PENDING` leased 상태에서 안전하게 retry 또는 permanent failure 처리합니다.

### retry-safe failure

```text
DeliveryOutcomeRetrySafe
-> planner가 attempt + 1 계산
-> maxRetries 경계에서 PENDING/FAILED 선택
-> retry 예정이면 max(policy backoff, provider Retry-After) 계산
-> repository가 expected state/version으로 적용
```

SQL은 `maxRetries`를 받지 않으며 `CASE`로 목적 상태를 선택하지 않습니다.

### outcome unknown

```text
provider 호출 결과 불명
-> DeliveryOutcomeUnknown
-> planner Hold
-> SENDING/lock/version 유지
-> stale-sending sweeper
-> SendingLeaseExpired decision
-> QUARANTINED
```

이 경로에서 claim release, failure bucket 삽입, 자동 retry를 하지 않습니다.

### revive

기존 revive 선정 조건을 보존합니다.

```text
outbox.status == FAILED
outbox.sent_at == nil
created_at >= freshness cutoff
active outbox lock 없음
FAILED delivery가 1개 이상이거나 delivery가 전혀 없음
```

적용은 한 transaction입니다.

```text
FAILED delivery -> PENDING, attempt_count=0, sent_at=NULL, error clear
SENT delivery -> 유지
QUARANTINED delivery -> 유지
outbox FAILED -> PENDING
```

전량 `QUARANTINED`인 outbox는 자동 revive하지 않습니다.

---

## PostgreSQL 계약

### schema 추가

다음 컬럼을 additive migration으로 추가합니다. migration 번호는 구현 시점의 다음 가용 번호를 사용합니다.

```sql
ALTER TABLE youtube_notification_delivery
    ADD COLUMN row_version bigint NOT NULL DEFAULT 0,
    ADD COLUMN error_code text NOT NULL DEFAULT '',
    ADD COLUMN state_changed_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE youtube_notification_outbox
    ADD COLUMN row_version bigint NOT NULL DEFAULT 0;
```

`locked_at`은 TTL과 stale 판정에 계속 사용합니다. `row_version`은 monotonic fencing token입니다.

### claim

```sql
UPDATE youtube_notification_delivery d
SET locked_at = $claim_at,
    row_version = row_version + 1
FROM claim
WHERE d.id = claim.id
RETURNING ..., d.row_version;
```

### transition CAS

```sql
UPDATE youtube_notification_delivery
SET status = $next_status,
    row_version = row_version + 1,
    attempt_count = $next_attempt_count,
    next_attempt_at = $next_attempt_at,
    sent_at = $sent_at,
    locked_at = $next_locked_at,
    error_code = $error_code,
    error = $error_text,
    state_changed_at = $changed_at
WHERE id = $id
  AND status = $expected_status
  AND row_version = $expected_row_version
  AND attempt_count = $expected_attempt_count
  AND locked_at IS NOT DISTINCT FROM $expected_locked_at
RETURNING row_version;
```

`locked_at`과 `row_version`을 병행 검사하는 전환 기간을 거친 뒤에도 둘 다 유지합니다. timestamp는 stale-time 의미, version은 writer identity 의미를 담당합니다.

### batch 적용

`ApplyBatch`는 heterogeneous decision을 입력받을 수 있습니다. store는 성능을 위해 `RuleID` 또는 mutation shape별로 partition할 수 있지만 목적 상태나 attempt를 다시 계산하면 안 됩니다.

batch 결과는 입력 delivery별로 반환합니다. 일부 row가 conflict여도 다른 row의 적용 결과를 잃지 않습니다.

### success transaction

`SENDING -> SENT`와 다음 항목은 동일 transaction입니다.

- community/shorts `alarm_sent_at`
- `authorized_at` 해제
- delivery 기반 sent tracking
- latency classification

CAS에 성공한 delivery ID만 후속 tracking 대상입니다.

### outbox direct update guard

pre-fanout 직접 update는 다음 조건을 포함합니다.

```sql
AND NOT EXISTS (
    SELECT 1
    FROM youtube_notification_delivery d
    WHERE d.outbox_id = o.id
)
```

child가 존재하는 outbox는 aggregate SQL 외 경로에서 status를 바꾸지 않습니다.

### constraint 도입

데이터 감사를 먼저 수행한 뒤 `NOT VALID`로 추가하고 별도 단계에서 validate합니다.

```sql
CHECK (attempt_count >= 0)
CHECK (row_version >= 0)
CHECK (status <> 'SENDING' OR locked_at IS NOT NULL)
CHECK (status <> 'SENT' OR sent_at IS NOT NULL)
CHECK (status IN ('PENDING', 'SENDING') OR locked_at IS NULL)
CHECK (status IN ('PENDING', 'SENDING', 'SENT', 'FAILED', 'QUARANTINED'))
```

---

## 직접 구현과 라이브러리 검토 결론

### 직접 planner가 대체하는 범위

- From/Event 허용 조합
- guarded 목적 상태 선택
- attempt와 retry 경계
- backoff 계산
- error code와 audit rule
- complete DB mutation plan

### 라이브러리를 사용해도 남는 범위

- expected version과 claim token
- attempt와 retry timestamp patch
- tracking transaction
- batch partial success
- aggregate projection
- external outcome unknown
- quarantine/revive recovery
- 기존 schema adapter

현재 상태 5개와 핵심 이벤트 9개 규모에서는 library adapter가 줄이는 코드보다 추가하는 경계가 더 큽니다.

### 재검토 trigger

다음 조건이 함께 성립하면 외부 library를 다시 평가합니다.

1. 선택된 transition ID와 structured metadata/effect plan을 순수 결과로 반환합니다.
2. caller-owned state와 persistence adapter를 분리합니다.
3. optimistic concurrency conflict를 일급 개념으로 지원합니다.
4. 기존 `youtube_notification_delivery` schema를 강제 교체하지 않습니다.
5. production adoption과 다중 maintainer 및 API 안정성이 충분히 축적됩니다.
6. 동일 contract test에서 직접 planner와 동등하고, adapter 포함 net policy code가 실질적으로 감소합니다.

library를 도입하더라도 domain과 repository API에는 외부 타입을 노출하지 않습니다.

---

## 구현 순서

각 단계는 독립 PR로 수행합니다. production writer를 두 개 두는 dual-write는 금지합니다.

### PR 1. 상태 타입과 순수 planner

범위:

- outbox와 delivery status Go 타입 분리
- `lifecycle` package 생성
- delivery/outbox snapshot, event, decision, policy, rule ID 정의
- 기존 동작을 재현하는 retry/revive policy 작성
- 모든 state/event 조합 table test
- runtime wiring 없음

완료 조건:

- planner test가 DB 없이 실행됩니다.
- `SENT` terminal, `QUARANTINED` no-auto-revive가 고정됩니다.
- 현재 max retry 경계와 linear backoff 동작을 그대로 재현합니다.

rollback:

- runtime 경로를 건드리지 않으므로 파일 제거만으로 복구됩니다.

### PR 2. typed delivery outcome

범위:

- `DispatchResult.FailureBuckets`와 `FailureRetryAfter` 제거
- `[]DeliveryOutcome` 도입
- SendEngine에서 retry-safe, permanent, unknown을 구조화
- failure code registry 도입
- 기존 repository mutator를 outcome 종류에 따라 호출하여 동작 유지

완료 조건:

- 상태 정책에서 오류 문자열 비교가 사라집니다.
- outcome unknown은 계속 failure/claim-release 경로에 들어가지 않습니다.
- grouped/per-room path가 동일 outcome contract를 사용합니다.

rollback:

- DB schema 변화가 없으므로 코드 revert로 복구됩니다.

### PR 3. transition service와 single writer

범위:

- `DeliveryTransitionService`와 `OutboxIntentTransitionService` 추가
- outcome을 planner decision으로 변환
- repository `ApplyBatch`와 `ApplySentInTx` 추가
- SQL에서 `maxRetries`와 상태 선택 `CASE` 제거
- legacy `MarkFailed*`, `MarkPermanentFailure*`, direct `MarkSent*` 호출 제거
- per-decision `Applied/Conflict/Missing` 결과와 metric 추가

완료 조건:

- delivery status를 변경하는 production entry point가 transition store로 제한됩니다.
- repository가 policy config를 받지 않습니다.
- partial batch conflict 테스트가 통과합니다.

rollback:

- 기존 mutator 삭제는 이 PR 마지막 commit에서 수행합니다. rollback용 release branch가 필요하면 한 릴리스 동안 deprecated wrapper를 내부에 유지하되 writer는 하나만 사용합니다.

### PR 4. preparation-before-SENDING와 row fencing

범위:

- outbox load, payload validation, formatting, request build를 `BeginSend`보다 앞으로 이동
- additive `row_version`, `error_code`, `state_changed_at` migration
- claim과 모든 transition에서 version 증가
- `DeliveryClaimToken`에 version 포함
- `locked_at + row_version` CAS
- stale version integration test

완료 조건:

- `SENDING` 진입 시 provider request와 stable ClientRequestID가 준비되어 있습니다.
- outbox load/format/build 실패는 `PENDING` leased 상태에서 처리됩니다.
- stale worker는 status가 같아도 version이 다르면 적용하지 못합니다.

rollback:

- 신규 컬럼은 additive이며 old binary가 무시할 수 있습니다. rollback 시 컬럼을 즉시 drop하지 않습니다.

### PR 5. outbox intent와 aggregate writer 경계

범위:

- `MaterializeFanout` transaction 도입
- child 존재 시 direct outbox status update 거부
- `StatusUpdater`를 pre-fanout intent 전용으로 대체하거나 제거
- aggregate SQL을 유일한 post-fanout writer로 고정
- revive planner와 existing transaction 연결

완료 조건:

- child가 있는 outbox를 direct mutator가 변경하지 못하는 integration test가 통과합니다.
- delivery aggregate의 기존 atomic semantics가 유지됩니다.
- FAILED revive가 SENT와 QUARANTINED child를 보존합니다.

rollback:

- schema 변경이 없고 query guard 중심이므로 코드 revert가 가능합니다.

### PR 6. 관측성과 architecture gate 및 정리

범위:

- transition metric/log 완성
- direct status SQL 금지 gate 추가
- deprecated mutator와 문자열 failure bucket 완전 삭제
- constraint audit 후 `VALIDATE CONSTRAINT`
- 문서의 delivery status를 `implemented`, 검증 완료 후 `verified`로 갱신

완료 조건:

- 허용된 store query 외 `youtube_notification_delivery.status` 직접 update가 CI에서 차단됩니다.
- 기존 retry/restart/exact-once/partial-failure 테스트가 모두 유지됩니다.
- 결정 레코드 implementation/evidence가 실제 파일과 테스트를 가리킵니다.

---

## 테스트 계약

### planner unit test

모든 state/event 조합을 table-driven test로 고정합니다.

```text
PENDING + BeginSend -> SENDING
PENDING + Delivered -> invalid
SENDING + Delivered -> SENT
SENDING + RetrySafe -> PENDING 또는 FAILED
SENDING + OutcomeUnknown -> Hold
SENDING + LeaseExpired -> QUARANTINED
SENT + automatic event -> invalid
FAILED + eligible Revive -> PENDING
QUARANTINED + Revive -> invalid
```

경계값:

```text
attempt=MaxRetries-2 + failure -> PENDING
attempt=MaxRetries-1 + failure -> FAILED
provider Retry-After > local backoff -> provider 값 사용
provider Retry-After <= local backoff -> local 값 사용
```

### repository integration test

- stale `locked_at` 거부
- stale `row_version` 거부
- expected attempt mismatch 거부
- batch 일부만 적용되는 경우 result 정합성
- `SENT` row를 failure가 덮어쓰지 못함
- 성공 CAS에 포함되지 않은 delivery의 tracking은 변경하지 않음
- delivery SENT와 tracking transaction rollback 원자성
- `QUARANTINED`는 normal claim 대상이 아님
- child가 있는 outbox direct update 거부
- aggregate priority와 `sent_at` 단조성

### crash-window test

| crash 위치 | 기대 복구 |
|---|---|
| claim 직후 | lock 만료 뒤 재claim |
| local preparation 중 | `PENDING` 유지, 안전한 재claim |
| `SENDING` commit 직후 provider 호출 전 | conservative quarantine 가능, 자동 중복 없음 |
| provider 성공 후 `SENT` commit 전 | `QUARANTINED`, 자동 재발송 없음 |
| `SENT` commit 후 aggregate 전 | reconcile loop가 outbox 수렴 |
| delivery success와 tracking 중간 오류 | transaction rollback, 부분 기록 없음 |

### sequence/fuzz invariant

임의 이벤트 순서에서 다음을 검사합니다.

- attempt count는 revive 외에는 감소하지 않습니다.
- `SENT`는 자동 이벤트로 벗어나지 않습니다.
- `QUARANTINED`는 자동 retry되지 않습니다.
- stale version은 현재 version을 덮어쓰지 못합니다.
- `DeliveryOutcomeUnknown`은 failure retry decision을 만들지 않습니다.
- tracking `SENT`는 실제 적용된 delivery success와 함께 존재합니다.

### 기존 회귀 테스트

다음 범주의 기존 테스트는 삭제하거나 약화하지 않습니다.

- delivery lock token
- sending state
- quarantine and aggregate failure
- retry exact-once integration
- selective retry send
- partial failure
- restart recovery
- community/shorts alarm-once claim
- canonical `sent_at`

---

## 관측성

### metric

```text
hololive_youtube_delivery_transition_total{
    rule,
    from,
    to,
    result
}

hololive_youtube_delivery_transition_conflict_total{
    rule,
    reason
}

hololive_youtube_delivery_outcome_unknown_total{
    provider,
    code
}

hololive_youtube_delivery_quarantine_total{
    cause
}

hololive_youtube_delivery_sending_age_seconds
hololive_youtube_outbox_aggregate_lag_seconds
```

ID, room ID, error text를 metric label로 사용하지 않습니다.

### structured log

```text
entity=delivery|outbox
rule_id
event
from_state
to_state
action=apply|hold
apply_outcome=applied|conflict|missing
attempt_count
row_version
delivery_id
outbox_id
failure_code
claim_age_ms
client_request_id_hash
```

room ID와 provider payload는 기존 민감정보 마스킹 규칙을 따릅니다.

### alert 후보

- 단일 replica에서 transition conflict가 지속 증가
- stale `SENDING` age가 lock timeout을 크게 초과
- quarantine 증가
- delivery `SENT`와 tracking mismatch
- aggregate lag 증가

threshold는 구현 후 baseline을 수집해 별도 운영 결정으로 정합니다. 이 문서는 임의 수치를 고정하지 않습니다.

---

## rollout

### 원칙

- production dual-write를 사용하지 않습니다.
- DB migration은 additive-first입니다.
- 기존 상태 문자열은 바꾸지 않습니다.
- 각 PR은 이전 단계의 테스트를 포함한 채 독립 배포 가능해야 합니다.
- 새 planner가 선택한 결과와 legacy 결과를 비교해야 하면 test 또는 read-only shadow 계산만 허용합니다. shadow 계산은 DB mutation을 하지 않습니다.

### 배포 순서

1. PR 1과 PR 2를 배포해 타입과 outcome contract를 안정화합니다.
2. PR 3에서 single writer를 전환합니다.
3. transition conflict, retry, sent tracking을 확인합니다.
4. PR 4 additive migration과 pipeline ordering을 배포합니다.
5. PR 5 outbox writer 경계를 적용합니다.
6. 한 번 이상의 retry, quarantine fault injection, aggregate reconcile 검증 후 PR 6 cleanup을 수행합니다.

### 즉시 rollback 조건

- `SENT -> PENDING/FAILED` 회귀
- outcome unknown이 자동 retry로 들어감
- stale token/version이 적용됨
- delivery success와 tracking이 불일치
- child가 있는 outbox가 direct writer로 변경됨
- aggregate가 terminal 상태에서 `PENDING`으로 잘못 회귀

### rollback 방법

- code path를 이전 단일 writer로 되돌립니다.
- additive 컬럼과 `NOT VALID` constraint는 유지합니다.
- 새 컬럼 drop은 별도 검증 후 수행하며 emergency rollback에 포함하지 않습니다.
- 이미 `QUARANTINED`된 row를 자동으로 `PENDING`으로 되돌리지 않습니다.
- provider 중복 여부가 불명확한 row는 운영 reconciliation 전까지 보존합니다.

---

## architecture gate

최종 단계에서 다음 규칙을 정적 검사합니다.

1. 허용된 store query 외부에서 `youtube_notification_delivery`의 `status`를 직접 update하지 않습니다.
2. `DeliveryRepository` 공개 메서드는 `maxRetries`나 backoff 정책값을 받지 않습니다.
3. lifecycle package는 DB, slog, prometheus, provider client를 import하지 않습니다.
4. failure reason 문자열로 permanent/retry를 선택하지 않습니다.
5. `SENT` tracking 변경은 적용된 delivery ID에서만 시작합니다.
6. aggregate SQL은 count-then-update 형태로 분리하지 않습니다.

초기 gate는 정확한 allowlist로 시작하고, generic grep 예외를 넓게 허용하지 않습니다.

---

## 완료 조건

다음 항목을 모두 충족하면 구현을 `implemented`로 변경합니다.

- [ ] outbox와 delivery status 타입이 분리되어 있습니다.
- [ ] `DeliveryPlanner`와 `OutboxIntentPlanner`가 순수 함수로 존재합니다.
- [ ] SendEngine이 typed outcome을 반환합니다.
- [ ] failure 문자열 비교가 lifecycle 정책에서 제거되었습니다.
- [ ] delivery status production writer가 transition store 하나로 제한됩니다.
- [ ] repository SQL이 max retry와 목적 상태를 결정하지 않습니다.
- [ ] 모든 transition이 expected status/version/attempt를 검사합니다.
- [ ] `SENDING`은 provider request 준비 완료 후 진입합니다.
- [ ] outcome unknown은 hold 후 quarantine 경로만 사용합니다.
- [ ] `QUARANTINED`는 자동 revive되지 않습니다.
- [ ] delivery `SENT`와 tracking이 한 transaction입니다.
- [ ] child delivery가 있는 outbox는 aggregate projector만 status를 씁니다.
- [ ] aggregate atomic SQL semantics가 유지됩니다.
- [ ] revive가 SENT/QUARANTINED child를 보존합니다.
- [ ] stale writer, partial batch, crash-window 통합 테스트가 통과합니다.
- [ ] direct status update architecture gate가 적용됩니다.
- [ ] alarm-worker replica는 별도 결정 전까지 1입니다.

다음 검증까지 완료하면 `verified`로 변경합니다.

- [ ] production 또는 production-equivalent 환경에서 retry 경로가 정상 수렴했습니다.
- [ ] outcome-unknown fault injection이 중복 자동 발송 없이 quarantine으로 수렴했습니다.
- [ ] aggregate reconcile이 crash 이후 수렴했습니다.
- [ ] transition conflict와 tracking mismatch에 비정상 증가가 없습니다.
- [ ] 결정 레코드의 `implementation`과 `evidence`가 실제 코드·테스트를 가리킵니다.

---

## 구현자가 유지해야 하는 비목표

이번 작업에서 다음 변경은 하지 않습니다.

- event sourcing 전환
- Temporal, Cadence 등 외부 workflow engine 도입
- provider 호출을 DB transaction 안으로 이동
- outbox aggregate를 Go에서 재계산
- `QUARANTINED` 자동 retry
- alarm-worker replica 확대
- retry 알고리즘 자체의 제품 정책 변경
- current DB status 문자열 변경

retry 알고리즘을 linear backoff에서 exponential backoff로 바꾸는 작업은 lifecycle 책임 분리가 완료된 뒤 별도 결정으로 수행합니다.
