# YouTube egress lifecycle 직접 구현과 FSM 라이브러리 리뷰

검토일: 2026-08-31 KST  
적용 결정: `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership`  
상위 문서: [`youtube-egress-lifecycle-transition-ownership-20260831.md`](youtube-egress-lifecycle-transition-ownership-20260831.md)

## 질문

YouTube per-room delivery lifecycle의 전이 정책을 다음 중 어떤 방식으로 구현할지 검토합니다.

1. alarm-worker 내부의 도메인 전용 pure planner
2. 범용 Go FSM 라이브러리
3. persistence/workflow framework

평가는 “상태 전이를 표현할 수 있는가”가 아니라 **현재 PostgreSQL row model, 외부 메시지 부수효과, stale worker fencing, tracking transaction을 포함한 전체 구조를 더 단순하고 안전하게 만드는가**를 기준으로 합니다.

## 홀로봇 요구사항

### Hard gate

후보는 다음 조건을 모두 만족하거나, 얇은 adapter로 만족시킬 수 있어야 합니다.

| ID | 조건 |
|---|---|
| G-01 | PostgreSQL row가 current state의 유일한 정본이어야 합니다. |
| G-02 | transition planning이 DB/network/action 없이 실행되어야 합니다. |
| G-03 | caller-owned state를 받아 여러 delivery row에 하나의 immutable definition을 재사용할 수 있어야 합니다. |
| G-04 | expected state/version conflict를 persistence 계층이 일급 결과로 표현할 수 있어야 합니다. |
| G-05 | selected transition identity와 complete mutation plan을 만들 수 있어야 합니다. |
| G-06 | provider send를 transaction callback이나 entry/exit action에 넣도록 강제하지 않아야 합니다. |
| G-07 | 기존 delivery/tracking/outbox schema를 additive version/terminal/ledger 확장으로 유지할 수 있어야 합니다. |
| G-08 | grouped operation all-or-none와 partial batch result를 애플리케이션이 통제할 수 있어야 합니다. |
| G-09 | `OutcomeUnknown`에서 아무 상태도 즉시 쓰지 않는 선택을 표현할 수 있어야 합니다. |
| G-10 | 라이브러리 제거가 domain/repository public contract를 바꾸지 않아야 합니다. |

### Complete decision 요구

홀로봇에서 필요한 결과는 `next state` 하나가 아닙니다.

```text
selected rule ID
expected state/version/attempt/lock
next state/version/attempt
next_attempt_at
sent_at 또는 satisfied_at
claim release/retention 의미
tracking mutation 의미
failure code와 sanitized diagnostic
operation atomicity unit
```

후보가 이 결과를 직접 제공하지 않으면 나머지를 adapter가 만들어야 하며, 동일 조건을 machine guard와 decision builder에서 중복 계산해서는 안 됩니다.

## 후보 요약

검토 기준 시점의 upstream 상태입니다.

| 후보 | 검토 버전/상태 | 핵심 모델 |
|---|---|---|
| 직접 domain planner | 저장소 내부 구현 | typed snapshot/event/policy -> concrete decision |
| `open-ships/statemachine` | v1.3.2 | immutable generic `Machine.Next`, optional state owners/persistence |
| `qmuntal/stateless` | v1.8.0 | UML-style state machine, external storage callback, actions/guards |
| `looplab/fsm` | v1.0.4 | string state/event, state-owning FSM, callbacks |
| `faustbrian/golib/pkg/state-machine` | archived | structured transition/effect result, PostgreSQL CAS/history/outbox |
| `modernice/goes` | v0.9.0, active framework | event sourcing, CQRS, projection, saga |

재현 가능한 upstream snapshot:

