# 2026-05-21 — Monorepo Refactor Master Plan

Cross-runtime refactoring plan for the `hololive-bot` monorepo. Phase 1 (this document) records goals, scope, sequencing, and stop rules. Phase 2 executes sub-plans in subagent-driven mode in separate sessions, one sub-plan per session.

## Goal

`hololive-bot` 모노리포 전체 8개 트리(5 Go runtime + 2 shared Go lib + admin-dashboard 풀스택)에 대해 다음 4축의 일관성·건전성을 끌어올린다.

1. **구조·경계** — `docs/architecture/file-loc-thresholds.txt` 위반/근접 해소, 함수 예산(60줄 / 복잡도 8 / 중첩 5) 준수, `internal/` 경계 명확화.
2. **네이밍·시그니처** — 약어 혼용/패키지-타입 중복/케이스 드리프트 제거, 모듈 간 유사 개념 동일 어휘화.
3. **테스트** — 비즈니스 핵심 경로(poller/observation/queue/dispatch/handler) 커버리지 보강.
4. **공통화** — 모듈 간 중복(ticker loop, exponential backoff, claim cache, HTTP lifecycle, error-wrap+log, slog setup) 제거.

본 plan 은 outcome-first 다. 모든 phase 는 PR-단위로 절단 가능하고, 각 phase 의 success criteria + validation command + stop rule 을 명시한다.

## Non-goals

- 기능 변경(behavior change) 자체가 목적인 작업은 포함하지 않는다. 단, 리팩토링 도중 발견된 명백한 버그는 별도 commit/PR 로 분리.
- DB 스키마 변경, contract 변경, 외부 API 호환성 영향 작업은 본 plan 의 범위를 벗어난다. 필요 시 별도 plan 으로 분리.
- 배포·재기동·secret/credential 작업은 본 plan 안에서 자동 수행하지 않는다. 운영 작업은 사용자 명시 승인 후 단계.

## Success criteria (전체 plan 기준)

각 phase 종료 시 다음을 모두 만족하지 않으면 phase 미완료다.

1. 변경 범위 내 `./build-all.sh --no-bump` 통과.
2. 변경 모듈에 대해 `go build ./...` + `go test ./...` 통과 (admin-dashboard Rust: `cargo build --release` + `cargo test`; frontend: `npm run typecheck` + `npm run test -- --run`).
3. 변경 파일에 대해 `docs/architecture/file-loc-thresholds.txt` OVER 항목 0개 유지 또는 감소.
4. 변경된 공개 식별자(rename, signature change) 가 호출부 전부에서 빌드 통과.
5. PR 본문이 `git-pr-conventions` (한국어, 변경 동기 중심) 형식을 따른다.
6. 회귀 차단을 위한 신규 테스트가 phase 설명에 명시된 분량만큼 추가됐다.

## Scope — 8 trees / 7 Go modules + 1 fullstack

| # | Tree | Lang | 핵심 부담 (inventory 요약) |
|---|------|------|----------------------------|
| 1 | `hololive/hololive-kakao-bot-go` | Go | UNLISTED-LARGE 5건(streamfeed/transport/formatter), bootstrap 테스트 부재. |
| 2 | `hololive/hololive-admin-api` | Go | `api_member.go` 430/430 OVER, lifecycle 테스트 부재, 4건 unlisted >300. |
| 3 | `hololive/hololive-alarm-worker` | Go | `youtube_checker.go` 93%, `notifier.go` 89%, checker/checking 패키지 분할 모호. |
| 4 | `hololive/hololive-llm-sched` | Go | bootstrap/repository/filter UNLISTED-LARGE, `Scheduler` 동명 충돌. |
| 5 | `hololive/hololive-youtube-producer` | Go | community shorts reports 6건 380+ LOC, active-active 케이스 드리프트. |
| 6 | `hololive/hololive-shared` | Go | 658 파일 / 24.6% 테스트 ratio, 타입-alias 브리지 40+, claim/observation 미커버. |
| 7 | `shared-go` | Go | thresholds 파일 stale(`logging.go:520`→215), operation/id 미커버. |
| 8 | `admin-dashboard` | Rust + TS | `handlers/auth.rs` 1308/1320, frontend `DockerContainerList.tsx` 235% OVER. |

세부 inventory: `docs/agent-workflows/plans/_inventory-2026-05-21/01-08-*.md` 참조.

## Cross-cutting findings — 모듈 간 중복 패턴

Phase 2 의 leverage 가 가장 큰 지점. 단일 모듈 작업 전에 처리하면 후속 모듈 작업이 짧아진다.

