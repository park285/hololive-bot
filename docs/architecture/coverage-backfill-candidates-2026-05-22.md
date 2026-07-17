# 2026-05-22 — 테스트 커버리지 백필 후보 식별 (Phase 2.E 진입 전)

> Historical document (퇴역한 5-runtime 모듈 기준 기록). Do not use as the current source of truth. See `docs/current/PROJECT_MAP.md`.

본 문서는 master plan 의 Phase 2.E 진입 전 핵심 미커버 경로를 식별해 후속 테스트 작성 task 들이 막연한 진입이 되지 않도록 sample list 와 우선순위를 제공한다. 본 문서는 식별만, test 작성은 2.E 의 일.

조사 기준:
- module ratio 는 각 모듈에서 `find ... -name "*.go" -not -name "*_test.go" | wc -l` 와 `find ... -name "*_test.go" | wc -l` 로 측정했다.
- sub-plan 근거는 `docs/agent-workflows/plans/2026-05-21-monorepo-refactor-*.md` 의 "테스트 보강" 섹션이다.
- 미커버 파일 수는 후보 디렉터리에서 같은 basename 의 `_test.go` 가 없는 prod file 수를 더한 proxy 이다. package-level test 가 이미 간접 커버하는 파일도 포함될 수 있으므로 coverage percentage 로 해석하지 않는다.

## 1. 모듈별 prod/test ratio (parent 사전 조사 보완)

| 모듈 | prod | test | ratio |
|------|------|------|-------|
| shared-go | 34 | 24 | 70.6% |
| hololive-shared | 444 | 215 | 48.4% |
| hololive-kakao-bot-go | 125 | 62 | 49.6% |
| hololive-admin-api | 35 | 22 | 62.9% |
| hololive-alarm-worker | 44 | 19 | 43.2% |
| hololive-llm-sched | 68 | 44 | 64.7% |
| hololive-youtube-producer | 101 | 55 | 54.5% |

관찰:
- `shared-go` 는 최근 Task 02-07 에서 logging/jsonutil/httputil/telemetry backfill 이 진행되어 ratio 가 가장 높다. Phase 2.E 의 1차 후보에서는 제외하고 residual utility case 만 별도 low priority 로 둔다.
- `hololive-alarm-worker`, `hololive-shared`, `hololive-kakao-bot-go` 는 ratio 와 runtime criticality 를 함께 보면 2.E 초반 후보로 적합하다.
- `hololive-admin-api`, `hololive-llm-sched`, `hololive-youtube-producer` 는 ratio 만으로는 덜 급하지만 handler/runtime/polltarget/report 경로가 user-visible failure 로 이어질 수 있어 모듈별 PR 로 분리하는 편이 안전하다.

## 2. 핵심 미커버 경로 (sub-plan 인용)

### hololive-shared (sub-plan 2.C.1)

- `pkg/service/youtube/poller/internal/polling` 38 prod / 12 test — `channel_stats_poller.go`, `community_shorts_detection.go`, `metrics.go`, `published_at_resolver_{candidate,controls}.go` 테스트.
- `pkg/service/youtube/tracking/internal/observation` 20 prod / 6 test — `alarm_state_repository*.go`, `observation_compare_{index,mismatch,sort}.go`, `community_alarm_sent_history.go` 테스트.
- `pkg/service/alarm/queue` 7 prod / 2 test — `consumer_delayed_retry.go`, `consumer_payload.go`, `consumer_primitives.go`, `consumer_retry.go`, `metrics.go` 테스트.
- `pkg/service/cache` sub-plan 12 prod / 6 test, 현재 측정 10 prod / 5 test — valkey/Redis 통합 시나리오 테스트.
- `pkg/service/notification/internal/alarmservice` — `alarm_service.GetRoomAlarms*` 유닛 테스트.

우선순위 판단: high. poller, observation, queue, notification alarm path 는 알림 지연/누락/중복과 직접 연결된다. cache 는 recovery/consistency 보강으로 medium 에 둔다.

### hololive-kakao-bot-go (sub-plan 2.C.2)

- `internal/app/bootstrap/` 15 prod / 1 test — `InitBotInfrastructure`, 알람/YouTube 스택 와이어링, 멤버 캐시 부트스트랩 테스트.
- `internal/app/http/` 1 prod / 0 test — webhook 라우터 테스트.
- `internal/command/internal/handlers/` 미커버 핸들러(schedule, subscriber, stats, help, subscriber_graph, member_info group, news subscription).
- `internal/bot/internal/orchestration/` 미커버 8건.

우선순위 판단: high. bootstrap 과 webhook/orchestration 은 서비스 시작 및 사용자 요청 처리의 첫 관문이다. command handler 는 사용자 직접 영향이지만 도메인별로 나누어 medium-high 로 처리한다.

