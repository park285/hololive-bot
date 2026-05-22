# 2026-05-21 — hololive-shared refactor (Phase 2.C.1)

Sub-plan of `2026-05-21-monorepo-refactor-master.md`. **본 모듈은 모노리포 코드의 거점이므로 2.C 시퀀스에서 가장 먼저** (`2.C.1`) 처리한다.

## Goal

`hololive-shared` 의 type-alias bridge 정책 결정, 테스트 24.6% → 핵심 영역 보강, 모듈 간 중복(claim cache / errgroup dispatch / retry policy) 통합 surface 제공. 임계 파일 ceiling 의 stale 항목 재baseline.

## Inventory

`docs/agent-workflows/plans/_inventory-2026-05-21/06-hololive-shared.md`

## Target work

LOC / 함수 budget:
- `pkg/service/holodex/internal/holodexprovider/api_client.go` 485/UNLISTED — provider 분할.
- `pkg/service/notification/internal/alarmservice/alarm_cache.go` 423/UNLISTED — cache layer 책임 분리.
- `pkg/service/youtube/outbox/internal/delivery/delivery_observation_context.go` 409/420 NEAR — observation context 분해(`applyDeliveryTelemetryObservationContext` 54라인 + 28~31라인 matching/assignment 분리).
- `pkg/service/youtube/stats/stats_repository_milestone.go` 406/UNLISTED — milestone 책임 분리.
- `pkg/config/internal/settings/config_types.go` 396/UNLISTED — 도메인별 settings 분리.
- 임계 baseline 재조정: `dispatcher_send.go` 980→실제 611, `repository.go` 650→513, 기타 과대 baseline 항목 — `docs/architecture/file-loc-thresholds.txt` 동시 갱신.

테스트 보강:
- `pkg/service/youtube/poller/internal/polling` 38 prod / 12 test — `channel_stats_poller.go`, `community_shorts_detection.go`, `metrics.go`, `published_at_resolver_{candidate,controls}.go` 테스트.
- `pkg/service/youtube/tracking/internal/observation` 20 prod / 6 test — `alarm_state_repository*.go`, `observation_compare_{index,mismatch,sort}.go`, `community_alarm_sent_history.go` 테스트.
- `pkg/service/alarm/queue` 7 prod / 2 test — `consumer_delayed_retry.go`, `consumer_payload.go`, `consumer_primitives.go`, `consumer_retry.go`, `metrics.go` 테스트.
- `pkg/service/cache` 12 prod / 6 test — valkey/Redis 통합 시나리오 테스트(가능한 범위에서).
- `pkg/service/notification/internal/alarmservice` — `alarm_service.GetRoomAlarms*` 유닛 테스트.

네이밍 단일화:
- `pkg/service/youtube/outbox/outbox.go:5–42` 40+ alias 정책: 명시적 외부 surface 패키지(`youtube/types.go`) 로 단일화, 또는 alias 유지 시 doc.go 에 정책 기술.
- 인터페이스 vs 구조체 케이싱(예: `deliveryRepository` interface vs `DeliveryRepository` struct) — 명시적 컨벤션 doc 후 일괄.
- 리시버 약어 (`as`, `svc`, `s`, `r`) — 모듈 컨벤션 정의(예: 항상 `s`).
- 패키지명 `dispatchoutbox` 의 compound 명명 — 분리 또는 doc.
- Suffix `Service` 의 적용 컨벤션 명시.

중복 → cross-cutting 추상화 제공:
- Phase 2.B.2 claim/reuse cache helper 의 본거지(`pkg/service/youtube/outbox/internal/delivery/dispatcher_claim_gate.go` + `pkg/service/youtube/tracking/internal/observation/alarm_state_repository_claim.go` 통합).
- Phase 2.B.1 `runTickerLoop` / `nextExponentialBackoff` 의 본거지(`pkg/util/` 또는 `shared-go` 중 결정).
- Phase 2.B.4 `logAndWrapError` 의 본거지.
- errgroup+SetLimit 디스패치 generic — `parallelDispatch[T]`.
- 행 스캔 helper — gorm/pgx scan 통합.

## File map

```
pkg/service/youtube/outbox/                            # alias 정책 + claim helper 본거지
pkg/service/youtube/poller/internal/polling/            # 테스트 보강
pkg/service/youtube/tracking/internal/observation/      # 테스트 + claim helper
pkg/service/alarm/queue/                                # retry/consumer 테스트
pkg/service/alarm/dispatchoutbox/                       # scan helper
pkg/service/notification/internal/alarmservice/         # GetRoomAlarms 테스트
pkg/service/cache/                                      # 통합 시나리오 테스트
pkg/service/holodex/internal/holodexprovider/           # 분할
pkg/config/internal/settings/                           # types 분할
pkg/util/  또는  shared-go/                              # cross-cutting helper 본거지 결정
docs/architecture/file-loc-thresholds.txt               # baseline 재조정
```

## Validation

```bash
./build-all.sh --no-bump
go build ./hololive/hololive-shared/...
go test  ./hololive/hololive-shared/...
./scripts/architecture/ci-boundary-gate.sh
./scripts/architecture/check-contract-map.sh
```

## Stop rules

- alias 정책 변경이 5개 Go runtime 빌드를 깨면 stop. 호환 alias 1단계 PR 로 분리.
- Phase 2.B.2 claim helper 통합 시 3개 호출부의 TTL/재시도 정책 차이가 발견되면 stop 후 동작 보존 단언 테스트 우선.
- `file-loc-thresholds.txt` 재baseline 이 다른 모듈의 OVER 상태를 만들면 stop.

## Out of scope

- Outbox/Dispatcher 의 외부 contract(payload shape, dedup key 정의) 변경.
- 알람 큐의 외부 인터페이스 변경.
- DB 스키마 변경.
- contract 패키지(`pkg/contracts/*`) 변경 — 별도 plan.