| 후보 | 검토 ref | peeled commit |
|---|---|---|
| `open-ships/statemachine` | `v1.3.2` | `953865075bde6c51427d19b9de4cb7b6bcd7d3f6` |
| `qmuntal/stateless` | `v1.8.0` | `baed0e505321437ea631845ab7d67ea3cddc9647` |
| `looplab/fsm` | `v1.0.4` | `b45606994edbcf2b560e89f2c92a622ee76f9b26` |
| `faustbrian/golib` | archived `main` | `e8da5e4d7f83ee3526f4ccd504a1ecb2d7fa727a` |
| `modernice/goes` | `v0.9.0` | `cc74dee59121da141e3055ae2239258c66795ec0` |

Version/tag, repository archived state, source API는 2026-09-01에 각 upstream repository와 release/tag source에서 다시 확인했습니다.

Upstream 링크:

- <https://github.com/open-ships/statemachine>
- <https://github.com/qmuntal/stateless>
- <https://github.com/looplab/fsm>
- <https://github.com/faustbrian/golib/tree/main/pkg/state-machine>
- <https://github.com/modernice/goes>

## 직접 domain planner

### 형태

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

`DeliveryDecision`은 concrete sealed decision입니다. Repository가 받는 command는 `BeginSend`, `Retry`, `Fail`, `Sent`, `Defer`, `AlreadySatisfied`처럼 의도를 드러냅니다.

### 장점

- complete decision을 한 번에 만듭니다.
- preparation과 send phase의 의미를 domain 이름으로 드러냅니다.
- attempt/backoff/tracking/fence를 같은 rule 안에서 검토할 수 있습니다.
- generic persistence adapter가 필요 없습니다.
- `RuleID`를 audit와 metric에 직접 연결할 수 있습니다.
- 현재 5개 상태와 제한된 event 집합에서는 구현 규모가 작습니다.

### 위험

- enum exhaustiveness를 compiler가 자동 보장하지 않습니다.
- transition table/DOT export를 직접 제공하지 않습니다.
- policy가 여러 package로 다시 분산될 수 있습니다.

### 통제

- single `DeliveryPlanner` production entrypoint
- 전체 state/event matrix contract test
- sealed decision constructor
- architecture gate로 우회 writer 금지
- rule descriptor를 문서/검증용으로 노출하되 실행 engine으로 일반화하지 않음

## `open-ships/statemachine`

### 적합한 부분

- `Machine`이 current state를 소유하지 않습니다.
- typed generic state/event를 사용합니다.
- immutable compiled table을 여러 aggregate에 재사용할 수 있습니다.
- `Machine.Next`는 guard만 평가하고 `Do` effect를 실행하지 않습니다.
- 동일 `(From, Event)` 그룹에서 unguarded row 뒤의 unreachable row를 compile 시 거부합니다.
- core가 표준 라이브러리만 사용하며 race, coverage, static analysis, fuzz gate를 운영합니다.

### 맞지 않는 부분

`Machine.Next`의 핵심 반환값은 destination state입니다.

```go
next, err := machine.Next(ctx, current, event, input)
```

홀로봇은 다음 작업을 별도로 해야 합니다.

```text
attempt_count 증가
retry 소진 판단 결과와 rule ID 연결
next_attempt_at 계산
expected version/lock 구성
tracking mutation 구성
operation atomicity 결정
```

예를 들어 guard에서 `attempt+1 >= maxRetries`를 계산하고, `buildDecision`이 next state를 보고 attempt/due를 만들면 동일 정책이 두 위치에 존재합니다. Event를 `RetryScheduled`, `RetryExhausted`로 미리 나누면 caller가 이미 핵심 정책을 결정하므로 library는 전이 검증기 역할만 남습니다.

Guard가 mutable input에 decision을 기록하는 방식은 사용하지 않습니다. `Next`와 `Permitted`가 선택되지 않은 row의 guard도 평가할 수 있으므로 guard side effect는 library 계약과 맞지 않습니다.

### persistence 모듈 판단

Conditional store와 conflict 개념은 유용하지만 홀로봇의 기존 기능을 대체하지 않습니다.

- exact delivery schema
- operation-level grouped CAS
- community/shorts tracking transaction
- aggregate projection
- stale quarantine/revive

