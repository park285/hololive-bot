# Handoff: youtube-producer 다중 워커 최적화 — Phase 1 구현

## Status: READY (미착수)

## 역할

implement. Phase 1만 구현한다. Phase 2(live batch), Phase 3(source cooldown 전역화)는 범위 밖.

## Worktree / 시작 상태

- 작업 디렉터리: `/home/kapu/work/iris-stack/hololive-bot` (meta-repo `iris-stack`의 submodule. git 작업은 submodule 안에서)
- branch: `main`, HEAD: `0c0dc77f` (결정서 커밋 완료, working tree clean)
- 시작 전 repo의 `CLAUDE.md`/`AGENTS.md`를 읽고 따른다 (`slog`, `fmt.Errorf("action: context: %w", err)`, `context.Context` 첫 인자, 코드 주석 기본 금지).

## SSOT (기준 문서)

`docs/current/architecture/youtube-producer-multi-worker-optimization-decisions-20260604.md`

- 구현 범위: §6 "Phase 1. Source-aware budget과 lease 안전화" (파일맵 + 작업 체크리스트 15항목)
- 설계 근거: D-001~D-005, D-010~D-013, §7 코드 스케치, §8.1~8.3/8.5 테스트 결정
- 이 문서는 2026-06-04 코드베이스 대조 검증 완료 상태다 (파일맵 존재성, 식별자, executeJob 순서 `scheduler_worker.go:61-93`, `jobClaimLeaseTTL` `:175-181`, env canonical/alias, unique index 등). 근거 사실을 재발굴할 필요 없이 신뢰하고 시작해도 된다.
- `docs/youtube_producer_multi_worker_optimization_decisions (1).md`는 동일 내용 미러본. 문서를 수정하게 되면 두 파일을 byte 단위 동일하게 유지한다 (`diff -q`로 확인).

## Write boundary

문서 §6 Phase 1 파일맵의 파일만 생성/수정한다.

- Create 4: `poller/internal/budget.go`, `polling/budget_validator.go`, `polling/global_budget_limiter.go`(+`_test.go`)
- Modify 21: 파일맵에 열거된 경로 그대로

boundary 밖 파일이 필요해지면 작업을 멈추고 보고한다. 리팩터링·정리 등 scope bleed 금지.

## 확정 경계 (구현 중 임의 변경 금지 — §1.1)

1. 기존 scraper/Holodex per-request limiter를 제거·대체하지 않는다.
2. `GlobalBudgetLimiter`는 scheduler admission/in-flight gate다. 긴 sleep으로 claim을 붙잡지 않는다.
3. `BudgetReservation` Commit/Release terminal semantics, readiness payload contract, `SCRAPER_SCHEDULER_WORKER_COUNT` canonical env, rollback flag 이름들을 바꾸지 않는다.
4. budget exhausted / source cooldown은 readiness failure가 아니다. fail-closed는 Valkey backend unavailable에만 적용한다.

## Stop rules (§6.1)

다음이 필요해지면 구현을 중단하고 문서 갱신을 먼저 요청한다:

- 기존 request limiter 제거
- runtime token bucket 필수화
- readiness HTTP status contract 변경
- 새 worker env alias
- Lua reservation cleanup의 비-idempotent inflight 보정

## Validation (완료 주장 전 필수 실행)

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/poller/internal -run 'TestScheduler|TestJobClaim|TestRunJobClaim|TestBudget|TestMetrics'
go test ./hololive/hololive-shared/pkg/providers -run 'Test.*Scheduler|Test.*Budget'
go test ./hololive/hololive-youtube-producer/internal/runtime/polling -run 'Test.*Budget|Test.*Registration|Test.*Backfill'
go test ./hololive/hololive-youtube-producer/internal/runtime/publishedat -run 'Test.*Registration|Test.*Resolver'
go test ./hololive/hololive-youtube-producer/internal/runtime/readiness -run 'TestStateResponse|Test.*Readiness'
go test ./hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime -run 'Test.*Readiness|Test.*Build.*YouTube'
```

release 후보:

```bash
go build ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-youtube-producer/...
go test ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-youtube-producer/...
```

성공 기준은 문서 "Phase 1 성공 기준" 9항목을 체크리스트로 검증한다.

## 금지

- 배포/restart/원격 push/PR 생성 (별도 승인 필요)
- Phase 2/3 선반영, 문서 결정사항 임의 수정

## 완료 보고 형식

- Outcome (completed / blocked)
- 변경 파일 목록
- 체크리스트 갱신 상태 (§6 Phase 1)
- 실행한 validation 명령과 결과
- 남은 blocker
