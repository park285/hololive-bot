# 03. Phase map과 커밋 경계

## 전체 순서

```text
Phase 00. Preflight, 계약 고정, 위험 감사
Phase 01. PostgreSQL schema와 repository 추가
Phase 02. Publisher PublishBatch() 전환
Phase 03. Valkey O(1) wakeup 구현
Phase 04. dispatcher-go PostgreSQL wiring
Phase 05. PG consumer와 상태 머신 구현
Phase 06. Reconciliation, retention, admin tooling
Phase 07. Cutover, canary, rollback runbook
Phase 08. Valkey cache/index hygiene와 command guard
```

## Phase 00. Preflight

목표: 동작 변경 없이 계약과 audit을 추가합니다.

권장 커밋:

```text
00-A docs: final dispatch contract
00-B docs: Valkey command policy
00-C tests: legacy behavior golden tests
00-D audit: current Valkey command usage report
```

완료 조건:

- runtime behavior 변화 없음
- 기존 dispatch path 확인 완료
- 고복잡도 Valkey command audit 완료

## Phase 01. Schema/repository

목표: 2테이블 ledger와 repository를 추가합니다. 아직 runtime 사용은 하지 않습니다.

권장 커밋:

```text
01-A migration: alarm_dispatch_events/deliveries
01-B domain: event/delivery models and statuses
01-C repository: InsertBatch and LoadEventsByID
01-D repository: ClaimDue and status transitions
01-E repository: reconciliation/retention methods
01-F tests: PostgreSQL integration tests
```

완료 조건:

- events/deliveries table 존재
- event payload room-agnostic 검증
- ClaimDue는 payload join 없음
- MarkSending/MarkSent strict 조건 검증

## Phase 02. Publisher

목표: `PublishBatch()`를 기본 API로 만들고, mode별 publish 순서를 구현합니다.

권장 커밋:

```text
02-A API: PublishBatch interface and Publish wrapper
02-B grouping: event/delivery build and canonical hash
02-C mode: valkey_only/shadow/pg_first switch
02-D shadow: Valkey success then PG shadow insert
02-E pg_first: PG pending commit then wakeup
02-F metrics/tests
```

완료 조건:

- `PublishBatch()`가 `Publish()` 반복 호출이 아님
- shadow는 `shadowed`만 생성
- pg_first는 legacy queue LPUSH 금지
- wakeup 실패는 non-fatal

## Phase 03. Valkey O(1) wakeup

목표: Pub/Sub 대신 fixed-list token wakeup을 구현합니다.

권장 커밋:

```text
03-A interface: DispatchWakeupClient
03-B implementation: SET guard NX PX + LPUSH + PEXPIRE
03-C consumer: BRPOP single fixed key
03-D fallback: timeout scan integration
03-E command audit tests
```

완료 조건:

- PUBLISH 미사용
- BRPOP key 1개
- token payload 없음
- fallback scan 존재

## Phase 04. Dispatcher DB wiring

목표: dispatcher-go가 PG mode에서 DB repository를 사용할 수 있게 합니다.

권장 커밋:

```text
04-A config/env parsing
04-B pg pool lifecycle
04-C readiness behavior
04-D docker-compose/deployment env
04-E tests
```

완료 조건:

- consumer_mode=valkey에서 기존 동작 유지
- consumer_mode=pg에서 DB readiness 필수

## Phase 05. PG consumer/state machine

목표: PG delivery를 실제로 claim/send/ack합니다.

권장 커밋:

```text
05-A PG consumer skeleton and loop
05-B ClaimDue + LoadEventsByID integration
05-C render failure retry/DLQ path
05-D MarkSending + Iris send + MarkSent path
05-E ambiguous send quarantine path
05-F metrics/tests
```

완료 조건:

- shadowed claim 없음
- MarkSending 전 send 없음
- MarkSent 실패 후 retry 없음
- ambiguous send는 quarantine

## Phase 06. Reconciliation/retention/admin

목표: stale state와 terminal cleanup을 bounded batch로 처리합니다.

권장 커밋:

```text
06-A RecoverExpiredLeased job
06-B QuarantineStaleSending job
06-C retry exhausted DLQ job
06-D bounded retention jobs
06-E admin query/requeue helper with audit
06-F metrics/tests
```

완료 조건:

- unbounded UPDATE/DELETE 없음
- stale sending 기본 quarantine
- manual requeue audit 존재

## Phase 07. Cutover/runbook

목표: shadow -> pg_first 전환과 rollback을 문서/도구화합니다.

권장 커밋:

```text
07-A shadow verification checklist
07-B legacy queue drain helper/runbook
07-C pg_first canary checklist
07-D rollback runbook
07-E no-go config guard
```

완료 조건:

- 금지 조합이 문서화되거나 startup에서 경고/차단
- legacy queue drain 절차 존재
- rollback에서 sending quarantine 정책 명확

## Phase 08. Valkey cache/index hygiene

목표: dispatch 외 Valkey 사용도 role별로 정리하고 command guard를 둡니다.

권장 커밋:

```text
08-A Valkey key classification docs
08-B interface split
08-C command allowlist test
08-D index rebuild bounded path
08-E O(log N) exception comments
```

완료 조건:

- dispatch hot path generic client 노출 최소화
- 고복잡도 command audit 통과
- 예외 command에 bounded 근거 있음

## Cross-phase no-go

어느 phase에서도 다음은 허용하지 않습니다.

```text
- event payload에 room_id 저장
- shadowed를 자동 pending 승격
- PublishBatch 내부 Publish 반복
- pg_first에서 legacy active queue LPUSH
- alarm dispatch wakeup에 PUBLISH 기본 사용
- stale sending 자동 retry
- unbounded retention SQL
- metric label에 event_key/room_id/dedupe_key 사용
```