Provider send를 `Do` 또는 store transaction effect로 넣으면 PostgreSQL rollback이 외부 메시지를 취소할 수 없다는 문제가 숨겨집니다. 따라서 도입하더라도 core `Next`만 사용할 수 있으며, 이 경우 줄어드는 코드는 destination selection 정도입니다.

### 성숙도

코드와 품질 gate는 강하지만 2026년에 시작된 매우 새로운 프로젝트이고 공개 adoption과 maintainer redundancy가 충분히 누적되지 않았습니다. 최근 caller-owned effectful API를 순수 `Next`로 교체하는 breaking refinement도 있었습니다. 변경 방향은 타당하지만 production 핵심 의존성의 API 안정성 증거는 아직 부족합니다.

### 판정

기술 철학은 가장 가깝지만 현재는 **조건부 미채택**입니다.

## `qmuntal/stateless`

### 적합한 부분

- 비교적 성숙한 프로젝트와 release history
- guard, hierarchy, entry/exit, introspection, DOT export
- external state accessor/mutator 지원

### 맞지 않는 부분

- state와 trigger가 `any` 기반입니다.
- primary API가 `Fire`와 action callback 중심입니다.
- external storage는 getter/setter callback이며 expected version과 conflict가 일급 결과가 아닙니다.
- action 오류가 state change 이후 발생한 경우 rollback mechanism이 없음을 문서가 명시합니다.
- PostgreSQL transaction을 accessor/mutator에 전달하려면 application-specific context plumbing이 필요합니다.

홀로봇에 맞추려면 action을 사용하지 않고 external mutator 안에서 CAS를 직접 구현해야 하므로 library execution model의 이점이 사라집니다.

### 판정

성숙도는 높지만 durable row-state와 external-effect 경계가 맞지 않아 미채택입니다.

## `looplab/fsm`

### 적합한 부분

- 긴 사용 이력
- 단순한 event/state 선언
- callback ecosystem

### 맞지 않는 부분

- `FSM` 인스턴스가 `current string`을 직접 소유합니다.
- state/event와 callback key가 문자열 중심입니다.
- DB row를 로드할 때마다 FSM을 생성/설정하면 process memory에 두 번째 state copy가 생깁니다.
- version/CAS/transaction/result planning이 핵심 API가 아닙니다.

### 판정

In-memory object lifecycle에는 적합하지만 PostgreSQL source-of-truth인 홀로봇에는 미채택입니다.

## `faustbrian/golib/pkg/state-machine`

### 적합한 부분

기능적으로 가장 가까웠습니다.

- pure structured transition result
- transition ID와 inert effect plan
- lock version과 previous state를 검사하는 PostgreSQL CAS
- state/history/effect outbox atomic commit

### 맞지 않는 부분

- upstream repository가 archived입니다.
- 프로젝트와 외부 adoption이 매우 새롭습니다.
- 기본 PostgreSQL store가 자체 `state_machine_instances`, history, snapshots, outbox schema를 소유합니다.
- 기존 YouTube delivery/tracking/aggregate schema를 유지하려면 store를 다시 구현해야 합니다.

### 판정

설계 참고 자료로 사용하지만 production dependency로는 하드 게이트 탈락입니다.

## `modernice/goes`

`goes`는 FSM helper가 아니라 event sourcing/CQRS framework입니다. 도입하면 source of truth가 row state에서 event stream으로 바뀌고 projection, replay, schema migration, operational tooling을 함께 재설계해야 합니다.

현재 작업은 transition ownership 분리이며 event-sourced rewrite가 아닙니다.

### 판정

범위 과다로 미채택입니다.

## Hard gate 결과

