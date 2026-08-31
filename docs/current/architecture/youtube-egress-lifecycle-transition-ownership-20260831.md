# YouTube egress lifecycle 전이 소유권

작성일: 2026-08-31 KST  
결정 ID: `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership`  
대상 런타임: `hololive-alarm-worker`  
결정 상태 정본: [`docs/decisions/records/DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership.json`](../../decisions/records/DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership.json)

## 목적

`youtube_notification_outbox`와 `youtube_notification_delivery`의 상태 변경, logical delivery 중복 방지, post tracking, 외부 발송, DB commit 판정 책임을 분리합니다.

상세 정본:

- 상태·logical group·token·attempt·CAS: [`youtube-egress-lifecycle-contract-20260831.md`](youtube-egress-lifecycle-contract-20260831.md)
- DB commit ambiguity: [`youtube-egress-lifecycle-commit-adjudication-20260831.md`](youtube-egress-lifecycle-commit-adjudication-20260831.md)
- 직접 구현과 library 판단: [`youtube-egress-lifecycle-library-review-20260831.md`](youtube-egress-lifecycle-library-review-20260831.md)
- 구현 순서: [`2026-08-31-youtube-egress-lifecycle-implementation.md`](../plans/2026-08-31-youtube-egress-lifecycle-implementation.md)

현재 DB 원자성과 stale-token 방어는 유지합니다. 변경 대상은 Go와 SQL에 분산된 전이 정책, logical ownership, writer 권한, external-effect 경계입니다.

## 결정

### D-001. alarm-worker 내부 typed policy가 전이를 결정한다

`hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle`은 다음을 소유합니다.

- 상태/event/failure vocabulary
- preparation과 send phase 구분
- retry 소진과 due 계산
- logical group state 해석
- revive 허용 여부
- stable rule ID와 concrete decision

```text
snapshot + typed event + policy + explicit time
    -> concrete decision 또는 명시적 거부
```

Policy는 DB, network, logger, metric, provider SDK, `time.Now()`에 의존하지 않습니다.

### D-002. PostgreSQL이 durable state와 집행을 소유한다

PostgreSQL은 상태, attempt, due, lock, delivery version, tracking, aggregate의 정본입니다.

```text
policy       어디로 가야 하는가
coordinator  어떤 logical/physical row가 함께 움직이는가
store        expected state/token이 아직 유효한가
PostgreSQL   mutation과 transaction이 commit됐는가
```

Repository는 `MaxRetries`나 failure 문자열을 다시 해석하지 않습니다.

### D-003. physical row와 logical delivery를 분리한다

Physical delivery는 `youtube_notification_delivery.id` row이며 claim/CAS 단위입니다.

Logical delivery는 사용자에게 같은 내용을 같은 방에 한 번 전달하는 단위입니다.

```text
Community/Shorts:
(kind, canonical_post_id, room_id)

기타 kind:
(outbox_id, room_id)
```

`(outbox_id,room_id)` unique key는 physical duplicate만 막습니다. 서로 다른 outbox/content ID가 같은 canonical post를 나타낼 수 있으므로 logical dedupe를 대체하지 않습니다.

### D-004. logical group의 provider/attempt owner는 하나다

같은 logical key의 retained row 중 deterministic owner를 선택합니다.

```text
owner = 최소 (created_at, delivery_id)
```

Durable evidence priority:

```text
SENT > QUARANTINED > SENDING > deterministic PENDING/FAILED owner
```

- `SENT`: group fulfilled
- `QUARANTINED`: group unresolved
- `SENDING`: group in-flight
- Owner `PENDING`: owner만 send/retry 가능
- Owner `FAILED`: group failed

Follower는 provider를 호출하거나 attempt를 증가시킬 수 없습니다. Owner retry due와 terminal 상태를 mirror합니다.

### D-005. preparation coordinator가 coalescing과 group resolution을 소유한다

