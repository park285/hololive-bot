# YouTube egress lifecycle 전이 소유권

작성일: 2026-08-31 KST  
결정 ID: `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership`  
대상 런타임: `hololive-alarm-worker`  
결정 상태 정본: [`docs/decisions/records/DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership.json`](../../decisions/records/DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership.json)

## 목적

`youtube_notification_outbox`와 `youtube_notification_delivery`의 상태 변경, 논리적 중복 방지, 외부 발송, DB commit 판정 책임을 명시적으로 분리합니다.

상세 규범과 실행 순서는 다음 문서가 소유합니다.

- 상태·token·attempt·logical delivery·CAS: [`youtube-egress-lifecycle-contract-20260831.md`](youtube-egress-lifecycle-contract-20260831.md)
- DB commit ambiguity: [`youtube-egress-lifecycle-commit-adjudication-20260831.md`](youtube-egress-lifecycle-commit-adjudication-20260831.md)
- 직접 구현과 library 판단: [`youtube-egress-lifecycle-library-review-20260831.md`](youtube-egress-lifecycle-library-review-20260831.md)
- 단계별 구현: [`2026-08-31-youtube-egress-lifecycle-implementation.md`](../plans/2026-08-31-youtube-egress-lifecycle-implementation.md)

현재 구현의 DB 원자성과 stale-token 방어는 유지합니다. 변경 대상은 DB 자체가 아니라 Go와 SQL에 분산된 전이 정책, writer 권한, external-effect 경계입니다.

## 결정

### D-001. alarm-worker 내부의 typed lifecycle policy가 전이를 결정한다

다음 정책은 `hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle`이 소유합니다.

- 허용 상태 전이와 terminal 의미
- preparation 실패와 send failure의 구분
- retry 소진과 due 계산
- revive 허용 여부
- stable rule ID와 typed decision

```text
snapshot + typed event + policy + explicit time
    -> concrete decision 또는 명시적 거부
```

Policy는 DB, network, logger, metric, provider SDK, `time.Now()`에 접근하지 않고 입력을 변경하지 않습니다.

### D-002. PostgreSQL은 durable state와 전이 집행을 소유한다

PostgreSQL은 다음 값의 정본입니다.

- outbox/delivery 현재 상태
- attempt와 next-attempt 시각
- active claim/send 시각
- delivery fencing version
- success tracking과 aggregate projection

Repository는 policy를 다시 계산하지 않습니다. Expected state/version/attempt/lock을 만족할 때만 concrete command를 집행합니다.

```text
policy       어디로 가야 하는가
coordinator  어떤 logical/physical row가 함께 움직이는가
store        아직 expected state와 token이 유효한가
PostgreSQL   mutation과 transaction이 원자적으로 commit되는가
```

### D-003. physical row와 logical delivery를 분리한다

Physical delivery는 `youtube_notification_delivery.id` row입니다. Claim과 CAS의 단위입니다.

Logical delivery는 사용자에게 같은 내용을 같은 방에 한 번 보내는 단위입니다.

```text
Community/Shorts:
(kind, canonical_post_id, room_id)

그 밖의 YouTube kind:
(outbox_id, room_id)
```

`(outbox_id, room_id)` unique index는 physical duplicate만 막습니다. 서로 다른 outbox/content ID가 같은 Community/Shorts post를 표현할 수 있으므로 logical duplicate 방어를 대체하지 않습니다.

Logical identity와 batch leader/follower 선출, durable sibling 상태 해석은 `PreparationCoordinator`가 소유합니다. Store는 sibling candidate 조회와 current-row fenced mutation을 제공합니다.

### D-004. 같은 logical delivery의 send leader는 하나다

한 process batch에서 같은 logical key를 가진 row는 `(created_at, delivery_id)` 순서의 첫 row만 leader입니다. Follower는 attempt 없이 defer합니다.

Community/Shorts leader는 provider 호출 전에 durable sibling을 확인합니다.

```text
SENT sibling
    -> current row AlreadySatisfied

SENDING sibling
    -> current row no-attempt defer

QUARANTINED sibling
    -> current row provider 미호출 QUARANTINED 전파
```

Post-level decision cache가 `Proceed`를 반환해도 room-level sibling gate를 생략할 수 없습니다. Cache는 post-level claim 계산만 재사용합니다.

이 규칙은 alarm claim timeout 이후 같은 post·room이 다시 발송되는 경로를 차단합니다.

### D-005. delivery에만 `row_version`을 도입한다