- **Ticker loop + exponential backoff**: lease(`youtube-producer`), photo sync, polltarget refresh, recovery loop, scheduler tick 곳곳. 공통 helper 후보: `runTickerLoop`, `nextExponentialBackoff`.
- **Lease/claim 토큰 + 재사용 캐시**: `youtube-producer/internal/runtime/ingestionlease`, `alarm-worker` notifier/dispatch_runner, `hololive-shared` outbox `dispatcher_claim_gate.go`, observation `alarm_state_repository_claim.go`. 세 곳 모두 token + CompareAndExpire + backoff. 공통 추상화 후보.
- **HTTP server lifecycle**: kakao-bot, admin-api, alarm-worker, youtube-producer 모두 listen + shutdown + signal 패턴을 모듈마다 재구현. `shared-go/pkg/runtime/lifecycle` 헬퍼가 있으나 wrapping 일관성 부족.
- **Error wrap + log**: `hololive-shared` 만 1378회 `logger.Error(...); return fmt.Errorf("op: %w", err)` 패턴. `logAndWrapError(ctx, logger, op, err)` helper 후보 (단, slog 컨벤션 침해 주의).
- **Slog setup**: admin-api/kakao-bot/llm-sched/youtube-producer 가 모듈마다 slog 초기화. `shared-go/pkg/logging` 의 입구 표준화 가능.
- **Queue 소비 루프**: `alarm-worker` dispatch_runner ↔ `hololive-shared/pkg/service/alarm/queue/consumer`. 두 루프가 dedup claim / DrainBatch / MarkDispatched 패턴 공유.
- **CORS/origin 검증**: admin-api `internal/app/http/middleware.go` ↔ hololive-shared `pkg/server/middleware`. 중복.
- **Type-alias bridge**: `hololive-shared/pkg/service/youtube/outbox/outbox.go` 5–42 의 40+ alias 가 read-through 부담. 정책 결정 필요(유지/축소/단일 surface 패키지).
- **Threshold 파일 drift**: `shared-go/pkg/logging/logging.go:520` (현재 215), `hololive-shared` 의 980/650 등 과대-baselined 항목. Phase 2 도입 시 재baseline.

## Phase 시퀀스 (Phase 2 실행 순서)

다음 순서로 진행. 각 phase 는 한 세션 = 한 plan = 한 PR 원칙.

```
Phase 2.A — 공통 기반(shared-go, hololive-shared 공용 surface)
  ├ 2.A.1 shared-go 미커버 모듈 테스트 + 임계 파일 재baseline
  └ 2.A.2 hololive-shared type-alias bridge 정책 결정 + 적용

Phase 2.B — 공통 helper 추출 (cross-cutting)
  ├ 2.B.1 runTickerLoop / nextExponentialBackoff 공통화
  ├ 2.B.2 lease/claim 캐시 공통화 (3개 구현 → 1개)
  ├ 2.B.3 HTTP server lifecycle 헬퍼 단일화
  └ 2.B.4 logAndWrapError + slog 표준화

Phase 2.C — 모듈별 LOC/함수 budget 정리 (parallel-safe)
  ├ 2.C.1 hololive-shared      (sub-plan: refactor-hololive-shared.md)
  ├ 2.C.2 hololive-kakao-bot-go (sub-plan: refactor-hololive-kakao-bot-go.md)
  ├ 2.C.3 hololive-admin-api    (sub-plan: refactor-hololive-admin-api.md)
  ├ 2.C.4 hololive-alarm-worker (sub-plan: refactor-hololive-alarm-worker.md)
  ├ 2.C.5 hololive-llm-sched    (sub-plan: refactor-hololive-llm-sched.md)
  ├ 2.C.6 hololive-youtube-producer (sub-plan: refactor-hololive-youtube-producer.md)
  ├ 2.C.7 admin-dashboard backend (sub-plan: refactor-admin-dashboard.md)
  └ 2.C.8 admin-dashboard frontend (same sub-plan, separate phase)

Phase 2.D — 네이밍 스위프 (after 2.C)
  └ 약어/케이스/패키지-타입 중복 단일 PR.

Phase 2.E — 테스트 커버리지 백필 (after 2.C)
  └ poller / observation / queue / bootstrap 등 미커버 핵심 경로.
```

2.A → 2.B 는 직렬. 2.C 의 sub-phase 들은 **parallel-safe** (서로 다른 모듈) 이지만, 한 세션 = 한 모듈 원칙 유지. 2.D, 2.E 는 2.C 완료 후.

## Sub-plan 파일 매핑

각 sub-plan 은 본 master 와 동일 디렉토리에 위치한다.

- `2026-05-21-monorepo-refactor-hololive-kakao-bot-go.md`
- `2026-05-21-monorepo-refactor-hololive-admin-api.md`
- `2026-05-21-monorepo-refactor-hololive-alarm-worker.md`
- `2026-05-21-monorepo-refactor-hololive-llm-sched.md`
- `2026-05-21-monorepo-refactor-hololive-youtube-producer.md`
- `2026-05-21-monorepo-refactor-hololive-shared.md`
- `2026-05-21-monorepo-refactor-shared-go.md`
- `2026-05-21-monorepo-refactor-admin-dashboard.md`

