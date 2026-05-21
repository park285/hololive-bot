# 2026-05-22 — 네이밍 sweep 후보 식별 (Phase 2.D 진입 전)

본 문서는 7 Go 모듈 + admin-dashboard 의 약어/케이스 드리프트/패키지-타입 중복 후보를 식별해 Phase 2.D 의 sweep 작업이 막연한 진입이 되지 않도록 sample list 를 제공한다. 본 문서는 식별만, rename 은 2.D 의 일.

조사 기준:
- plan 근거: `docs/agent-workflows/plans/2026-05-21-monorepo-refactor-master.md` 와 8개 sub-plan.
- 약어 빈도: `grep -rnE "\b(cfg|repo|svc|pgCfg|cacheSvc)\b" --include="*.go" hololive/ shared-go/ | wc -l` -> `3944`.
- 패키지/타입 중복: 각 Go file 의 `package` 이름과 `type <PackageName>`/`type <ExportedPackageName>` 패턴을 대조.
- 외부 surface: 다른 package import, facade alias, exported type/function/field 여부를 기준으로 추정.

## 1. 약어 후보

빈도 요약:

| 약어 | count | 모듈 분포 |
|------|-------|-----------|
| `cfg` | 1384 | shared 679, youtube-producer 237, kakao-bot 137, admin-api 104, shared-go 100, llm-sched 87, alarm-worker 40 |
| `repo` | 910 | shared 769, llm-sched 95, admin-api 29, kakao-bot 11, youtube-producer 6 |
| `svc` | 709 | shared 629, llm-sched 46, kakao-bot 14, youtube-producer 12, admin-api 8 |
| `pgCfg` | 4 | kakao-bot 4 |
| `cacheSvc` | 1003 | shared 442, alarm-worker 252, youtube-producer 234, kakao-bot 54, admin-api 11, llm-sched 10 |

| 약어 | 권장 풀네임 | 모듈 분포 | sample | 외부 surface | 비용 |
|------|-------------|-----------|--------|---------------|------|
| `cfg` | `config`; 구체 타입에서는 `postgresConfig`, `serverConfig` | 전 Go 모듈 + shared-go | `hololive/hololive-llm-sched/cmd/llm-scheduler/main.go:48`; `hololive/hololive-llm-sched/internal/app/internal/runtime/llm_search_providers.go:37`; `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/bootstrap_youtube_producer_youtube.go:217`; `shared-go/pkg/httputil/client.go:102` | 대부분 로컬 변수/파라미터. exported signature 의 파라미터 이름은 compile surface 는 아니지만 godoc/readability surface 다. | medium: 빈도 높음, 의미별 rename 필요 |
| `repo` | `repository`; 도메인 필드에서는 `memberRepository`, `statsRepository` | shared, llm-sched, admin-api 중심 | `hololive/hololive-llm-sched/internal/app/internal/runtime/api_internal_majorevent.go:37`; `hololive/hololive-llm-sched/internal/app/internal/runtime/bootstrap_alarm.go:51`; `hololive/hololive-admin-api/internal/server/internal/api/api_deps.go:39`; `hololive/hololive-shared/pkg/service/alarm/queue/publisher.go:67` | 일부 exported constructor/option 파라미터에 노출. struct field 는 대체로 unexported. | medium: repository 타입이 많아 기계적 rename 만으로는 의미 충돌 가능 |
| `svc` | `service`; 단일 필드가 도메인을 품으면 `memberNewsService`, `cache` 등 | shared, llm-sched 중심 | `hololive/hololive-llm-sched/internal/app/internal/runtime/api_internal_membernews.go:43`; `hololive/hololive-llm-sched/internal/app/internal/runtime/api_internal_membernews.go:57`; `hololive/hololive-shared/pkg/service/fallback/secondary_test.go:41`; `hololive/hololive-kakao-bot-go/internal/app/bootstrap/services_providers.go:23` | 대부분 로컬/테스트. receiver 로는 거의 쓰이지 않음. | low-medium: 모듈별 작은 PR 가능 |
| `pgCfg` | `postgresConfig` | kakao-bot | `hololive/hololive-kakao-bot-go/internal/app/internal/botruntime/db_integration_runtime.go:45`; `hololive/hololive-kakao-bot-go/internal/app/internal/botruntime/db_integration_runtime.go:56`; `hololive/hololive-kakao-bot-go/internal/app/internal/botruntime/bootstrap_core_tools.go:72`; `hololive/hololive-kakao-bot-go/internal/app/internal/botruntime/bootstrap_core_tools.go:73` | exported function parameter name only; compile impact 없음. | low |
| `cacheSvc` | `cache`; 필요 시 `cacheClient`/`cacheService` | shared, alarm-worker, youtube-producer 중심 | `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/chzzk_checker.go:55`; `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/common.go:119`; `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatcher.go:94`; `hololive/hololive-kakao-bot-go/internal/app/bootstrap/types.go:94` | `CacheSvc` exported field 가 kakao-bot bootstrap type 에 존재. `cacheSvc` 파라미터는 compile surface 아님. | medium-high: count 1003, `Cache`/`CacheSvc` 필드와 함께 처리 필요 |

