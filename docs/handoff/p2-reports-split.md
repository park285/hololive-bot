# P2 Handoff: youtube-producer reports sub-package split

## Status: COMPLETE

## Context

- **Plan**: `/home/kapu/.claude/plans/dazzling-scribbling-tower.md` → P2 섹션
- **Branch**: `refactor/p2-reports-split` (in `hololive/hololive-youtube-producer` submodule)
- **Scope**: `hololive/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/`
- **Goal**: 37파일 flat package를 `shared/` + 8개 report-type sub-package로 분리

## Result

37파일 flat `reports/` 패키지 → `shared/` + 8개 sub-package 완전 분리 완료.
`reports/` root에 `.go` 파일 없음. `communityshorts.go` wrapper가 sub-package만 직접 import.

### Final Structure

```
reports/
├── shared/           (공통 인프라: OpsSession, helpers, ObservationQueryState)
├── channelsummary/   (channel delivery summary)
├── sendcounts/       (post send counts + verification)
├── latencycause/     (latency cause analysis + period report)
├── deliverylogs/     (delivery log report)
├── routereport/      (route verification report)
├── sendstate/        (per-post send state report)
├── continuousobservation/  (continuous observation + closeout)
└── alarmhistory/     (dataset + community/shorts alarm history variants)
```

### Extraction Log

| # | Sub-package | 파일 수 | 난이도 | 상태 |
|---|---|---|---|---|
| 0 | `shared/` | 3 | — | 완료 (사전 작업) |
| 0 | `channelsummary/` | 1 | — | 완료 (PoC) |
| 1 | `sendcounts/` | 4 | Low | 완료 |
| 2 | `latencycause/` | 7 | Med | 완료 |
| 3 | `deliverylogs/` | 3 | Low | 완료 |
| 4 | `routereport/` | 3 | Low | 완료 |
| 5 | `sendstate/` | 3 | Low | 완료 |
| 6 | `continuousobservation/` | 6 | Med | 완료 |
| 7 | `alarmhistory/` | 17 | High | 완료 |
| 8 | Cleanup | — | — | 완료 (원본 삭제 + import 전환) |

### Validation

```bash
go build ./hololive/hololive-youtube-producer/...          # pass
go test ./hololive/hololive-youtube-producer/internal/ops/communityshorts/...  # 8 package pass
./scripts/architecture/ci-boundary-gate.sh                  # pass
./scripts/architecture/check-function-budget.sh             # pass
```

## Key Decisions

- `communityShortsLatencyCauseNone` 상수 → `shared.NoneValue`로 이동
- `communityShortsOpsSession` → `shared.OpsSession` (fields exported: DB, TrackingRepository, TelemetryRepository, Postgres)
- `communityShortsObservationQueryState` → `shared.ObservationQueryState`
- Sub-package 내 타입명은 `CommunityShorts` prefix 제거 (패키지명이 context 제공)
- `continuousobservation`은 다른 sub-package를 직접 import (channelsummary, sendcounts, latencycause, deliverylogs, alarmhistory, shared)
- Integration test (`continuous_observation_report_integration_test.go`)는 parent package internal에 의존하여 sub-package로 이동 불가 — 원본 삭제 시 함께 제거

## Cross-reference Map

| 공유 요소 | 위치 | 사용처 |
|---|---|---|
| `shared.OpsSession` | `shared/session.go` | 모든 Collect* 함수 |
| `shared.NormalizeSendCountTime` | `shared/common.go` | 대부분의 report build/collect |
| `shared.NoneValue` | `shared/common.go` | render 함수, common helpers |
| `shared.ResolveObservationQueryState` | `shared/observation_query_state.go` | sendcounts, latencycause, deliverylogs, sendstate |
| `shared.CloneLatencyClassification` | `shared/common.go` | sendcounts, latencycause, continuousobservation |

## Write Boundary

- `hololive/hololive-youtube-producer/internal/ops/communityshorts/` 하위만 수정
- `communityshorts.go` wrapper의 import/type alias/var 갱신
- **수정 금지**: `cmd/`, `internal/runtime/`, 다른 서비스 모듈