한 batch의 동일 logical key는 owner/leader 하나만 provider candidate가 됩니다. Post-level cached `Proceed`도 room-level logical group resolution을 생략할 수 없습니다.

Coordinator 책임:

```text
logical key 생성
-> retained group 조회
-> evidence/owner 판정
-> follower projection decision
-> tracking requirement
-> immutable request
```

Store는 bounded group query와 fenced group mutation을 제공합니다.

### D-006. delivery에만 `row_version`을 추가한다

```sql
ALTER TABLE youtube_notification_delivery
ADD COLUMN row_version bigint NOT NULL DEFAULT 0;
```

Claim과 모든 physical mutation은 version을 1 증가시킵니다. `locked_at`은 TTL/관측/보조 predicate, `row_version`은 주 fencing token입니다. Version은 인덱싱하지 않습니다.

Outbox에는 이번 범위에서 version을 추가하지 않습니다.

### D-007. 외부 발송은 lifecycle effect와 DB callback 밖에 둔다

Provider 호출은 PostgreSQL rollback으로 취소할 수 없습니다.

```text
preparation 완료
-> operation-level PENDING to SENDING commit 확인
-> provider call
-> typed outcome
-> fenced logical-group finalization
```

Policy, entry/exit action, store callback 안에서 provider를 호출하지 않습니다.

### D-008. outcome unknown은 immediate transition이 아니다

Provider 처리 여부가 불명확하면 다음을 하지 않습니다.

- retry/failure state write
- claim release
- grouped fallback
- provider resend

Owner는 `SENDING`에 남고 stale sweeper가 logical group을 quarantine합니다.

### D-009. preparation과 send phase를 분리한다

Provider 전 local failure와 `SENDING` 이후 provider outcome을 같은 오류/event로 합치지 않습니다.

```text
Preparation:
payload, canonical identity, group resolution, tracking requirement,
rendering, route, request construction

Send:
delivered, known-not-delivered retryable/permanent, unknown
```

### D-010. generic mutation DSL을 금지한다

금지:

```text
UpdateStatus(id,status)
ApplyPatch(map[string]any)
expected token 생략 writer
repository MaxRetries argument
raw error 문자열 branch
```