### hololive-admin-api (sub-plan 2.C.3)

- `internal/app/` 7 prod / 3 test — `runtime_admin_api.go`, `_shutdown.go`, `_runner.go`, `settings_applier.go`, `http/{registration,middleware}.go` 테스트.
- `internal/app/runtime/` 2 prod / 0 test — `lifecycle.go`, `http_server.go`.
- `internal/server/internal/api/` 미커버 핸들러: `api_alarm`, `api_majorevent`, `api_deps`, `api_domains`, `oauth_proxy`. catch-all `api_low_coverage_test.go` 의 분량을 도메인별 테스트로 분리.

우선순위 판단: medium. 운영/관리 API 라우팅과 shutdown 신뢰성은 중요하지만 poller/queue 대비 즉시 사용자 알림 경로와는 한 단계 떨어져 있다.

### hololive-alarm-worker (sub-plan 2.C.4)

- `internal/app/internal/workerapp` 17 prod / 6 test — dispatch_runner_loop/idle/karing_community, build_egress 테스트.
- `internal/service/alarm/scheduler` 5 prod / 2 test — events/cache_recovery/twitch 테스트.
- `internal/service/alarm/checker/internal/checking` 15 prod / 8 test — youtube_checker_*, chzzk_checker_lookup_job, notifier_publish 테스트.

우선순위 판단: high. checker, scheduler, dispatch runner 는 알림 발송의 live path 이며 queue/poller 보강과 같은 2.E 초반 batch 에 넣는 것이 좋다.

### hololive-llm-sched (sub-plan 2.C.5)

- `cmd/llm-scheduler` 1 — 진입점 최소 테스트.
- `internal/model` 1 — search 모델 라운드트립.
- `internal/service/consensus` 1 — types contract 단언.
- `internal/llm` 5 prod / 1 test — `client.go`, `openai_response_diagnostics.go`, `openai_provider_errors.go`, `openai_fallback.go` 테스트.
- `internal/service/majorevent` 4 prod / 1 test — repository + errors.

우선순위 판단: medium-low. LLM fallback/error classification 은 consistency 에 중요하나 alarm live path 보강 뒤에 둔다. entrypoint/model/consensus 는 low-risk contract tests 로 묶을 수 있다.

### hololive-youtube-producer (sub-plan 2.C.6)

- `internal/ops/communityshorts/internal/reports` 34 prod / 14 test — builder/renderer 핵심 경로 fixture 기반 테스트.
- `internal/runtime/polltarget` 11 prod / 3 test — `youtube_poll_target_refresh.go` 테스트.
- `cmd/ops/internal/communityshortscli` 10 prod / 4 test — latency_cause/latency_period 파서 + 플래그 테스트.
- `internal/ops/communityshorts` 1 prod / 0 test — 진입 dispatcher.

우선순위 판단: medium. `polltarget` refresh 는 poller 대상 누락/과잉과 연결되므로 medium-high 로 올리고, reports/CLI 는 low 로 둔다.

## 3. 우선순위 분류

### High priority — business critical path

- `hololive-shared/pkg/service/youtube/poller/internal/polling`
- `hololive-shared/pkg/service/youtube/tracking/internal/observation`
- `hololive-shared/pkg/service/alarm/queue`
- `hololive-shared/pkg/service/notification/internal/alarmservice` 의 `GetRoomAlarms*`
- `hololive-kakao-bot-go/internal/app/bootstrap`
- `hololive-kakao-bot-go/internal/app/http`
- `hololive-kakao-bot-go/internal/bot/internal/orchestration`
- `hololive-alarm-worker/internal/app/internal/workerapp`
- `hololive-alarm-worker/internal/service/alarm/checker/internal/checking`

이 그룹은 poller / observation / queue / dispatcher / bootstrap 중심이다. 알림 생성, 상태 관측, 사용자 요청 처리, 서비스 시작 신뢰성을 직접 보호한다.

### Medium priority — recovery / consistency

- `hololive-shared/pkg/service/cache`
- `hololive-admin-api/internal/app`
- `hololive-admin-api/internal/app/runtime`
- `hololive-admin-api/internal/server/internal/api`
- `hololive-alarm-worker/internal/service/alarm/scheduler`
- `hololive-llm-sched/internal/llm`
- `hololive-llm-sched/internal/service/majorevent`
- `hololive-youtube-producer/internal/runtime/polltarget`

이 그룹은 recovery loop, claim helper, cache helpers, runtime lifecycle, handler consistency 를 보호한다. live path 의 직접 smoke 이후에 모듈별로 붙이는 편이 좋다.

### Low priority — utility / CLI