샘플 10곳:
- `hololive/hololive-llm-sched/internal/app/internal/runtime/api_internal_majorevent.go:37` `repo *majorevent.Repository`
- `hololive/hololive-llm-sched/internal/app/internal/runtime/api_internal_membernews.go:43` `svc *membernewssvc.Service`
- `hololive/hololive-kakao-bot-go/internal/app/internal/botruntime/db_integration_runtime.go:45` `pgCfg config.PostgresConfig`
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/chzzk_checker.go:55` `cacheSvc cache.Client`
- `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatcher.go:94` `cacheSvc cache.Client`
- `hololive/hololive-kakao-bot-go/internal/app/bootstrap/types.go:94` `CacheSvc cache.Client`
- `hololive/hololive-admin-api/internal/app/build_runtime.go:351` `cacheSvc cache.Client`
- `hololive/hololive-youtube-producer/internal/runtime/ingestionlease/job_run_guard.go:47` `instanceID string`
- `shared-go/pkg/httputil/client.go:102` `NewInternalServiceClient`
- `hololive/hololive-shared/pkg/service/fallback/secondary_test.go:41` string value `"svc"`; identifier rename 대상 아님.

## 2. 패키지-타입 중복 후보

| 후보 | 근거 sample | 외부 surface | 비용 |
|------|-------------|---------------|------|
| `matcher.MemberMatcher` | `hololive/hololive-kakao-bot-go/internal/service/matcher/matcher.go:72`; `internal/app/bootstrap/providers_alarm_consumers.go:73`; `internal/command/internal/handlers/command.go:72` | exported type/function 이고 kakao-bot 내부 여러 package 가 import. Go `internal/` 경계로 모듈 외 import 는 없음. | high inside module: 호출부와 테스트 다수 |
| `api.APIHandler` | `hololive/hololive-admin-api/internal/server/internal/api/api.go:55`; `internal/server/server.go:5`; `internal/app/build_runtime.go:220` | `internal/server/server.go` facade alias 로 `server.APIHandler` 가 admin-api 내부 surface. | high: embed wrapper 11개와 tests 대량 영향 |
| `dispatchoutbox` | `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/model.go:1`; `hololive/hololive-alarm-worker/internal/app/internal/workerapp/build_egress.go:13`; `hololive/hololive-shared/pkg/service/alarm/queue/publisher.go:32` | public import path. alarm-worker 와 hololive-shared queue 가 직접 import. | high: import path rename 이 디렉터리 이동/compat wrapper 를 동반 |
| `majorevent/scheduler.Scheduler` | `hololive/hololive-llm-sched/internal/service/majorevent/scheduler/scheduler.go:75`; `internal/app/internal/runtime/bootstrap_llm.go:47` | llm-sched 내부 package import. | medium-high: alias/import alias 정리 필요 |
| `membernews/scheduler.Scheduler` | `hololive/hololive-llm-sched/internal/service/membernews/scheduler/scheduler.go:52`; `internal/service/membernews/facade_scheduler.go:35`; `internal/app/internal/runtime/bootstrap_llm.go:92` | facade alias `membernews.Scheduler` 도 존재. | high: facade, tests, runtime wiring 영향 |

자동 대조로 발견한 추가 후보:

| 패턴 | sample | 판단 |
|------|--------|------|
| `scheduler.Scheduler` x2 | `hololive/hololive-llm-sched/internal/service/membernews/scheduler/scheduler.go:52`; `hololive/hololive-llm-sched/internal/service/majorevent/scheduler/scheduler.go:75` | 실제 Phase 2.D 후보 |
| `config.Config` alias | `hololive/hololive-shared/pkg/config/config.go:11` | facade alias 이므로 2.A.2 alias 정책과 같이 다룰 것 |
| `poller.Poller` alias | `hololive/hololive-shared/pkg/service/youtube/poller/poller.go:19` | public facade, sweep 제외 또는 별도 alias PR |
| `bot.Bot` / `command.Command` alias | `hololive/hololive-kakao-bot-go/internal/bot/bot.go:7`; `internal/command/command.go:7` | internal facade alias. rename 보다 doc 명시가 우선 |

## 3. 케이스 드리프트 후보

| 후보 | sample | 외부 surface | 비용 |
|------|--------|---------------|------|
| `ActiveActiveEnabled`/`ActiveActiveInstance` vs `activeActive`/`instanceID` | `hololive/hololive-youtube-producer/internal/runtime/readiness/ingestion_runtime_readiness.go:36`; `:44`; `:175` | exported `readiness.Features` field 와 log key 가 함께 있음. | medium: identifier 만 바꾸고 log key 는 유지해야 함 |
| `LLMSchedulerRuntime` vs `llmSchedulerFormatter` | `hololive/hololive-llm-sched/internal/app/internal/runtime/bootstrap_llm_scheduler.go:49`; `formatter_llm_scheduler_test.go:135`; `formatter_llm_scheduler.go` | `LLMSchedulerRuntime` 는 alias/export facade 로 노출. formatter 는 unexported. | medium: unexported 쪽만 우선 |
| `internal/llm.Client` vs `summarizer.LLMClient` | `hololive/hololive-llm-sched/internal/service/majorevent/summarizer/llm_client.go:25`; `internal/service/membernews/summarizer/summarizer.go:55`; `internal/app/internal/runtime/llm_providers_local.go:32` | interface 중복과 provider return type 이 연결됨. | medium-high: mock/test 영향 |
| `Cache` / `cacheSvc` / `CacheSvc` | `hololive/hololive-kakao-bot-go/internal/app/bootstrap/types.go:94`; `internal/app/bootstrap/services_modules.go:79`; `internal/app/wiring/dependency_views.go:34` | `CacheSvc` exported field 가 있음. | medium-high |
| admin-dashboard `State(state)` vs `State(app_state)` | `docs/agent-workflows/plans/2026-05-21-monorepo-refactor-admin-dashboard.md` | Rust handler parameter naming only. | low, backend-only |

## 4. 인터페이스 vs 구조체 케이싱

| 후보 | sample | 판단 |
|------|--------|------|
| `deliveryRepository` interface vs `DeliveryRepository` struct | `hololive/hololive-shared/pkg/service/delivery/dispatcher.go:45`; `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/delivery_repository.go:36`; `outbox/outbox.go:35` | 서로 다른 package 의 동명 domain 이라 rename 시 오히려 더 명확하게 `deliveryStore`/`outboxDeliveryRepository` 로 분리 가능. exported struct 는 alias 로도 노출되어 high risk. |
| lowercase local interfaces | `hololive/hololive-alarm-worker/internal/app/internal/workerapp/alarm_dispatch_runner.go:15`; `hololive/hololive-llm-sched/internal/service/membernews/repository.go:44`; `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/repository_batch.go:37` | package-local dependency seam 은 lowercase 유지가 자연스럽다. 일괄 export 금지. |
| `Repository` generic struct names | `hololive/hololive-shared/pkg/service/member/repository.go:56`; `hololive/hololive-llm-sched/internal/service/membernews/repository.go:61`; `hololive/hololive-shared/pkg/service/alarm/repository.go:36` | package context 가 충분한 곳은 유지. package name 이 `repository` 인 경우만 별도 검토. |

## 5. 리시버 약어

대상 리시버 count: `as`/`svc`/`s`/`r` 합산 `1280`.

| receiver | 분포 | 판단 |
|----------|------|------|
| `as` | hololive-shared 107 | alarm service 계열로 보이며 모듈-local 컨벤션화 필요. |
| `svc` | 0 | receiver 로는 사실상 쓰이지 않음. 로컬 변수 약어 후보로만 처리. |
| `s` | shared 299, kakao-bot 141, llm-sched 103, alarm-worker 56, youtube-producer 30, admin-api 26 | service/state/scheduler 에 광범위. 현상 유지 기본. |
| `r` | shared 351, llm-sched 55, alarm-worker 46, youtube-producer 45, kakao-bot 12, admin-api 5, shared-go 4 | repository/result/registration 혼용. repository method 는 `r` 유지, result value receiver 는 타입별 검토. |

권장: Phase 2.D 에서 receiver 만 단독 sweep 하지 않는다. 해당 타입 rename 또는 파일 분해와 같은 변경에 붙어 있을 때만 정리한다.

## 6. Suffix 컨벤션

`Service` suffix 참조는 `hololive-shared` 932, `kakao-bot` 261, `youtube-producer` 209, `llm-sched` 97, `alarm-worker` 72, `admin-api` 69, `shared-go` 0 이다.

| 패턴 | sample | 판단 |
|------|--------|------|
| package-local `type Service` | `hololive/hololive-shared/pkg/service/acl/service.go:132`; `hololive/hololive-llm-sched/internal/service/membernews/service.go:37`; `hololive/hololive-kakao-bot-go/internal/service/streamfeed/service.go:31` | package context 로 충분하면 유지. |
| domain-prefixed service | `hololive/hololive-shared/pkg/service/template/admin_service.go:45`; `pkg/service/member/profile.go:39`; `pkg/service/database/postgres.go:36` | 외부 import 또는 shared package 에서 discoverability 때문에 허용. |
| abbreviation field `cacheSvc` | `hololive/hololive-shared/pkg/service/holodex/internal/holodexprovider/service.go:83`; `hololive/hololive-kakao-bot-go/internal/service/matcher/matcher.go:93` | `cache.Client` 타입은 service 가 아니라 client 이므로 `cache`/`cacheClient` 로 정리 후보. |

정책 제안: public/shared package 는 `DomainService`, package-local concrete 는 `Service`, dependency field 는 타입 역할에 맞춰 `cache`, `repository`, `sender` 처럼 suffix 를 줄인다.

## 7. PR 분리 그룹 제안

### Group A — 약어 (low risk, 내부)

대상:
- `pgCfg` -> `postgresConfig`: kakao-bot 단독.
- `svc` -> `service`: llm-sched route handler/테스트부터.
- `repo` -> `repository`: 모듈별 package-local parameter/field 만.
- `cacheSvc` -> `cache`/`cacheClient`: `CacheSvc` exported field 는 제외하고 내부부터.

Stop rule:
- exported field/type/function 이름 변경이 필요하면 Group B/C 또는 별도 PR 로 이동.
- 한 PR 에서 변경 모듈 1개 또는 shared package 1개를 넘기지 않는다.
- string literal, metric label, log key 는 식별자 rename 과 분리한다.

Risk: low-medium. 빈도는 많지만 compile surface 는 대부분 로컬이다.

### Group B — 케이스 드리프트 (medium risk)

대상:
- `LLM` casing: `llmSchedulerFormatter` 같은 unexported identifier 부터.
- `ActiveActive` casing: exported `Features` field 는 유지하고 internal state name 만 정렬.
- `Cache`/`CacheSvc` field drift: kakao-bot bootstrap/wiring 내부에서 시작.
- admin-dashboard Rust parameter/doc drift: backend phase 에 묶기.

Stop rule:
- JSON field, env var, log key, telemetry event name, metric label 변경이 보이면 stop.
- exported facade alias 와 unexported implementation rename 을 같은 commit 에 섞지 않는다.

Risk: medium. 표기 정리처럼 보여도 operational strings 와 엮일 수 있다.

### Group C — 패키지-타입 중복 (high risk)

대상:
- `api.APIHandler` / `server.APIHandler` facade.
- `matcher.MemberMatcher`.
- `dispatchoutbox` package path.
- llm-sched `Scheduler` 동명.
- hololive-shared facade aliases (`Config`, `Poller`, `Bot`, `Command`) 는 직접 rename 대신 alias 정책 문서와 연결.

Stop rule:
- import path rename 이 필요하면 compat alias package 또는 별도 plan 을 먼저 작성한다.
- 5개 Go runtime build 영향이 보이면 한 식별자 단위 PR 로 축소한다.
- public facade alias 삭제/축소는 `hololive-shared-outbox-alias-decision-2026-05-22.md` 의 alias 정책과 충돌하지 않아야 한다.

Risk: high. 호출부 폭주와 facade compatibility 영향이 가장 크다.

## 8. 외부 surface 영향 평가

cross-module 또는 facade surface sample:

| 식별자 | sample | 영향 |
|--------|--------|------|
| `dispatchoutbox.Writer` | `hololive/hololive-alarm-worker/internal/service/alarm/scheduler/runtime_scheduler.go:96`; `hololive/hololive-shared/pkg/service/alarm/queue/publisher.go:59` | shared public package 를 alarm-worker 가 import. package path rename 은 high risk. |
| `dispatchoutbox.NewPgxRepository` | `hololive/hololive-alarm-worker/internal/app/internal/workerapp/build_egress.go:97`; `build_runtime.go:219` | runtime wiring compile break 가능. |
| `server.APIHandler` facade | `hololive/hololive-admin-api/internal/server/server.go:5`; `internal/app/build_runtime.go:220`; `internal/app/api_router_test.go:39` | `api.APIHandler` rename 은 facade alias 와 route registration 전체 영향. |
| `matcher.MemberMatcher` | `hololive/hololive-kakao-bot-go/internal/bot/internal/orchestration/deps.go:62`; `internal/command/internal/handlers/command.go:72`; `internal/app/bootstrap/types.go:107` | module internal 이지만 command/bootstrap/orchestration 전체 영향. |
| `membernews.Scheduler` alias | `hololive/hololive-llm-sched/internal/service/membernews/facade_scheduler.go:35`; `internal/app/internal/runtime/bootstrap_llm.go:92` | facade alias + concrete package rename 동시 필요. |
| `LLMSchedulerRuntime` | `hololive/hololive-llm-sched/internal/app/app.go:6`; `internal/app/internal/runtime/bootstrap_llm_scheduler.go:49` | public-ish app facade. casing 유지 권장. |
| `CacheSvc` exported field | `hololive/hololive-kakao-bot-go/internal/app/bootstrap/types.go:94`; `internal/app/bootstrap/services_modules.go:79` | field rename 은 struct literal 호출부 영향. Group B 별도. |

## 9. 결론

2.D 진입 우선순위는 Group A -> Group B -> Group C 다.

권장 PR 크기:
- Group A: 모듈별 1 PR, 50-250 LOC diff 예상. `pgCfg` 는 독립 1 commit 가능.
- Group B: 주제별 1 PR. `LLM` casing 과 `ActiveActive` casing 은 서로 다른 runtime 이므로 분리 가능.
- Group C: 한 식별자 1 PR. `dispatchoutbox` 는 rename 보다 public package 유지 + doc/alias 정책 보강을 먼저 검토한다.

권장 stop rule:
- compile surface 가 exported identifier, facade alias, import path, metric/log key, env/config key 로 확장되는 순간 해당 PR 을 중단하고 별도 mini-plan 으로 승격한다.
- 단순 identifier rename 이 300 LOC 를 넘거나 3개 이상 모듈을 동시에 건드리면 split 한다.
- rename 비용 추정이 어려운 후보는 별도 brainstorming 대상으로 넘긴다. 현재 별도 brainstorming 후보는 `dispatchoutbox` package path 와 `api.APIHandler` facade rename 이다.