각 sub-plan stub 은 다음 골격을 따른다.
- Goal (1–2 줄)
- Inventory link (`_inventory-2026-05-21/NN-*.md`)
- Target work (LOC + 함수 + 테스트 + 네이밍 + 중복 항목)
- File map (실제 변경 대상 파일 경로)
- Validation commands (모듈 빌드 + 테스트 + LOC gate)
- Stop rules
- Out of scope

Cross-cutting Phase 2.A / 2.B 작업은 별도 sub-plan 으로 분리되지 않는다. Phase 2.C 진입 전, 본 master 의 cross-cutting 섹션을 직접 참조하여 한 세션 = 한 cross-cutting unit (예: `2.B.1`) 으로 진행한다.

## Validation commands

각 phase 종료 시 모두 실행한다.

```bash
./build-all.sh --no-bump
go build ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-admin-api/... \
         ./hololive/hololive-alarm-worker/... ./hololive/hololive-kakao-bot-go/... \
         ./hololive/hololive-llm-sched/... ./hololive/hololive-youtube-producer/...
go test ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-admin-api/... \
        ./hololive/hololive-alarm-worker/... ./hololive/hololive-kakao-bot-go/... \
        ./hololive/hololive-llm-sched/... ./hololive/hololive-youtube-producer/...
./scripts/architecture/ci-boundary-gate.sh   # 구조 변경 phase 종료 시
```

admin-dashboard 가 변경되는 phase 만 추가:
```bash
( cd admin-dashboard/backend && cargo build --release && cargo test )
( cd admin-dashboard/frontend && npm run typecheck && npm run test -- --run )
```

테스트 커버리지 백필 phase 종료 시:
```bash
go test -cover ./<changed packages>
```

## Stop rules

다음 중 하나라도 발생하면 해당 phase 를 **stop** 한다. 사용자 확인 후 재개.

1. `./build-all.sh --no-bump` 가 기존 main 에서 실패하는 동안 추가 작업.
2. `go test ./...` 의 *변경 모듈* 패키지가 빨강이고 원인이 본 plan 의 변경에 직접 기인.
3. 임계 파일에서 OVER 항목이 phase 시작 전보다 증가.
4. 단일 PR 가 변경 모듈 외 경로(설정/배포/secret/contract) 를 의도치 않게 건드림.
5. 외부 contract(공개 API 응답 shape, queue payload, DB 스키마, 파일 포맷) 변경 필요성이 발견됨 — 본 plan 범위 외이므로 사용자 알림 후 별도 plan.
6. 호환성 깨짐 — 다른 모듈에서 import 하는 식별자 rename/제거가 호출부에서 빌드 실패 발생.

## Risk register

- **타입-alias bridge 정책 변경(Phase 2.A.2)**: 40+ alias 의 collapse 는 호출부 폭주 가능. *완화*: 우선 ban-list 만 만들고, alias 1개씩 작은 PR 로.
- **lease/claim 캐시 통합(Phase 2.B.2)**: 3개 구현체가 미묘하게 다른 TTL/재시도 정책 보유. *완화*: 동작 보존 단언 테스트 우선 작성.
- **slog 표준화(Phase 2.B.4)**: 로그 라인 모양 변경 시 alert/grep 룰 깨질 위험. *완화*: log key 변경 없이 wrapper 만, 또는 명시적 schema 문서화 후 변경.
- **공통 helper 도입 시 abstraction premature 위험**: 3건 이상 동일 패턴 + 호출부 단순화가 명백할 때만 추출. 2건 중복은 그대로 둔다.

## Phase 2 운영 규칙

- 한 세션 = 한 sub-plan = 한 PR. 세션 시작 시 `executing-plans` 또는 사용자 명시 시 `subagent-driven-development` 진입.
- subagent-driven 모드에서 implementer subagent 는 한 task 단위만 처리. parent 는 spec-compliance + code-quality reviewer subagent 로 task 종료마다 검증. 최종 통합/PR 본문은 parent 책임.
- push / PR open / merge / branch delete 는 사용자 명시 승인 후. 본 plan 은 빌드·테스트·로컬 commit 까지만 자율 권한.
- 회귀 차단을 위해 모든 task 끝에서 위 validation commands 일괄 실행.

## 산출 상태

- Inventory 8건 완료: `docs/agent-workflows/plans/_inventory-2026-05-21/01..08-*.md`
- Master plan: 본 파일.
- Sub-plan stub 8건: 다음 commit 에 동반 생성.
- Phase 0(작업 트리 정리): 완료 (e8d32b98 기준 main HEAD).
- Phase 1(plan 작성): 본 commit.
- Phase 2 진입 전 사용자 승인 필요.
