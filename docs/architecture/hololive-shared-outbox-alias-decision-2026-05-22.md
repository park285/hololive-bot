# 2026-05-22 — hololive-shared outbox alias 정책 결정 (Phase 2.A.2)

> Historical document (퇴역한 5-runtime 모듈 기준 기록). Do not use as the current source of truth. See `docs/current/PROJECT_MAP.md`.

본 문서는 `docs/agent-workflows/plans/2026-05-21-monorepo-refactor-master.md` 의 Phase 2.A.2 결정 기록이다. 대상은 `hololive/hololive-shared/pkg/service/youtube/outbox/outbox.go` 의 public re-export surface 이며, `internal/delivery` 구현 타입과 상수, 생성 함수가 `outbox` 패키지에서 외부 API 로 다시 노출되는 현재 정책을 유지할지 축소할지 결정한다.

## 배경

Phase 2.A.2 의 쟁점은 `outbox.go` 의 bridge layer 를 collapse 할 수 있는지였다. `internal/delivery` 로 실제 구현이 이동한 뒤에도 외부 runtime 은 `github.com/kapu/hololive-shared/pkg/service/youtube/outbox` 를 import 한다. 따라서 alias 를 줄이면 호출부가 `internal/delivery` 로 접근할 수 없고, 5개 Go runtime 빌드에 연쇄 영향이 생긴다.

검증 기준 파일은 `hololive/hololive-shared/pkg/service/youtube/outbox/outbox.go` 이다. 현재 public re-export 는 type alias 20개, const alias 25개, var re-export 9개, 총 54개다. `outbox.go` 안에는 alias 가 아닌 신규 `type` 선언은 없다.

## 호출부 조사 결과

`rg -l '"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"' hololive shared-go -g '*.go' | grep -v 'outbox/doc.go'` 기준 import 분포는 다음과 같다 (outbox 패키지 자체의 `doc.go` 안 사용 예시 import 는 제외).

| 모듈 | 파일 수 | 용도 |
|---|---:|---|
| `hololive-admin-api` | 3 | admin stats API 의 community/shorts delivery telemetry 조회와 테스트 |
| `hololive-alarm-worker` | 3 | YouTube outbox dispatcher 구성, alarm dispatch payload rendering |
| `hololive-shared` | 3 | poller after-commit latency classification, outbox integration tests (`outbox/doc.go` 의 godoc 예시 import 는 분포 계산에서 제외) |
| `hololive-youtube-producer` | 25 | community/shorts ops report, latency cause, route, send-state report |
| `hololive-llm-sched` | 0 | shared outbox import 없음 |

주요 호출부 sample 은 다음과 같다.

- `hololive/hololive-admin-api/internal/server/internal/api/api_youtube_ops.go:25` — `ListPostSendCountsSince` 가 `[]outbox.PostSendCount` 를 외부 API repository 계약으로 사용.
- `hololive/hololive-admin-api/internal/server/internal/api/api_youtube_ops.go:114` — `outbox.BuildChannelPostDeliverySummaries` 로 admin response 집계.
- `hololive/hololive-admin-api/internal/server/internal/api/api_youtube_ops.go:121` — `outbox.BuildPostLatencyPeriodSummaries` 로 latency period 집계.
- `hololive/hololive-admin-api/internal/server/internal/api/api_youtube_ops.go:192` — `[]outbox.PostLatencyPeriod` 로 admin latency window 생성.
- `hololive/hololive-admin-api/internal/server/internal/api/api_youtube_ops.go:201` — `outbox.PostLatencyPeriodSummary` 를 response summary 로 사용.
- `hololive/hololive-admin-api/internal/server/internal/api/api_youtube_ops.go:210` — `[]outbox.ChannelPostDeliverySummary` 를 channel response builder 입력으로 사용.
- `hololive/hololive-admin-api/internal/server/internal/api/api_youtube_ops.go:347` — `*outbox.DeliveryTelemetryRepository` 가 admin repository interface 를 만족함을 컴파일 타임에 고정.
- `hololive/hololive-admin-api/internal/app/build_runtime.go:265` — `outbox.NewDeliveryTelemetryRepository` 로 admin runtime repository 를 구성.
- `hololive/hololive-alarm-worker/internal/app/internal/workerapp/build_egress.go:147` — `youtubeoutbox.NewDispatcher` 로 alarm-worker runtime dispatcher 를 구성.
- `hololive/hololive-alarm-worker/internal/app/internal/workerapp/build_egress.go:153` — `youtubeoutbox.DefaultConfig` 로 dispatcher config 를 구성.
- `hololive/hololive-alarm-worker/internal/app/internal/workerapp/alarm_dispatch_runner.go:197` — `outbox.FormatYouTubeOutboxPayload` 로 alarm dispatch payload 를 렌더링.
- `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/repository_batch.go:153` — `[]outbox.PostTrackingIdentity` 로 after-commit classification 대상을 구성.
- `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/repository_batch.go:167` — `outbox.NewDeliveryTelemetryRepository` 로 classification persistence 를 호출.
- `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatcher_integration_test.go:112` — `outbox.Config` 로 public facade 기준 integration test 를 유지.
- `hololive/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/session.go:21` — `*outbox.DeliveryTelemetryRepository` 를 ops report session 에 보관.
- `hololive/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/session.go:48` — `outbox.NewDeliveryTelemetryRepository` 로 report session repository 를 구성.
- `hololive/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/community_shorts_latency_cause_build.go:21` — `[]outbox.PostSendCount` 를 latency cause report 입력으로 사용.
- `hololive/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/community_shorts_latency_cause_build.go:22` — `[]outbox.PostDeliveryTimeline` 을 latency cause report 입력으로 사용.
- `hololive/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/community_shorts_latency_cause_build.go:75` — `outbox.BuildPostLatencyPeriodSummaries` 로 producer-side report 집계.