의도별 command를 사용합니다.

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
```

### D-011. provider operation은 owner member 전체에 all-or-none이다

Provider request 한 번에 포함되는 exact owner 집합이 operation입니다.

다음은 owner member 전체에 all-or-none입니다.

- `BeginSending`
- grouped success finalization
- grouped known-failure finalization

Mixed pre/post member는 partial success가 아니라 atomicity breach입니다.

### D-012. post tracking은 exact token 또는 already-sent로 충족된다

Community/Shorts tracking은 post 전역, delivery는 room별입니다. 여러 room이 같은 claim token을 공유할 수 있습니다.

Tracking requirement:

```text
NoTracking
RequireClaimOrAlreadySent
RequireAlreadySent
```

첫 room success가 exact token을 소비하고 tracking을 sent로 만들면 후속 room success는 already-sent를 정상 terminal 상태로 수용합니다. Tracking token conflict만으로 provider-success delivery를 실패시키지 않습니다.

### D-013. DB commit ambiguity를 일급 결과로 다룬다

```text
Applied
Conflict
Missing
Indeterminate
```

Write primary exact read-back으로 owner, follower projection, tracking requirement를 함께 판정합니다.

- Begin post-state 확인 전 provider call 금지
- Provider success 후 DB-only finalization retry만 허용
- Provider callback/send 자동 재실행 금지
- ID-only `SENDING -> SENT` recovery 금지
- Mixed group/tracking은 atomicity breach

### D-014. same-logical `SENT` evidence는 단조 reconciliation을 허용한다

`SENT`는 절대 terminal입니다.

`FAILED`와 `QUARANTINED`는 automatic resend/revive에 terminal이지만, exact same-logical `SENT` evidence가 생기면 provider 호출 없이 `SENT`로 reconcile할 수 있습니다.

```text
PENDING/FAILED/QUARANTINED -> SENT
오직 same-logical durable SENT evidence가 있을 때
```

다음은 금지합니다.

```text
QUARANTINED -> PENDING/FAILED
FAILED -> PENDING outside group revive
SENT -> *
```

### D-015. child 생성 후 outbox writer는 aggregate projector뿐이다

Pre-fanout에서는 `OutboxFanoutService`가 no-target, fanout failure, materialization을 소유합니다. Delivery insert와 outbox lock clear는 한 transaction입니다.

Child가 생긴 뒤에는 atomic aggregate SQL만 outbox status/error/sent_at을 계산합니다. Direct writer는 transaction 안에서 `NOT EXISTS child`를 확인합니다.

### D-016. revive는 outbox row가 아니라 logical group 단위다

Same-logical `SENT/QUARANTINED` evidence가 없고 deterministic owner가 `FAILED`일 때만 group revive를 허용합니다.

- Owner/follower `PENDING`
- Attempt 0
- Due 정렬
- Lock/error clear
- Version 증가
- Touched outbox aggregate 갱신

### D-017. terminal cleanup는 dedupe evidence retention을 보장한다

Terminal delivery row는 same-room logical dedupe evidence입니다.

Cleanup은 다음을 보장합니다.

- Same-logical nonterminal row가 있으면 terminal row 삭제 금지
- `CleanupAfter`가 claim/revive freshness envelope보다 김
- Cleanup retry가 cutoff를 앞당기지 않음

Persisted logical key/receipt ledger는 이번 범위에 추가하지 않습니다. Computed group query 또는 retention이 한계라는 운영 evidence가 생기면 별도 결정으로 검토합니다.

### D-018. worker 전용 persistence를 alarm-worker internal로 회수한다

현재 `hololive-shared/pkg/service/youtube/outbox/store`의 production consumer는 alarm-worker 하나입니다.

```text
hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store
```

Claim/transition SQL, group query, aggregate sync, tracking transaction, worker row DTO를 internal owner로 이동합니다. Cross-runtime 계약과 진성 다중 소비자만 shared에 남깁니다.

### D-019. 범용 FSM library를 현재 도입하지 않는다

홀로봇은 destination state 외에 logical owner, follower projection, tracking requirement, exact version, due, operation atomicity, no-write outcome을 포함한 complete decision이 필요합니다.

검토한 library가 줄이는 범위보다 adapter와 duplicate policy 표현이 더 큽니다. Domain-specific pure planner를 직접 구현하되 재검토 조건은 library-review 문서가 소유합니다.

### D-020. replica 확대를 승인하지 않는다

Computed logical owner와 current coordinator는 replica=1을 전제로 합니다. Replica>1에서는 persisted logical affinity/lock이 추가로 필요합니다. 기존 scale-out 결정에 따라 단일 인스턴스를 유지합니다.

## 현재 구조에서 확인된 문제

### Policy 분산

- `status_updater.go`: outbox retry/FAILED
- `claim_manager_pipeline.go`: reason 문자열 분류
- retry SQL: max retry와 목적 상태
- `claim_manager_revive.go`: revive policy

### Logical ownership gap

- Post-level cache와 room-level sent check의 책임이 분리됨
- `SENDING/QUARANTINED/PENDING/FAILED` sibling이 일관된 group state로 해석되지 않음
- Follower가 별도 retry budget으로 owner를 추월할 수 있음

### Tracking lifecycle mismatch

Post claim은 전역이고 delivery는 room별입니다. 첫 room success가 token을 소비한 뒤 후속 room success는 already-sent를 수용해야 합니다.

### Unsafe recovery

Provider success 뒤 mark-sent 오류에서 ID만으로 `SENDING -> SENT`와 tracking을 복구하는 경로는 fence를 우회하므로 제거합니다.

### 보존할 방어

- Exact `status + locked_at` stale-token 거부
- 실제 CAS 성공 row만 tracking update
- Outcome unknown hold와 stale quarantine
- Atomic outbox aggregate SQL

## 목표 아키텍처

```text
┌──────────────────────────────────────────────────────────────┐
│ Preparation coordinator                                      │
│ logical key -> group resolve -> owner/follower -> tracking   │
└────────────────────────────┬─────────────────────────────────┘
                             │ typed result
                             ▼