- `shared-go` residual utility cases after Task 02-07
- `hololive-llm-sched/cmd/llm-scheduler`
- `hololive-llm-sched/internal/model`
- `hololive-llm-sched/internal/service/consensus`
- `hololive-youtube-producer/internal/ops/communityshorts/internal/reports`
- `hololive-youtube-producer/cmd/ops/internal/communityshortscli`
- `hololive-youtube-producer/internal/ops/communityshorts`

이 그룹은 CLI parser, reports renderer, formatter, type contract 성격이다. fixture/golden tests 로 회귀 차단 효과는 있지만 critical runtime path 뒤에 배치한다.

## 4. 각 후보의 test scope 추정

추정 방식:
- 단위 테스트는 branch/function 단위의 대표 valid/invalid case 를 기준으로 잡았다.
- 통합 테스트는 package boundary 또는 in-memory/fake dependency 로 한 scenario 를 검증하는 단위로 잡았다.
- 회귀 차단 단언은 알림 누락/중복, route mismatch, lifecycle error wrapping, fallback classification 처럼 장애로 이어질 수 있는 핵심 조건만 센다.

| 모듈 | 후보 카테고리 | 직접 `_test.go` 없는 후보 파일 수 | 단위 테스트 추정 | 통합/시나리오 추정 | 회귀 차단 단언 추정 |
|------|---------------|----------------------------------|------------------|--------------------|---------------------|
| hololive-shared | 5 | 70 | 35-60 | 8-14 | 15-25 |
| hololive-kakao-bot-go | 4 | 47 | 20-35 | 4-8 | 8-14 |
| hololive-admin-api | 3 | 20 | 15-25 | 4-7 | 6-12 |
| hololive-alarm-worker | 3 | 26 | 18-30 | 5-8 | 8-14 |
| hololive-llm-sched | 5 | 10 | 8-16 | 2-4 | 4-8 |
| hololive-youtube-producer | 4 | 38 | 20-35 | 4-8 | 6-12 |

총 추정:
- 식별된 핵심 미커버 영역: 24 카테고리.
- 직접 `_test.go` 가 없는 후보 파일: 약 211개.
- 예상 추가 test functions: 모듈별 scope 를 모두 합치면 대략 110-200개. 실제 2.E 에서는 critical path 를 우선 잘라 모듈당 10-30개 수준으로 제한하는 것이 review 가능하다.

## 5. Phase 2.E 진입 시 sub-task 분해 제안

2.E 는 모듈당 1 PR 권장. 다만 `hololive-shared` 는 후보가 넓어 첫 PR 을 queue/poller 중심으로 제한하고, observation/cache/alarmservice 는 같은 PR 의 후속 commit 또는 2번째 PR 로 나누는 것이 안전하다.

권장 sub-task:

1. `hololive-shared` high path backfill: poller, observation, queue, `GetRoomAlarms*` 중심으로 20-30 새 test functions.
2. `hololive-alarm-worker` alarm execution backfill: checker, scheduler, workerapp dispatch runner 중심으로 15-25 새 test functions.
3. `hololive-kakao-bot-go` bootstrap/webhook/orchestration backfill: startup wiring, webhook router, orchestration failure path 중심으로 15-25 새 test functions.
4. `hololive-youtube-producer` polltarget + communityshorts entry backfill: poll target refresh 와 dispatcher 최소 smoke 중심으로 10-20 새 test functions.
5. `hololive-admin-api` app/runtime/API handler backfill: lifecycle/http server, 도메인 handler 분리 tests 중심으로 10-20 새 test functions.
6. `hololive-llm-sched` LLM/model/majorevent backfill: fallback/error/model contract 중심으로 10-20 새 test functions.
7. `shared-go` residual utility follow-up: Task 02-07 이후 남은 utility edge case 가 발견될 때만 5-10 새 test functions.

권장 순서:

1. high priority 모듈: `hololive-shared` queue/poller, `hololive-alarm-worker` checking/workerapp.
2. service entry reliability: `hololive-kakao-bot-go` bootstrap/webhook/orchestration.
3. consistency/runtime: `hololive-youtube-producer` polltarget, `hololive-admin-api` app/runtime/API.
4. utility/contract: `hololive-llm-sched`, `shared-go` residual.

## 6. 결론

- 식별된 핵심 미커버 영역은 총 24 카테고리이며, 직접 `_test.go` 가 없는 후보 파일은 proxy 기준 약 211개다.
- 우선순위는 high -> medium -> low 로 진행한다. high 는 poller / observation / queue / dispatcher / bootstrap 을 먼저 보호한다.
- 권장 다음 step 은 2.C 완료 후 모듈당 1 PR 로 2.E 에 진입하는 것이다. 첫 batch 는 `hololive-shared`, `hololive-alarm-worker`, `hololive-kakao-bot-go` 를 권장한다.
- 본 문서는 후보 식별만 수행했다. test 작성, helper 추출, naming sweep, lifecycle helper 구현은 각 Phase 2 task 의 범위로 남긴다.