```sql
ALTER TABLE youtube_notification_delivery
ADD COLUMN row_version bigint NOT NULL DEFAULT 0;
```

Claim과 모든 delivery mutation은 version을 1 증가시킵니다. 후속 writer는 이전 operation이 반환한 version을 expected value로 사용합니다.

`locked_at`은 TTL과 운영 관측, 전환 기간의 보조 predicate입니다. `row_version`은 stale writer fencing의 주 token입니다. 모든 write에서 바뀌는 version은 인덱싱하지 않습니다.

Outbox에는 이번 범위에서 version을 추가하지 않습니다. Pre-fanout은 기존 outbox claim token과 transaction, post-fanout은 aggregate projector로 보호합니다.

### D-006. 외부 발송은 lifecycle effect와 DB callback 밖에서 실행한다

Provider 호출은 PostgreSQL rollback으로 취소할 수 없습니다. Policy, state entry/exit action, repository callback 안에 Iris/Kakao 발송을 넣지 않습니다.

```text
local preparation 완료
-> operation-level PENDING to SENDING commit 확인
-> provider call
-> typed provider outcome
-> 확정 가능한 outcome만 finalization
```

`SENDING`은 exact provider operation이 durable하게 확정됐다는 뜻입니다. 실제 socket write 또는 provider 수신을 증명하지 않습니다.

### D-007. outcome unknown은 즉시 상태 전이가 아니다

Timeout, connection reset, process failure처럼 provider 처리 여부를 증명할 수 없으면 `OutcomeUnknown`입니다.

Application service는 다음 작업을 하지 않습니다.

- retry scheduling
- `FAILED` 또는 즉시 `QUARANTINED` write
- alarm claim release
- grouped fallback
- provider resend

Row는 `SENDING`에 남고 stale-sending sweeper만 `QUARANTINED`로 이동시킵니다.

### D-008. preparation과 send phase를 분리한다

`PENDING + PreparationLease`에서 발생한 실패는 provider 호출 전 failure입니다.

- outbox/payload load
- validation/rendering
- canonical logical identity 생성
- sibling resolution
- alarm claim
- request construction

`SENDING + SendFence` 이후의 결과는 provider outcome입니다.

- delivered
- known-not-delivered retryable/permanent
- outcome unknown

두 phase를 하나의 `RetrySafeFailure`나 오류 문자열로 합치지 않습니다.

### D-009. generic mutation DSL을 사용하지 않는다

다음 API를 금지합니다.

```text
UpdateStatus(id, status)
ApplyPatch(map[string]any)
caller가 expected state/token을 생략하는 writer
repository가 MaxRetries를 받는 API
```

다음과 같은 의도별 command를 사용합니다.

```text
ClaimPending
BeginSending
CompleteAlreadySatisfied
DeferClaim
PropagateQuarantine
ScheduleRetry
Fail
CompleteSent
QuarantineStaleSending
ReviveFailedOutboxes
```

Concrete constructor가 source state, token, timestamp, required field shape를 검증합니다.

### D-010. provider operation member는 all-or-none이다

외부 provider 요청 한 번에 포함되는 exact delivery member 집합을 provider operation이라고 합니다.

다음 mutation은 operation member 전체에 all-or-none입니다.

- `BeginSending`
- grouped success finalization
- grouped known-failure finalization

Member 하나라도 conflict/missing이면 transaction 전체를 rollback합니다. CAS에서 탈락한 row를 포함한 payload를 provider에 보내지 않습니다. Mixed pre/post member는 partial success가 아니라 atomicity breach입니다.

### D-011. DB commit ambiguity를 일급 결과로 다룬다

DB client가 `COMMIT` 오류를 받았다고 transaction이 rollback됐다고 단정하지 않습니다.

```text
Applied
Conflict
Missing
Indeterminate
```

Effect 인접 operation은 write primary의 exact read-back으로 판정합니다.

- `BeginSending` post-state 확인 전 provider call 금지
- Provider success 이후 DB retry는 local finalization만 허용
- Provider send 자동 재호출 금지
- ID-only `SENDING -> SENT` recovery 금지
- Delivery/tracking 또는 grouped member mixed state는 atomicity breach

세부 판정은 commit-adjudication 문서가 정본입니다.

### D-012. child delivery 생성 후 outbox 상태 writer는 aggregate projector뿐이다

Outbox는 두 phase를 가집니다.

```text
pre-fanout intent
    child 없음

post-fanout aggregate
    child 하나 이상
```

Pre-fanout에서는 `OutboxFanoutService`만 no-target completion, fanout failure, delivery materialization을 수행합니다. Delivery insert와 outbox lock clear는 한 transaction입니다.