┌──────────────────────────────────────────────────────────────┐
│ Pure lifecycle policy                                        │
│ snapshot + event + policy + time -> concrete decision        │
└────────────────────────────┬─────────────────────────────────┘
                             │ intent-specific command
                             ▼
┌──────────────────────────────────────────────────────────────┐
│ Internal transition store                                    │
│ group CAS, tracking tx, commit adjudication, aggregate       │
└────────────────────────────┬─────────────────────────────────┘
                             │ BeginSending Applied only
                             ▼
┌──────────────────────────────────────────────────────────────┐
│ Provider transport                                           │
│ immutable request -> typed outcome                           │
└──────────────────────────────────────────────────────────────┘
```

## 책임 행렬

| 책임 | 소유자 | 금지 사항 |
|---|---|---|
| State/event/failure | lifecycle | raw error branch 금지 |
| Logical key/group owner | preparation coordinator | physical ID를 logical identity로 간주 금지 |
| Group query/CAS | internal store | post cache로 대체 금지 |
| Retry/revive policy | lifecycle | DB/I/O/clock 금지 |
| Tracking requirement | coordinator + store | token conflict를 무조건 failure로 해석 금지 |
| Provider outcome | transport adapter | planner의 SDK 문자열 파싱 금지 |
| External send | SendEngine | DB callback 안에서 호출 금지 |
| Commit adjudication | store/application service | provider 자동 retry 금지 |
| Outbox aggregate | atomic SQL | Go count 후 update 금지 |
| Metrics/audit | transition service | policy side effect 금지 |

## 스키마 범위

필수 schema 변경은 delivery `row_version` 하나입니다.

초기 범위에서 추가하지 않습니다.

- Outbox `row_version`
- Persisted logical key/receipt ledger
- `state_changed_at`
- 별도 `error_code`
- 별도 `send_started_at`

Group query 또는 retention evidence가 한계를 보이면 normalized logical ledger를 별도 결정으로 검토합니다.

## 배포 호환성

| 단계 | Schema | Binary | 허용 |
|---|---|---|---|
| Migration 전 | version 없음 | 기존 | 예 |
| Migration 후 | version default 0 | 기존 | 예 |
| Cutover 후 | version/group CAS | 새 single instance | 목표 |
| Rollback | column 유지 | 기존 single instance | 예 |
| Old/new 동시 writer | 어느 schema든 | 두 egress binary | 금지 |

Cutover 전 `SENDING`을 drain/quarantine합니다. Additive column은 rollback하지 않습니다.

## 안전성과 가용성

보장:

1. Stale writer 차단
2. Logical provider/attempt owner 단일화
3. Follower retry-budget 우회 차단
4. Same-room durable group dedupe
5. Outcome unknown 자동 resend 차단
6. Multi-room tracking idempotency
7. Provider success 뒤 DB 오류의 provider replay 차단
8. Child outbox writer 단일화

자동 가용성을 포기하는 경우:

- Provider outcome unknown
- Logical unresolved group
- Commit `Indeterminate`
- Tracking/group mixed state

## 재검토 조건

1. Provider가 result query 또는 replay dedupe 계약을 제공합니다.
2. Quarantine 운영 처리가 반복됩니다.
3. Computed group query/retention의 안전·성능 한계가 측정됩니다.
4. Store에 두 번째 production runtime consumer가 생깁니다.
5. FSM library가 재검토 gate를 모두 만족합니다.
6. Replica>1 게이트가 별도 결정으로 해소됩니다.