| 조건 | 직접 planner | open-ships core | qmuntal | looplab | faustbrian |
|---|---:|---:|---:|---:|---:|
| G-01 DB 단일 정본 | 통과 | 통과 | 조건부 | 실패 | 통과 |
| G-02 pure planning | 통과 | 통과 | 부적합 | 부적합 | 통과 |
| G-03 caller-owned typed state | 통과 | 통과 | 부분 | 실패 | 통과 |
| G-04 optimistic conflict | 직접 구현 | adapter 필요 | adapter 필요 | 직접 지원 없음 | 통과 |
| G-05 complete decision | 통과 | 실패 | 실패 | 실패 | 통과 |
| G-06 external send 분리 | 통과 | `Next` 한정 통과 | 불편 | 불편 | 통과 |
| G-07 기존 schema 유지 | 통과 | 가능 | 가능 | 부적합 | 기본 store 실패 |
| G-08 operation atomicity | 통과 | 직접 구현 | 직접 구현 | 직접 구현 | custom store 필요 |
| G-09 no-write unknown | 통과 | application 구현 | application 구현 | 부적합 | 통과 가능 |
| G-10 dependency 격리 | 통과 | adapter로 가능 | adapter로 가능 | 어려움 | 어려움 |

## 가중 평가

5점 만점의 상대평가입니다. 점수는 library 일반 품질이 아니라 홀로봇 적합성을 나타냅니다.

| 기준 | 가중치 | 직접 planner | open-ships | qmuntal | looplab |
|---|---:|---:|---:|---:|---:|
| complete decision 적합성 | 20 | 5 | 3 | 2 | 1 |
| PostgreSQL/CAS 적합성 | 20 | 5 | 3 | 2 | 1 |
| external-effect 안전 경계 | 15 | 5 | 5 | 2 | 2 |
| 타입/정적 검증 | 10 | 5 | 5 | 2 | 1 |
| 운영 성숙도 | 15 | 4 | 1 | 5 | 5 |
| 기존 코드 통합 비용 | 10 | 5 | 3 | 2 | 1 |
| 감사/테스트 적합성 | 10 | 5 | 3 | 4 | 3 |
| 환산 점수 | 100 | **97** | **64** | **53** | **39** |

`faustbrian`은 기능 점수와 무관하게 archived hard gate로 제외합니다.

## 예상 코드 비용

### 직접 planner

```text
status/event/snapshot/decision type   120~180 LOC
policy/retry/revive                   200~320 LOC
adapter/error/rule descriptor          80~140 LOC
합계                                  400~640 LOC
```

### open-ships adapter

```text
transition table/guards               100~180 LOC
library adapter                        80~140 LOC
complete decision builder             180~280 LOC
domain type/error mapping               60~100 LOC
합계                                  420~700 LOC
```

CAS store, tracking transaction, aggregate SQL, crash-window tests는 두 선택 모두 별도로 필요합니다. LOC는 구현 전 추정이며 최종 채택 근거가 아니라 adapter가 자동으로 작아지지 않는다는 확인용입니다.

## 최종 판정

```text
채택:
- domain-specific pure DeliveryPlanner
- typed event/outcome/decision
- PostgreSQL version-fenced command store
- shared contract test

미채택:
- 범용 FSM production dependency
- library callback의 provider send
- library-owned current state
- generic mutation DSL
- 사내 범용 FSM framework
```

직접 구현은 NIH를 선택한 것이 아닙니다. 후보가 줄여 주는 부분은 destination selection이고, 홀로봇이 직접 유지해야 하는 complete mutation decision과 PostgreSQL/외부 효과 경계가 더 큽니다.

## 재검토 gate

다음 조건을 모두 만족하는 후보가 생기면 같은 contract suite로 PoC합니다.

1. 최소 12개월의 호환 release history가 있습니다.
2. active maintainer가 2명 이상입니다.
3. 외부 production adoption이 확인됩니다.
4. selected transition ID와 structured result를 반환합니다.
5. caller-owned state와 pure planning을 지원합니다.
6. optimistic conflict를 일급 결과로 제공합니다.
7. 기존 schema를 custom adapter로 유지할 수 있습니다.
8. provider effect를 engine 밖에 둘 수 있습니다.
9. direct implementation과 동일 contract test를 통과합니다.
10. adapter를 포함한 lifecycle production code가 직접 구현 대비 20% 이상 감소합니다.

한 조건이라도 충족하지 않으면 현재 결정을 유지합니다.