Child가 생긴 뒤에는 atomic aggregate SQL만 outbox `status`, aggregate error, `sent_at`을 계산합니다. Direct finalization은 transaction 안에서 `NOT EXISTS child`를 확인합니다.

### D-013. `SENT`와 `QUARANTINED`는 자동 terminal이다

- `SENT -> *` 자동 전이는 없습니다.
- `QUARANTINED -> *` 자동 전이는 없습니다.
- `FAILED -> PENDING`은 freshness, never-sent, eligible child 조건을 만족하는 revive만 허용합니다.
- Logical sibling의 `QUARANTINED`를 current row에 전파하는 것은 terminal state에서 나가는 전이가 아니라 unresolved logical delivery를 새 physical row에 복제하는 방어입니다.

Provider evidence 기반 reconciliation이나 manual replay는 별도 결정, duplicate-risk acknowledgement, immutable audit가 필요합니다.

### D-014. worker 전용 persistence를 alarm-worker internal로 회수한다

현재 `hololive-shared/pkg/service/youtube/outbox/store`의 production consumer는 alarm-worker 하나입니다. `DEC-20260825-hololive-shared-public-path-scoped-retention`에 따라 다음 위치로 이동합니다.

```text
hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store
```

Worker 전용 claim/transition SQL, sibling query, aggregate sync, tracking transaction, row DTO는 internal owner가 소유합니다. Cross-runtime 계약과 진성 다중 소비자만 shared에 남깁니다.

Package copy와 dual implementation을 만들지 않고 `git mv`, import cutover, old package removal을 한 변경 묶음에서 수행합니다.

### D-015. 범용 FSM 라이브러리를 현재 도입하지 않는다

가장 가까운 후보는 caller-owned immutable `Machine.Next`를 제공하는 `open-ships/statemachine`이었습니다. 그러나 홀로봇은 destination state 외에 다음 complete decision을 필요로 합니다.

- logical rule identity
- expected state/version/attempt/lock
- next attempt/due
- claim retain/release 의미
- tracking mutation
- operation atomicity
- no-write outcome

Library destination selection과 application mutation builder가 같은 정책을 중복 계산하거나 mutable guard를 사용해야 한다면 도입 이득이 없습니다. 성숙한 state-owning/callback FSM은 PostgreSQL source-of-truth와 맞지 않았고, 가장 완전한 persistence 후보는 archived 상태였습니다.

따라서 domain-specific pure planner를 직접 구현합니다. 재검토 조건은 library-review 문서가 정본입니다.

### D-016. alarm-worker replica 확대를 승인하지 않는다

Delivery fencing과 logical coalescing을 강화해도 replica>1의 canonical-group claim, background loop, local fallback 게이트는 별개입니다. `alarm-egress-scale-out-decisions-20260730.md`에 따라 단일 인스턴스를 유지합니다.

## 현재 코드에서 확인된 문제

### 정책 분산

- `status_updater.go`: outbox retry/FAILED 선택
- `claim_manager_pipeline.go`: reason 문자열 permanent/retry 분류
- `delivery_repository_lock_0190_03.sql`: max retry와 목적 상태 선택
- `claim_manager_revive.go`: revive 정책

Policy와 SQL이 같은 결정을 나눠 소유합니다.

### Logical dedupe gap

현재 post-level claim cache는 room과 무관한 결정을 재사용합니다. Per-room gate는 `SENT` sibling을 확인하지만 `Proceed` cache hit에서 항상 실행된다고 보장되지 않고, `SENDING`/`QUARANTINED` sibling을 차단 근거로 사용하지 않습니다.

Physical unique key `(outbox_id, room_id)`만으로 서로 다른 outbox가 같은 canonical post를 나타내는 경우를 막을 수 없습니다.

### Unsafe success recovery

Provider success 뒤 mark-sent 오류에서 ID 집합만으로 `SENDING -> SENT`와 tracking을 복구하는 경로가 있습니다. Stale token/version을 우회할 수 있으므로 제거합니다. Exact Send fence와 primary read-back으로 local finalization만 재시도합니다.

### 보존할 방어

- `status + locked_at` exact stale-token 거부
- 실제 CAS 성공 delivery만 tracking update
- Outcome unknown hold와 stale quarantine
- Atomic outbox aggregate SQL

## 목표 아키텍처