`hololive/hololive-llm-sched/internal/service/membernews/scheduler/digest_helper.go:85` 의 `outbox.Enqueue` 는 shared outbox package 호출이 아니다. 해당 파일은 `github.com/kapu/hololive-shared/pkg/service/youtube/outbox` 를 import 하지 않으며, `outbox` 는 `outboxEnqueuer` 파라미터 이름이다. 이 호출은 본 alias 정책의 호출부 sample 에서 제외한다.

## 결정

결정: 유지.

`outbox` 패키지는 외부 runtime 이 사용하는 public facade 다. 현재 repo 내부 직접 호출이 모든 re-export symbol 을 균등하게 사용하지는 않지만, exported re-export 는 `hololive-shared` 의 외부 API surface 로 공개되어 있다. `internal/delivery` 는 Go `internal` 규칙 때문에 외부 모듈이 직접 import 할 수 없으므로 alias collapse 는 단순한 내부 정리가 아니라 public import contract 변경이다.

따라서 Phase 2.A.2 에서는 alias collapse 또는 개별 alias 삭제를 진행하지 않는다. 호출부 폭주와 5개 Go runtime 빌드 연쇄 실패 위험을 피하기 위해, 현 public facade 를 문서화하고 후속 변경은 alias 1개 단위의 작은 plan/PR 로만 검토한다.

## 향후 정책

- 신규 alias 또는 re-export 는 `internal/delivery` 의 type, const, func, error 가 외부 runtime 의 public surface 로 의도된 경우에만 추가한다.
- 외부 surface 가 아닌 internal helper 는 `outbox` alias 를 추가하지 않는다.
- 외부 모듈은 `internal/delivery` 를 직접 import 하지 않고 `github.com/kapu/hololive-shared/pkg/service/youtube/outbox` 를 import 한다.
- 향후 `outbox` 패키지 외부 API 가 안정화되면 `internal/delivery` 를 `outbox` 로 흡수할 수 있는지 별도 plan 에서 검토한다.
- 개별 re-export 제거는 해당 symbol 의 repo 내부/외부 호출부, runtime 빌드 영향, migration path 를 별도로 확인한 뒤 작은 단위로 진행한다.

## Ban-list

없음.

이번 phase 에서 제거 대상으로 금지할 alias 는 지정하지 않는다. 현재 결정은 public facade 유지이며, 미사용으로 보이는 re-export 도 별도 영향 분석 없이 삭제하지 않는다.

## 마이그레이션 list

없음.

현재 사용 정책을 유지한다. 호출부는 계속 `outbox` 패키지를 import 하며, `internal/delivery` 직접 import 로 이동하지 않는다.
