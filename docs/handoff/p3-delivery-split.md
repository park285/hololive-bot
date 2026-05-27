# P3 Handoff: delivery/ sub-package split

## Status: COMPLETE

## Context

- **Plan**: `/home/kapu/.claude/plans/dazzling-scribbling-tower.md` → P3
- **Branch**: `refactor/p3-delivery-split` (in `hololive-bot` submodule)
- **Scope**: `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/`
- **Goal**: 53 source files, 9,120 LOC flat package → cohesive sub-packages

## Result

9,120 LOC → 7,888 LOC root + 3 sub-packages (1,494 LOC extracted, -16.4%).
Root는 orchestration core (9개 struct 양방향 참조 + 126건 unexported type cross-ref)로 추가 분할 비용 > 이득.

### Final Structure

```
delivery/
├── analytics/   (504 LOC) — PostSendCount 등 5개 타입 + Build* 순수 함수
├── timeline/    (523 LOC) — PostDeliveryTimeline 등 12개 타입 + BuildPostLatencyClassification
├── format/      (467 LOC) — MessageFormatter + DispatchPayloadFormatter
├── (root)       (7,888 LOC) — Dispatcher, ClaimManager, SendEngine, AuditLogger 등 orchestration core
```

### Extraction Log

| Wave | Sub-package | LOC | Commit | Pattern |
|---|---|---|---|---|
| 1 | `analytics/` | 504 | 2d08edbb | type alias + var alias |
| 1 | `timeline/` | 523 | 2d08edbb | type alias + const alias |
| 2 | `format/` | 467 | 9d8095b5 | wrapper delegation (unexported method test 호환) |

### Deferred Targets (비용 > 이득)

| Target | LOC | Reason |
|---|---|---|
| `repository/` | ~885 | DeliveryRepository 메서드가 root 파일 3개에 분산, deliveryClaimToken 참조 |
| `telemetry/` | ~1,014 | DeliveryTelemetryRepository 36개 메서드 across 11 파일 (Go 메서드 제약) |
| `observability/` | ~1,032 | AuditLogger → TelemetryProcessor → DeliveryTelemetryRepository 양방향 참조 |
| `claim/` | ~1,268 | ClaimManager가 10+ 내부 타입 참조, 전체 orchestration 허브 |
| `grouping/` | ~289 | Config 순환 의존성, deliveryGroup 6개 파일에서 참조 |

### Design Decisions

1. **Type alias pattern**: `type X = subpkg.X` — root에서 alias 유지하여 기존 코드/테스트 무수정
2. **Var alias for functions**: `var F = subpkg.F` — 함수 re-export
3. **Wrapper delegation (format/)**: root에 wrapper struct 유지, format.MessageFormatter에 위임. unexported 메서드 테스트 호환성 보존.
4. **Helper 복사**: sub-package → parent import 불가. analytics/에 `CloneUTCTimePtr`, timeline/에 `clonePostLatencyInt64` 복사.
5. **Orchestration core 유지**: 남은 root 파일들은 struct 메서드 + unexported 타입 공유로 분할 불가. 이는 의도된 설계 — 컴포넌트 간 내부 상태 접근이 정당한 영역.

## Verification

```
go build ./hololive/hololive-shared/... — pass
go test ./hololive/hololive-shared/... — pass
./scripts/architecture/ci-boundary-gate.sh — pass
```
