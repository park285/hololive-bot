# Plan H — Hololive Dispatcher Retry Queue Sentinel Adoption

**Date:** 2026-05-23
**Status:** Proposed (Plan B Task 4 escalate)
**Origin:** Plan B Task 4의 stop condition 발동. dispatcher retry가 in-process가 아니라 DB queue/state-machine model로 확인됨 → 단순 `errors.Is(err, iris.ErrPermanent)` 분기로 처리 불가.
**Independent of:** Plan A (이미 완료), Plan B (Task 3/5만 적용), C, D, E, F, G

---

## Goal

hololive dispatcher의 DB-driven retry queue가 iris-client-go v0.13.0 sentinel(`iris.ErrPermanent`, `ErrAuthFailed`)을 인식해 영구 실패는 retry 없이 즉시 `permanent_failure` 상태로 전이하도록 구현한다.

## Background

Plan B Task 4 worker가 발견:
- `dispatcher_claim.go:279` — 실패 bucket을 `MarkFailedRetryBatch`로 넘김
- `delivery_repository.go:267` — `attempt_count`, `next_attempt_at`, terminal `FAILED` 상태 update
- retry 결정은 in-process가 아닌 **DB row 상태 + scheduler**가 담당

단순한 in-process retry skip이 아니라 다음 변경 필요:
1. failure bucket 분류에 sentinel 신호 포함
2. `MarkFailedRetryBatch` 또는 동등 함수에 "permanent" flag 추가
3. repository update에서 permanent flag 받으면 `attempt_count` 무관하게 즉시 `FAILED` 상태로

이는 schema/repository 영역 touching이라 별도 plan으로 분리됨.

## Architecture

- **단계 1 (read-only mapping):** dispatcher_claim → MarkFailedRetryBatch → delivery_repository call chain 매핑. row state machine 다이어그램.
- **단계 2 (interface 확장):** `MarkFailedRetryBatch`에 `PermanentFailures []permanentFailure` 파라미터 또는 별도 메서드 `MarkPermanentFailureBatch`.
- **단계 3 (repository 구현):** permanent 케이스에서 `attempt_count = max_attempts`, `status = FAILED`, `next_attempt_at = NULL` 직접 set.
- **단계 4 (caller wire):** `dispatcher_send_flow.go`의 failure bucket 분류 시 `errors.Is(err, iris.ErrPermanent) || errors.Is(err, iris.ErrAuthFailed)` 케이스를 permanent bucket으로.

## Tech Stack

Go 1.26.3, postgres + pgx, hololive-shared retry queue.

## Execution

`executing-plans` (small scope). 단일 worker. hololive-bot worktree.

---

## Success Criteria

1. iris `ErrAuthFailed`/`ErrPermanent` 응답 시 outbox row가 `attempt_count` 무관하게 `permanent_failure`로 즉시 전이.
2. iris `ErrRetryable`/`ErrRateLimited`/`ErrTransport` 응답은 기존 retry 동작 유지.
3. 신규 단위 테스트: sentinel별 bucket 분류 + repository 전이 시나리오.
4. `go test ./hololive/hololive-shared/pkg/service/youtube/outbox/... -count=1 -race` PASS.
5. `./build-all.sh --no-bump` PASS (별도 pre-existing budget violation은 본 plan 범위 외).

## File Map

- 분석: `dispatcher_claim.go`, `dispatcher_send_flow.go`, `delivery_repository.go`
- Modify: `dispatcher_send_flow.go` — sentinel 분류 → bucket 결정
- Modify: `delivery_repository.go` — permanent flag 처리
- Modify: `dispatcher_claim.go` — MarkPermanentFailureBatch 호출 추가
- Test: 신규 unit test

---

## Task 1 — call chain mapping (read-only)
- [ ] `dispatcher_claim.go:279` → `MarkFailedRetryBatch` 호출처와 인자 매핑
- [ ] `MarkFailedRetryBatch` interface + implementation 확인
- [ ] `delivery_repository.go:267` row update SQL 캡처
- [ ] state machine 다이어그램 (idle → claimed → success | failed_retry → ... → permanent_failure)

## Task 2 — sentinel-aware bucket 분류
- [ ] RED: `TestDispatcherFlowCategorizesPermanentSentinel` (auth/permanent → permanent bucket)
- [ ] GREEN: `dispatcher_send_flow.go`의 failure bucket 분류 함수에서 sentinel 분기

## Task 3 — repository permanent update
- [ ] RED: `TestRepository_MarkPermanentFailureBatch_ImmediatelySetsFAILED`
- [ ] GREEN: 신규 메서드 `MarkPermanentFailureBatch` 또는 기존 메서드 시그니처 확장
- [ ] 회귀 테스트: 기존 retry 동작 영향 0

## Task 4 — wire end-to-end
- [ ] `dispatcher_claim.go`에서 permanent bucket 발생 시 새 repository 메서드 호출
- [ ] integration test (DB sandbox) — auth 실패 시 outbox row가 즉시 FAILED 상태

## Task 5 — 검증 + metric
- [ ] `delivery_failure_reason` metric의 `auth`/`http-permanent` 라벨이 permanent 분기와 일치하는지 확인 (Plan B Task 3 결과와 정합)
- [ ] `go test ./hololive/hololive-shared/pkg/service/youtube/outbox/... -race`
- [ ] `./build-all.sh --no-bump` (budget violation은 별도 follow-up)

---

## Validation

```bash
cd /home/kapu/work/iris-stack/hololive-bot
go test ./hololive/hololive-shared/pkg/service/youtube/outbox/... -count=1 -race
IRIS_CLIENT_GO_WORKSPACE_PATH=/home/kapu/work/iris-stack/iris-client-go ./build-all.sh --no-bump
```

## Stop Rules

- repository 변경이 production migration 필요 → 별도 schema-change plan으로 분리
- state machine 다이어그램에서 race가 발견되면 사전 design review 필요

## Risk Gates

| Gate | Trigger | Mitigation |
|---|---|---|
| **Schema migration** | repository column 추가 시 | 가능하면 신규 column 없이 status enum + attempt_count로 처리. 추가 column 시 별도 migration plan. |
| **Retry behavior change** | permanent bucket이 너무 광범위해 정상 retry도 skip | sentinel만 정확히 분류 (Plan B Task 3의 deliveryFailureReason와 동일 표준) |
| **Concurrent dispatchers** | 다중 dispatcher 인스턴스의 race | DB row lock + atomic update |