```text
┌──────────────────────────────────────────────────────────────┐
│ Claim / preparation coordinator                              │
│                                                              │
│ logical key -> batch coalesce -> sibling gate -> alarm claim │
└────────────────────────────┬─────────────────────────────────┘
                             │ typed preparation result
                             ▼
┌──────────────────────────────────────────────────────────────┐
│ Pure lifecycle policy                                        │
│                                                              │
│ snapshot + event + policy + explicit time -> decision        │
└────────────────────────────┬─────────────────────────────────┘
                             │ intent-specific command
                             ▼
┌──────────────────────────────────────────────────────────────┐
│ Transition service / internal store                          │
│                                                              │
│ operation partition, exact CAS, commit adjudication          │
│ tracking transaction, sibling query, aggregate projection    │
└────────────────────────────┬─────────────────────────────────┘
                             │ only after BeginSending Applied
                             ▼
┌──────────────────────────────────────────────────────────────┐
│ Provider transport                                           │
│                                                              │
│ immutable request -> typed provider outcome                  │
└──────────────────────────────────────────────────────────────┘
```

## 책임 행렬

| 책임 | 소유자 | 금지 사항 |
|---|---|---|
| State/event/failure vocabulary | lifecycle package | raw error 문자열 branch 금지 |
| Logical key와 leader/follower | preparation coordinator | physical row ID를 logical identity로 간주 금지 |
| Durable sibling candidate 조회 | internal store | post-level cache로 대체 금지 |
| Retry/revive 결정 | lifecycle policy | DB/I/O/clock 직접 호출 금지 |
| Provider error 분류 | transport adapter | planner가 SDK 문자열 파싱 금지 |
| Claim과 exact CAS | internal store | unconditional status update 금지 |
| External send | SendEngine | DB callback 안에서 호출 금지 |
| Commit adjudication | internal store/application service | callback/provider 자동 retry 금지 |
| Success tracking | `CompleteSent` transaction | conflict member tracking 금지 |
| Outbox aggregate | atomic SQL projector | Go count 후 update 금지 |
| Metrics/audit | transition service | policy side effect 금지 |

## 스키마 범위

필수 schema 변경은 delivery `row_version` 하나입니다.

추가하지 않는 항목:

- outbox `row_version`
- persisted logical delivery key
- `state_changed_at`
- 별도 `error_code`
- 별도 `send_started_at`

Logical key는 현재 canonical post resolver와 room/outbox 필드에서 계산합니다. Query evidence가 unbounded scan 또는 운영 부하를 보여 줄 때에만 normalized key/index를 별도 결정으로 검토합니다.

## 배포 호환성

| 단계 | Schema | Binary | 허용 여부 |
|---|---|---|---|
| Migration 전 | version 없음 | 기존 | 허용 |
| Migration 후 | version default 0 | 기존 | 허용 |
| Cutover 후 | version CAS | 새 단일 instance | 목표 |
| Rollback | column 유지 | 기존 단일 instance | 허용 |
| Old/new 동시 writer | 어느 schema든 | 두 egress binary | 금지 |

Cutover 전 모든 `SENDING`을 drain 또는 quarantine합니다. Additive column은 binary rollback 뒤에도 유지합니다.

## 안전성과 가용성 선택

보장하는 속성:

1. Stale worker가 최신 row를 finalize하지 못합니다.
2. Same logical delivery의 same-batch leader는 하나입니다.
3. Durable `SENDING`/`QUARANTINED` sibling이 same-room resend를 막습니다.
4. Outcome unknown은 자동 retry되지 않습니다.
5. Provider success 뒤 DB 오류가 provider resend를 만들지 않습니다.
6. Success tracking은 exact operation commit을 따릅니다.
7. Child outbox writer가 하나입니다.

의도적으로 포기하는 자동 가용성:

- Provider 결과 불명에서는 자동 resend를 포기합니다.
- Commit `Indeterminate`에서는 자동 overwrite를 포기합니다.
- `QUARANTINED`는 audited reconciliation 전까지 terminal로 남습니다.

## 재검토 조건

다음 중 하나가 발생하면 다시 검토합니다.

1. Provider가 stable request ID 결과 조회 또는 replay dedupe 계약을 제공합니다.
2. `QUARANTINED` 운영 처리가 반복되어 reconciliation workflow가 필요합니다.
3. Query evidence가 computed logical-key sibling lookup의 부하를 보여 줍니다.
4. YouTube store에 두 번째 production runtime consumer가 생깁니다.
5. 검증된 FSM library가 library-review 재검토 gate를 모두 만족합니다.
6. Alarm-worker replica>1 게이트가 별도 결정으로 해소됩니다.
