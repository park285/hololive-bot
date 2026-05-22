# 2026-05-21 — hololive-youtube-producer refactor (Phase 2.C.6)

Sub-plan of `2026-05-21-monorepo-refactor-master.md`.

## Goal

community shorts reports 6건의 380+ LOC 파일을 분할하고, active-active 케이스 드리프트 + Lua 스크립트의 `redis` → `valkey` 명명을 정리한다. lease/recovery/photo-sync 의 ticker+backoff 코드를 cross-cutting helper(2.B.1) 로 흡수.

## Inventory

`docs/agent-workflows/plans/_inventory-2026-05-21/05-hololive-youtube-producer.md`

## Target work

LOC / 함수 budget:
- `internal/ops/communityshorts/internal/reports/community_shorts_alarm_sent_history_dataset_render.go` 416/420 NEAR — renderer 책임별 분할(table/cell/section).
- `internal/ops/communityshorts/internal/reports/community_shorts_route_report_build.go` 386/UNLISTED — baseline/indexing/aggregation 분리.
- `internal/ops/communityshorts/internal/reports/community_shorts_latency_cause_build.go` 380/UNLISTED — period builder vs comparison context 분리.
- `internal/runtime/ingestionlease/job_run_guard.go` 377/UNLISTED — Lua 상수 + Valkey lease 작업 책임 분리.
- `internal/ops/communityshorts/internal/reports/community_shorts_delivery_logs_report.go` 366/UNLISTED — delivery observation collection 분리.
- 함수: `renew`(33), `runRecoveryLoop`(30), `recoveryLoopIteration`(21) — backoff 헬퍼로.

테스트 보강:
- `internal/ops/communityshorts/internal/reports` 34 prod / 14 test — builder/renderer 핵심 경로 fixture 기반 테스트.
- `internal/runtime/polltarget` 11 prod / 3 test — `youtube_poll_target_refresh.go` (348) 테스트.
- `cmd/ops/internal/communityshortscli` 10 prod / 4 test — latency_cause/latency_period 파서 + 플래그 테스트.
- `internal/ops/communityshorts` 1 prod / 0 test — 진입 dispatcher.

네이밍 단일화:
- `ActiveActiveEnabled`/`ActiveActiveInstance` 와 `activeActive`/`instanceID` 의 casing 일치 (PascalCase 채택).
- 텔레메트리 reason string `"valkey_unavailable_active_active_fail_closed"` 같은 snake_case 와 코드 식별자 분리 정책 명시.
- Lua script 안의 `redis.call()` → `valkey` 호환 식별자로 정정 또는 주석 추가(외부 valkey-go 호환 보장).
- `JobRunGuard` 두 구현(`internal/runtime/polling/job_run_guard_claimer.go` vs `internal/runtime/ingestionlease/job_run_guard.go`) 책임 명명 차별화 또는 통합.
- `OutboxCount` field vs `OutboxKind` enum — 보고 도메인 plural/singular 정렬.

중복 → cross-cutting:
- Ticker renewal loop 3건(lease/photo_sync/polltarget) → Phase 2.B.1 `runTickerLoop`.
- Exponential backoff 2건(`nextRecoveryBackoff`, `leaseBackoffDelay`) → `nextExponentialBackoff`.
- Valkey/lease 스크립트 → Phase 2.B.2 `LeaseScripts` + claim 캐시.
- CLI slog setup 2건 → `newCLILogger`.
- 컨텍스트+done 채널 패턴(photo_sync_guard, recovery_loop) → `runIsolatedLoop`.
- Job identity 상수(readinessProbe* / photoSyncLease*) → `JobIdentityFor(service)`.

## File map

```
internal/ops/communityshorts/                      # reports 분할, dispatcher 테스트
internal/runtime/ingestionlease/                   # lease + job_run_guard split, Lua 식별자 정리
internal/runtime/leases/                           # photo_sync_guard ticker helper 적용
internal/runtime/readiness/                        # recovery_loop casing/필드 정리
internal/runtime/polling/                          # job_run_guard_claimer 정명/통합 결정
internal/runtime/polltarget/                       # refresh 테스트
cmd/ops/internal/communityshortscli/               # CLI 로거/파서 helper
```

## Validation

```bash
./build-all.sh --no-bump
go build ./hololive/hololive-youtube-producer/...
go test  ./hololive/hololive-youtube-producer/...
./scripts/architecture/ci-boundary-gate.sh
```

## Stop rules

- active-active 동작(readiness/recovery/lease) 의미 변경이 의심되면 stop 후 회귀 테스트 우선.
- Lua script 호환성 변경 가능성이 발견되면 stop — valkey 런타임에서 동일 결과 보장 필요.
- community shorts report 출력 포맷 변화가 사용자/관리자 보고에 영향을 주면 stop.

## Out of scope

- Holodex / YouTube API 호출 변경.
- 활성-활성 정책의 의미 변경(예: lease TTL, fail-closed 정책).
- community shorts 통계 정의 변경.
