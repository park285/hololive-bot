# YouTube Three-Provider Convergence — Task 3 Implementation Handoff

> **문서 상태 — 2026-08-14 완료:** Task 3 three-provider collector adapters는 로컬 구현·targeted validation을 완료했습니다. 아래 원문은 실행 당시의 write boundary와 판단 근거를 보존하는 historical implementation record입니다. 새 세션에서 Task 3를 다시 실행하지 말고 canonical contract와 status 문서의 현재 상태를 우선하십시오.
>
> **후속:** `viewer_sample` channel-ID plant, `MaxPublishBatchSize=100`, collector `HOLODEX_API_KEY` 미전달은 이후 커밋에서 닫혔습니다. Task 4는 `2026-08-14-youtube-convergence-task4-implementation-handoff.md`를 따르십시오.

## 완료 업데이트

Outcome: `complete`

status = `Task 3 로컬 검증 완료`

세 provider가 collector-owned fixture-backed adapter와 typed registry를 통해 Task 2 monotonic lease와 atomic `PublishBatch`를 사용합니다. Community는 YouTube.js `community_collect` runner로 흡수되었고 `communitycollector` direct publish path는 없습니다.

### 실제 enabled provider/kind와 fixture

- `youtubejs` / `community_collect` → `community_page`
  - `hololive/hololive-youtube-collector/internal/runtime/youtubejscollector/testdata/community.json`
  - helper: `youtubejs/src/fetch-community.mjs`, `pagination.mjs`
- `youtubejs` / `youtubejs_content` → `video_list`, `shorts_list`
  - `testdata/videos.json`, `testdata/shorts.json`
- `youtubejs` / `youtubejs_channel_live` → `live_snapshot`
- `youtubejs` / `youtubejs_channel_metadata` → `channel_stats`, `channel_profile`, `channel_photo`
  - `testdata/channel.json`
- `youtubejs` / `youtubejs_viewer` → `viewer_sample`
  - `testdata/viewer_hidden.json`
- `holodex` / `holodex_live` → `live_snapshot`, `viewer_sample`
- `holodex` / `holodex_schedule` → `schedule_snapshot`
- `holodex` / `holodex_metadata` → `channel_stats`, `channel_photo`
  - `holodexcollector/testdata/live.json`, `empty.json`
  - `channel_profile`은 live API에 handle/description/country/joined_date가 없어 미발행
- `hololive_official` / `official_schedule` → `schedule_snapshot`
  - `officialcollector/testdata/success.json`, `empty.json`

### typed registry/config/publish/observability

- registry key `(provider, collection_job_kind)`; duplicate/unknown emission/`InitialJobContracts` mismatch는 startup fail closed
- 공유 bounded queue + total worker; provider semaphore와 total worker를 모두 통과한 뒤에만 외부 호출
- 한 response의 여러 kind는 정확히 한 `PublishBatch`; empty observations는 publish 생략 후 `lease.Complete()`
- config owner: `settings.Config.YouTubeCollector`; non-default override test 통과
- collector Compose는 collector budget env만 interpolation; `HOLODEX_API_KEY`는 collector service에 전달하지 않음
- adapter는 API key가 있으면 호출하고 없으면 collect-time fail closed
- collector `go.mod`에서 `github.com/prometheus/client_golang v1.24.1`을 direct require로 승격 (version upgrade 없음)
- metric label은 bounded `provider`/`kind`/`result`/`phase`/`outcome`만 사용

production migration, deploy, restart, secret sync, live data 변경, commit과 remote write는 수행하지 않았습니다. Task 4–10과 producer 제거는 구현하지 않았습니다.

### 최종 검증 증거

2026-08-14 최종 상태에서 다음 검증이 통과했습니다.

```text
go test -count=1 \
  ./hololive/hololive-youtube-collector/internal/runtime/... \
  ./hololive/hololive-shared/pkg/contracts/sourceobservation \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation \
  ./hololive/hololive-shared/pkg/config/settings

(cd hololive/hololive-youtube-collector/youtubejs && npm test)
# 42 pass / 0 fail

go test -race -count=1 \
  ./hololive/hololive-youtube-collector/internal/runtime/...

go test -run '^$' -bench '^BenchmarkCanonicalizeJSON1MiB$' -benchmem \
  ./hololive/hololive-shared/pkg/contracts/sourceobservation
# BenchmarkCanonicalizeJSON1MiB-12  48  25874544 ns/op  11532227 B/op  56 allocs/op
```

YouTube.js process lifetime는 `youtubejs.Helper`, helper HTTP는 `youtubejs.RPC`입니다. `collectorruntime`에서 `communitycollector|PollWithLease|currentCommunityContractGeneration` production match는 없습니다. adapter package의 `youtube-producer|notification|dispatchoutbox|canonical` match는 `youtubejscollector` import-boundary 금지어 검사뿐이며 producer/canonical/notification ownership import는 없습니다. scoped `git diff --check`는 통과했습니다. 성능 개선 주장은 하지 않습니다.

### NFR / tech-debt

- queue, worker, page, payload, retry, jitter, TTL, provider inflight, helper body는 collector config bound입니다.
- timeout/parser drift는 observation/checkpoint를 만들지 않습니다. Holodex empty live array는 POSITIVE_ONLY로 0건이며 complete absence로 격상하지 않습니다.
- Official `isLive`는 `ScheduleItemV1.IsLive` evidence only입니다.
- Holodex high-level fallback service를 evidence로 재사용하지 않습니다.
- Community result-length exhaustion 추론은 제거되었고 continuation/exhaustion metadata를 사용합니다.

의도적으로 남기는 항목:

- `viewer_sample` contract subject는 video ID입니다. Task 2 operational roster는 channel ID로 `viewer_sample` target을 심습니다. production publish fence mismatch는 Task 4 진입 조건입니다.
- Holodex가 채널×kind로 `MaxPublishBatchSize=100`을 넘기면 부분 kind drop 없이 fail closed입니다. 현재 fixture는 작은 응답입니다.
- collector Compose가 `HOLODEX_API_KEY`를 받지 않으므로 production Holodex collect는 key wiring 전까지 collect-time fail closed입니다.
- helper HTTP limiter는 기존 `ProvideYouTubeProducerRateLimiterWithConfig`를 재사용합니다. collector budget owner는 아닙니다.
- Stage 3 function-budget은 Task 10 full gate가 소유합니다.

### Task 4 진입 조건

1. `viewer_sample` subject를 video ID로 projection하거나, collector publish fence가 그 identity를 받게 할 것. 현재 Task 2 target은 channel ID입니다.
2. Holodex `live_snapshot` subject는 channel ID이며 operational roster와 일치합니다. `viewer_sample`은 video ID지만 동일한 2분 cadence의 `holodex_live` batch에서 별도 subject 공간을 사용합니다.
3. Holodex live API는 `channel_profile` 필드를 제공하지 않아 해당 kind를 발행하지 않습니다. profile evidence는 YouTube.js `youtubejs_channel_metadata`가 소유합니다.
4. collector는 아직 production에 배포되지 않았고 API YouTube plane/reconciler는 Task 4 이후입니다.

---

**Architecture:** Go `youtube-collector`가 provider fetch, parsing, normalization, coverage/completeness 판정과 observation publish를 소유합니다. TypeScript는 YouTube.js helper process로만 유지하며 canonical envelope와 identity는 Go가 생성합니다. 각 adapter는 provider 고유 응답을 typed observation batch로 바꾸되 provider precedence, canonical reconciliation, notification 또는 producer compatibility path를 소유하지 않습니다.

**Tech Stack:** Go 1.26, PostgreSQL/pgx, Prometheus, Node.js 22+/YouTube.js helper, `source-observation-canonical-json-v1`.

## 역할과 단일 완료 목표

`/home/kapu/work/iris-stack/hololive-bot`에서 canonical contract의 다음 범위만 구현하십시오.

> `Task 3 — Three-provider collector adapters`: 세 provider의 fixture-backed 수집과 truthful pagination/exhaustion metadata를 구현하고, 모든 enabled job이 Task 2의 monotonic lease와 publish fence를 통과해 observation을 발행하게 한다.

Task 4의 API YouTube plane/Community canonical ownership 이전, Task 5–8 reconciler, Task 9 producer 제거·deployment 전환, Task 10 full gate는 구현하지 마십시오.

## 시작 workflow

1. `$executing-plans`로 이 handoff만 실행합니다.
2. 구현 완료 뒤 `$nfr-gate`, `$tech-debt-review`가 설치되어 있으면 적용합니다. 없으면 canonical contract Sections 20–21을 같은 blocking rule로 직접 적용합니다.
3. 완료 주장 직전에 `$verification-before-completion`을 적용합니다.
4. 첫 commentary에서 dirty worktree 보존, Task 3 범위와 첫 discovery pass를 알립니다.
5. subagent는 사용자가 해당 실행 세션에서 명시적으로 승인하지 않으면 사용하지 않습니다.

## Worktree identity

- Absolute worktree: `/home/kapu/work/iris-stack/hololive-bot`
- Expected branch: `feat/schedule-api-and-community-observation`
- Handoff 작성 시 HEAD: `5248898cd`
- Repository type: Go monorepo with central and AP runtimes
- Migration `144`–`155`와 `youtube-collector`는 production에 적용되지 않았습니다.
- Task 1, Task 2와 Canonical JSON v1 후속은 현재 dirty worktree에서 로컬 구현·targeted validation을 완료했습니다.

시작 직후 다음을 확인하십시오.

```bash
cd /home/kapu/work/iris-stack/hololive-bot
pwd
git branch --show-current
git rev-parse --short=9 HEAD
git status --short
```

worktree 또는 branch가 다르면 수정하지 말고 차이를 보고하십시오. HEAD가 달라졌으면 현재 code와 diff를 우선 조사하십시오. 대규모 modified/untracked WIP는 사용자 소유이므로 관련 없는 파일을 restore, reset, checkout 또는 삭제하지 마십시오.

## 반드시 읽을 문서

다음 순서로 읽습니다.

1. `/home/kapu/work/iris-stack/hololive-bot/AGENTS.md`
2. `/home/kapu/work/iris-stack/hololive-bot/docs/current/architecture/youtube-three-provider-convergence-contract-v2-20260814.md`
   - Sections 1–6, 8, 10, 15–18의 Task 3, 19–23
3. `/home/kapu/work/iris-stack/hololive-bot/docs/current/architecture/youtube-collector-convergence-status-20260814.md`
4. `/home/kapu/work/iris-stack/hololive-bot/docs/current/handoffs/2026-08-14-youtube-convergence-task2-implementation-handoff.md`
5. `/home/kapu/work/iris-stack/hololive-bot/docs/current/contracts/source-observation-canonical-json-v1.md`

Canonical source of truth는 두 번째 문서의 v2.1 contract입니다. 이 handoff와 현재 WIP가 contract와 다르면 contract와 현재 검증된 code를 근거로 판단하고, 규범을 임의 완화하지 마십시오.

## 확정된 선행 상태

Task 3은 다음을 새로 만들지 않고 재사용해야 합니다.

- provider/kind typed envelope와 V1 payload/coverage contract
- `source-observation-canonical-json-v1` 및 language-neutral fixture
- `InitialJobContracts`의 `community_collect`, `holodex_live`, `holodex_schedule`, `holodex_metadata`, `official_schedule`, `youtubejs_content`, `youtubejs_channel_live`, `youtubejs_channel_metadata`, `youtubejs_viewer`
- generation-based target projection과 current/valid target fence
- PostgreSQL job candidate/acquire/renew/complete/defer/release와 stale epoch fence
- publish transaction의 per-observation target/job/contract 검증
- Community lease-loss cancellation/join과 bounded queue/worker baseline

Task 2 종료 시 obsolete alarm/member `channelResolver`는 삭제됐습니다. 다시 도입하거나 target projection과 병행하는 compatibility target source를 만들지 마십시오.

## 수정 전 bounded discovery

한 번의 `rg`/`rg --files` batch로 다음을 확인하고, 사실이 달라졌을 때만 계획을 조정하십시오.

- `collectorruntime/scheduler.go`의 Community-only runner, local lease/budget constants와 queue ownership
- `collectorruntime/infrastructure.go`가 현재 YouTube.js helper와 shared infra만 구성하는 상태
- `communitycollector.PollWithLease`가 fetch, envelope, checkpoint, publish를 한 package에서 직접 수행하는 상태
- Go helper response가 posts만 반환하고 `fetch-community.mjs`가 cursor/page/exhaustion metadata를 보존하지 않는 상태
- `InitialJobContracts` emission과 target projection의 provider/kind/subject/cadence
- V1 payload/coverage bounds와 `PrepareEnvelope` validation
- Holodex 기존 high-level service의 cache/fallback/retry behavior와 raw response mapping surface
- Official Schedule fetch/parser가 현재 shared htmlscraper/producer-oriented domain mapping 안에 있는 상태
- collector metrics registration 방식과 health/metrics server wiring
- Task 2 tests의 lease loss, atomic publish, global singleton과 subject distribution fixtures

현재 확인된 사실은 다음과 같습니다.

- `holodexcollector`, `officialcollector`, `youtubejscollector` package는 아직 없습니다.
- current scheduler는 `communitycollector.PollerName`과 `PollWithLease`에 직접 결합됩니다.
- lease TTL `1m`, renew `20s`, normalization/publish budget 각 `5s`, acquisition cadence `1s`가 local constant입니다.
- existing `Scraper` worker/timeout/backoff override는 runtime에 도달하지만 collector-specific lease/provider budget owner는 없습니다.
- YouTube.js helper는 Community 한 endpoint만 제공하며 result length로 exhaustion을 추정합니다.
- Holodex high-level service에는 cache와 Official/YouTube fallback이 있어 collector evidence provenance adapter로 그대로 사용할 수 없습니다.
- Official `isLive`는 schedule evidence field일 뿐 canonical `LIVE` truth가 아닙니다.
- 모든 V1 payload/coverage type과 compile-time job vocabulary는 이미 존재합니다.

## Task 3 완료 범위

### 1. Typed job runner registry

Community 전용 `leaseScheduler` 분기를 다음 책임으로 일반화합니다.

```go
type JobRunner interface {
	Provider() contract.Provider
	JobKind() string
	Emissions() []contract.ObservationKind
	Collect(ctx context.Context, input RunInput) (RunOutput, error)
}

type RunInput struct {
	Spec                joblease.JobSpec
	Lease               contract.LeaseProof
	ContractGenerations map[contract.ObservationKind]int64
}

type RunOutput struct {
	Observations     []contract.Envelope
	Checkpoints      []sourceobservation.CheckpointEntry
	CollectionLatency time.Duration
}
```

이름은 현재 package convention에 맞춰 다듬을 수 있지만 다음 invariant는 바꾸지 마십시오.

- registry key는 `(provider, collection_job_kind)`이며 duplicate, unknown emission과 `InitialJobContracts` mismatch는 startup에서 fail closed합니다.
- scheduler는 모든 registry entry의 candidates를 순회하되 하나의 bounded queue와 total worker limit을 공유합니다.
- provider별 semaphore/request budget과 total worker gate를 모두 통과한 뒤에만 외부 호출합니다.
- owner instance, acquisition loop, renew/cancel/join과 retry/defer policy는 공통 scheduler가 한 번만 소유합니다.
- current contract generation은 runner emission 전체를 bounded set query로 읽고 kind별 map으로 전달합니다.
- adapter가 한 provider response에서 여러 kind를 만들면 scheduler/publisher가 정확히 한 번의 `PublishBatch`로 observation과 checkpoint를 commit합니다.
- required envelope 하나라도 normalize/validate 실패하면 해당 response batch 전체를 publish하지 않습니다.
- Community는 이 registry의 YouTube.js runner로 흡수합니다. 기존 `communitycollector` direct publish path나 compatibility wrapper를 병행하지 마십시오.

### 2. Collector configuration owner

`settings.Config.YouTubeCollector`와 전용 config file을 추가하고 Task 2 local constants를 이동합니다. 기존 default behavior는 보존하되 non-default override가 runtime에 실제 도달하는 test를 작성합니다. `ScraperConfig`나 `YouTubeProducerGlobalBudgetConfig`를 collector config owner로 계속 사용하지 마십시오.

필수 config surface:

```text
YOUTUBE_COLLECTOR_TOTAL_WORKERS
YOUTUBE_COLLECTOR_QUEUE_CAPACITY
YOUTUBE_COLLECTOR_ACQUISITION_BATCH
YOUTUBE_COLLECTOR_ACQUISITION_CADENCE_MS
YOUTUBE_COLLECTOR_LEASE_TTL_SECONDS
YOUTUBE_COLLECTOR_RENEW_INTERVAL_SECONDS
YOUTUBE_COLLECTOR_NORMALIZATION_BUDGET_SECONDS
YOUTUBE_COLLECTOR_PUBLISH_BUDGET_SECONDS
YOUTUBE_COLLECTOR_RETRY_MIN_SECONDS
YOUTUBE_COLLECTOR_RETRY_MAX_SECONDS
YOUTUBE_COLLECTOR_RELEASE_JITTER_MIN_MS
YOUTUBE_COLLECTOR_RELEASE_JITTER_MAX_MS
YOUTUBE_COLLECTOR_HOLODEX_MAX_INFLIGHT
YOUTUBE_COLLECTOR_OFFICIAL_MAX_INFLIGHT
YOUTUBE_COLLECTOR_YOUTUBEJS_MAX_INFLIGHT
YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS
YOUTUBE_COLLECTOR_MAX_PAGES
YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES
```

Default는 현재 동작과 contract를 유지합니다.

- total workers: `DefaultScraperWorkerCount()`와 같은 현재 numeric default를 collector config에 명시
- queue capacity: `4 * total workers`, `joblease.MaxQueueCapacity` 이하
- acquisition batch: `min(queue capacity, joblease.MaxAcquisitionBatch)`
- cadence: `1s`
- lease TTL: `60s`
- renew interval: `20s`
- normalization/publish budget: 각 `5s`
- retry min/max: current `DefaultScraperSchedulerConfig()`와 동일
- release jitter min/max: `100ms`/`1s`
- provider max inflight: default는 total workers와 같고 항상 total workers 이하이며 최소 `1`
- YouTube.js timeout: current Go helper HTTP timeout과 같은 `30s`
- max pages: current one-page behavior를 보존하는 `1`, 허용 범위 `1..100`
- max aggregate bytes: `contract.MaxPayloadBytes`인 `1 MiB` 이하

Validation은 `joblease.Config.Validate`를 재사용하고 다음을 추가로 fail closed합니다.

- `max(Holodex timeout, Official timeout, YouTube.js timeout) + normalization + publish >= lease TTL`
- provider max inflight 또는 total workers가 `1` 미만이거나 existing upper bound 초과
- provider max inflight가 total workers 초과
- max pages `1..100` 밖
- max aggregate bytes가 `1..1 MiB` 밖
- renew interval이 TTL의 1/3 초과
- retry/release jitter가 `joblease.Config.Validate` 범위 밖

새 env는 `.env.example`과 collector Compose environment에만 전달합니다. default-only Compose interpolation을 추가하되 service deploy/recreate는 수행하지 마십시오. producer budget env를 collector 이름으로 alias하거나 재사용하지 마십시오.

### 3. Holodex collector adapter

`internal/runtime/holodexcollector`가 Holodex API request, fixture parser, normalization과 batch 구성을 소유합니다.

- cadence별 `holodex_live`, `holodex_schedule`, `holodex_metadata` lease와 current projection target set을 사용합니다.
- high-level `holodexprovider.Service`의 Official/YouTube fallback, producer retry scheduler 또는 mixed cache result를 evidence로 재사용하지 않습니다.
- 기존 low-level HTTPS transport/rate-limit utility를 재사용할 수 있지만 provider response body와 provenance는 Holodex로 고정해야 합니다.
- API 응답 fixture로 입증된 필드만 `live_snapshot`, `viewer_sample`, `channel_stats`, `channel_profile`, `channel_photo`, `schedule_snapshot`에 발행합니다.
- hidden/missing viewer count를 `0`으로 만들지 않고 typed availability로 보존합니다.
- requested channel IDs는 projection에서 읽어 trim/validate/sort/deduplicate하고 coverage에 그대로 결합합니다.
- global response가 requested subset과 failed channel을 구분하지 못하면 `POSITIVE_ONLY`이며 `SCOPED_ABSENCE`를 선언하지 않습니다.
- response limit/pagination 종료를 증명하지 못하면 `PARTIAL`; timeout, parse drift, cursor gap은 no publish입니다.
- 동일 Holodex response에서 파생된 모든 kind는 한 atomic `PublishBatch`에 들어갑니다.

Fixture에는 최소한 reordered response, empty exhausted success, missing/hidden viewer, malformed schema, timeout, page gap과 oversized aggregate를 포함합니다.

### 4. Official Schedule collector adapter

`internal/runtime/officialcollector`가 `official_schedule` global job을 직접 실행합니다.

- subject는 `global:hololive-schedule`입니다.
- configured HTTPS origin, timeout과 `MaxResponseBodyBytes`를 사용하고 status/content-type/body bound를 검증합니다.
- 기존 shared htmlscraper parser를 호출해 producer domain state로 변환하지 않고 collector-owned fixture parser가 `ScheduleSnapshotV1`을 만듭니다.
- successful complete response만 `COMPLETE`; timeout, invalid JSON, schema drift, oversized body는 no publish입니다.
- item ordering 변화가 canonical payload/hash를 바꾸지 않아야 합니다.
- `is_live`는 `ScheduleItemV1.IsLive` evidence로만 남기고 `live_snapshot`, canonical live transition 또는 notification을 만들지 않습니다.
- empty successful response는 fixture가 response 전체 범위를 증명할 때만 complete-empty입니다.

### 5. YouTube.js collector/helper expansion

Go collector를 유지하고 TypeScript는 Unix-socket helper로만 사용합니다.

- helper response는 items만 반환하지 않고 `page_count`, `cursor_start`, `cursor_end`, `exhausted`, `continuity`와 bounded result metadata를 반환합니다.
- Community는 실제 continuation/exhaustion을 보존합니다. result length로 exhausted를 추정하지 않습니다.
- `youtubejs_content`, `youtubejs_channel_live`, `youtubejs_channel_metadata`, `youtubejs_viewer` runner를 추가해 Community 포함 8개 YouTube.js kind를 fixture-backed로 활성화합니다.
- kind별 helper endpoint 또는 typed request union을 사용할 수 있지만 universal `map[string]any` response를 만들지 않습니다.
- helper는 raw provider data와 pagination metadata만 반환합니다. canonical JSON, observation key/hash, lease proof와 DB publish는 Go가 소유합니다.
- 첫 validated page 전 timeout/transport/parser failure는 no publish입니다. 한 page 이상 검증한 뒤 후속 page 누락·timeout, cursor loop, max-page/max-byte 도달은 검증된 prefix만 `PARTIAL + GAP_UNRESOLVED`로 발행합니다. parser schema drift는 prefix가 있어도 no publish입니다.
- empty successful exhausted response만 `COMPLETE`; missing tab/unsupported field를 complete absence로 과장하지 않습니다.
- Community와 fixture가 complete absence를 입증하지 않은 kind는 `POSITIVE_ONLY`로 유지합니다.
- 각 enabled kind는 raw fixture, mapper fixture와 Go envelope fixture가 모두 있어야 합니다. fixture 없는 parser field/kind는 enable하지 않습니다.

TypeScript collector 또는 TypeScript canonical envelope writer는 만들지 마십시오. 8개 kind 활성화 뒤 helper RPC call count, latency, CPU와 failure amplification을 동일 workload에서 측정해 material bottleneck이 확인될 때만 별도 리뷰에서 언어 변경을 검토합니다.

### 6. Publish, failure와 observability

Task 3에서 다음 collector metric과 low-cardinality label contract를 구현합니다.

```text
youtube_collection_attempts_total{provider,kind,result}
youtube_collection_duration_seconds{provider,kind}
youtube_collection_last_success_timestamp_seconds{provider,kind}
youtube_collection_freshness_seconds{provider,kind}
youtube_collection_completeness_total{provider,kind,completeness,continuity}
youtube_collection_lease_acquire_total{provider,kind,result}
youtube_collection_lease_lost_total{provider,kind,phase}
youtube_observation_publish_total{provider,kind,outcome}
```

- `provider`, `kind`, `result`, `phase`, `outcome`은 compile-time bounded vocabulary만 사용합니다.
- `subject_key`, `job_key`, cursor, error text를 metric label로 사용하지 않습니다.
- structured log는 canonical Section 16 field 중 현재 phase에서 유효한 값만 사용하고 payload, body, token, API key, description을 기록하지 않습니다.
- timeout/cancel, parser drift, pagination gap, cooldown, lease loss와 publish rejection을 다른 bounded error code로 구분합니다.
- provider request count와 helper RPC count를 fixture test에서 고정해 duplicate call amplification을 막습니다.
- retry는 existing bounded provider policy를 넘지 않고 context deadline/cancellation을 보존합니다.
- response body, rows, timers, helper process와 worker/renewer는 모든 경로에서 close/join합니다.

## File ownership map

### Primary write boundary

```text
hololive/hololive-youtube-collector/internal/runtime/collectorruntime/**
hololive/hololive-youtube-collector/internal/runtime/holodexcollector/**
hololive/hololive-youtube-collector/internal/runtime/officialcollector/**
hololive/hololive-youtube-collector/internal/runtime/youtubejscollector/**
hololive/hololive-youtube-collector/internal/runtime/youtubejs/**
hololive/hololive-youtube-collector/youtubejs/src/**
hololive/hololive-youtube-collector/youtubejs/package.json
hololive/hololive-youtube-collector/youtubejs/package-lock.json
```

기존 `communitycollector/**`는 registry 전환과 test 이동에 필요한 범위에서 수정·삭제할 수 있습니다. 완료 상태에는 production-reachable direct Community publish path 또는 duplicate runner가 남아 있으면 안 됩니다.

예상 file responsibility는 다음과 같습니다. 현재 package convention상 더 작은 파일 분리가 필요하면 책임은 유지하되 unrelated refactor를 추가하지 마십시오.

```text
collectorruntime/scheduler.go       common candidate queue, acquire/run/defer lifecycle
collectorruntime/registry.go        typed runner registration and contract validation
collectorruntime/publisher.go       contract generation batch load and one PublishBatch
collectorruntime/metrics.go         bounded collector metric vectors
collectorruntime/infrastructure.go  three provider clients, semaphores and cleanup

holodexcollector/client.go          Holodex-only bounded HTTP calls
holodexcollector/mapper.go          raw fixture to typed V1 payloads
holodexcollector/runner.go          cadence-specific Holodex batch construction
holodexcollector/testdata/*.json    raw provider fixtures

officialcollector/client.go         Official JSON API request/status/body bounds
officialcollector/mapper.go         schedule fixture to ScheduleSnapshotV1
officialcollector/runner.go         official_schedule batch construction
officialcollector/testdata/*.json   raw provider fixtures

youtubejscollector/runner.go        community/content/channel/viewer job runners
youtubejscollector/mapper.go        helper result to typed V1 payloads
youtubejs/helper.go                 typed Go helper request/response protocol
youtubejs/src/server.mjs            bounded endpoint routing
youtubejs/src/fetch-community.mjs   truthful Community pagination metadata
youtubejs/src/fetch-content.mjs     video/shorts fixture-backed fetch
youtubejs/src/fetch-channel.mjs     live/stats/profile/photo fixture-backed fetch
youtubejs/src/fetch-viewer.mjs      viewer fixture-backed fetch
youtubejs/src/*.test.mjs            raw/helper protocol regressions
```

### Conditional contract/config boundary

아래는 Task 3 fixture와 runtime config에 필요한 최소 변경만 허용됩니다.

```text
hololive/hololive-shared/pkg/contracts/sourceobservation/**
hololive/hololive-shared/pkg/service/youtube/sourceobservation/**
hololive/hololive-shared/pkg/config/settings/config.go
hololive/hololive-shared/pkg/config/settings/config_youtube_collector.go
hololive/hololive-shared/pkg/config/settings/config_youtube_collector_test.go
hololive/hololive-shared/pkg/config/settings/config_scraper_*.go
hololive/hololive-shared/pkg/config/settings/config_env_loaders.go
hololive/hololive-shared/pkg/config/settings/config_validation.go
hololive/hololive-shared/pkg/config/settings/*test.go
.env.example
deploy/compose/docker-compose.prod.yml
deploy/compose/docker-compose.osaka.yml
deploy/compose/docker-compose.osaka2.yml
deploy/compose/docker-compose.seoul.yml
deploy/compose/docker-compose.main-ap.yml
hololive/hololive-youtube-collector/go.mod
hololive/hololive-youtube-collector/go.sum
```

제한:

- provider/kind vocabulary, schema version, migration `144`–`155`와 job emission contract를 편의상 바꾸지 않습니다.
- Canonical JSON profile을 바꾸지 않습니다. fixture correction이 필요하면 contract 오류 근거와 versioning 영향을 먼저 보고합니다.
- settings는 collector config와 validation만 추가합니다. producer config alias나 broad environment rename을 하지 않습니다.
- existing dependency version을 올리거나 새 runtime dependency를 추가하지 않습니다. direct import 승격만 필요하면 `go.mod`/`go.sum` diff를 최소화합니다. 새 dependency가 필요하면 stop condition입니다.
- Compose는 env 전달만 수정하며 image, deploy policy, replica topology, secrets와 live host config를 변경하지 않습니다.

검증 성공 뒤 다음 문서에 Task 3 진척 증거만 갱신할 수 있습니다.

```text
docs/current/architecture/youtube-collector-convergence-status-20260814.md
docs/current/handoffs/2026-08-14-youtube-convergence-task3-implementation-handoff.md
```

## 명시적 비범위

- `hololive-api` YouTube plane, queue consumer, reconciler, canonical state와 notification intent
- producer registration 삭제, binary/module/Compose/systemd 제거
- Task 4 이후 Community canonical ownership 이전
- live end due-finalizer, profile/photo canonical selection, schedule merge
- migration, schema golden, DB grants 또는 contract generation 변경
- production build artifact, deploy, restart, secret sync, live DB/API 호출
- TypeScript collector, TypeScript canonical writer 또는 Go/TS runtime parity 구현
- provider precedence, primary/fallback branch 또는 shadow/dual writer
- full pre-push, repository-wide NilAway/perf/security gate

기존 producer/shared provider code는 read-only reference로만 사용합니다. Task 3에서 삭제·rewire하지 마십시오.

## 구현 순서

- [ ] **1. Baseline discovery와 contract mapping**
  - 현재 job emissions, target subjects, adapter fixture source와 config defaults를 표로 정리합니다.
  - fixture로 증명할 수 없는 provider field를 미리 제외합니다.

- [ ] **2. Collector config와 validation**
  - collector-owned config type/default/env loader를 추가합니다.
  - invalid budget과 non-default runtime override test를 먼저 작성합니다.
  - scheduler가 local constants 대신 validated config만 사용하게 합니다.

- [ ] **3. Typed runner registry와 common publisher**
  - registry duplicate/mismatch/startup failure test를 작성합니다.
  - contract generation batch load, shared queue/semaphore와 one-`PublishBatch` path를 구현합니다.
  - Community direct publish를 새 runner로 이동하고 구 path를 제거합니다.

- [ ] **4. Truthful YouTube.js pagination contract**
  - Node fixture test로 exhausted, continuation, missing page, cursor loop와 max-page/max-byte를 고정합니다.
  - Go helper typed response와 Community envelope semantics를 갱신합니다.

- [ ] **5. YouTube.js 8-kind fixture-backed runners**
  - content, channel, viewer helper/mapper fixture를 추가합니다.
  - input permutation이 canonical bytes/hash를 바꾸지 않는 Go test를 추가합니다.
  - fixture 없는 field와 absence capability는 enable하지 않습니다.

- [ ] **6. Official Schedule adapter**
  - success/empty/schema/status/content-type/oversize/timeout fixture를 작성합니다.
  - `schedule_snapshot` envelope와 atomic publish를 구현합니다.
  - `is_live` evidence-only regression을 고정합니다.

- [ ] **7. Holodex adapter**
  - raw API fixtures와 page/result bounds를 작성합니다.
  - live/viewer/channel/schedule typed observations를 한 response batch로 구성합니다.
  - fallback-free provenance, requested ID coverage와 hidden viewer regression을 고정합니다.

- [ ] **8. Metrics, logs와 resource lifetime**
  - isolated Prometheus registry로 metric names/labels/outcomes를 test합니다.
  - lease loss, timeout, parser drift, partial과 publish rejection의 bounded log/error code를 검증합니다.
  - shutdown에서 worker, renewer, timer와 helper process가 join되는 race test를 유지합니다.

- [ ] **9. NFR evidence와 final validation**
  - max-sized canonical payload benchmark를 동일 fixture workload로 실행합니다.
  - request/RPC/query count가 configured bound 안인지 test evidence를 남깁니다.
  - 아래 command와 scoped diff hygiene를 final state에서 실행합니다.

- [ ] **10. Status와 handoff 완료 갱신**
  - 검증 성공 뒤에만 status를 `Task 3 로컬 검증 완료`로 갱신합니다.
  - 실제 enabled fixture/kind, validation 결과와 Task 4 진입 조건을 기록합니다.

## 필수 regression matrix

```text
registry duplicate or emission mismatch -> startup failure
non-default collector config -> runtime scheduler/adapter budget에 반영
invalid lease/provider budget -> startup failure
global Holodex/Official job -> fleet-wide one active holder
YouTube.js subject jobs -> distributed, same slot duplicate publish 없음
lease renewal loss -> provider request cancel + no publish + renewer join
target disabled or generation changed mid-fetch -> whole batch rollback
one response, multiple kinds -> one PublishBatch transaction
input ordering permutation -> canonical payload/scope/evidence hash unchanged
validated prefix 뒤 page 누락/timeout/cursor loop/max page/max bytes -> PARTIAL + GAP_UNRESOLVED
first page timeout 또는 any-page parser drift -> no publish
empty successful exhausted page -> COMPLETE
POSITIVE_ONLY complete-empty -> absence capability 격상 없음
timeout/parser/status/content-type error -> no observation/checkpoint/job completion publish
requested IDs -> trimmed, sorted, deduplicated coverage
hidden viewer count -> typed unavailable, zero로 변환하지 않음
Official isLive -> schedule evidence only
metric labels -> bounded vocabulary, subject/cursor/error text 없음
shutdown -> helper/worker/renewer/timer join
```

## Validation

먼저 Task 3 direct scope를 실행합니다.

```bash
go test -count=1 \
  ./hololive/hololive-youtube-collector/internal/runtime/... \
  ./hololive/hololive-shared/pkg/contracts/sourceobservation \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation \
  ./hololive/hololive-shared/pkg/config/settings

(cd hololive/hololive-youtube-collector/youtubejs && npm test)

go test -race -count=1 \
  ./hololive/hololive-youtube-collector/internal/runtime/...
```

8개 YouTube.js kind를 활성화하기 전에 Canonical JSON max workload evidence를 남깁니다. benchmark 이름은 `BenchmarkCanonicalizeJSON1MiB`로 고정해 CI와 handoff에서 같은 command를 재실행할 수 있게 합니다.

```bash
go test -run '^$' -bench '^BenchmarkCanonicalizeJSON1MiB$' -benchmem \
  ./hololive/hololive-shared/pkg/contracts/sourceobservation
```

Architecture scan은 최소 다음을 증명해야 합니다.

```bash
rg -n "communitycollector|PollWithLease|currentCommunityContractGeneration" \
  hololive/hololive-youtube-collector/internal/runtime/collectorruntime

rg -n "youtube-producer|notification|dispatchoutbox|canonical" \
  hololive/hololive-youtube-collector/internal/runtime/{holodexcollector,officialcollector,youtubejscollector}
```

첫 scan은 완료 상태에서 obsolete Community-specific scheduler reference가 없어야 합니다. 두 번째 scan의 match는 type/import 문맥을 직접 검토하고 producer/canonical/notification ownership import가 없어야 합니다. 단어 match만으로 자동 통과 처리하지 마십시오.

마지막으로 touched scope의 `git diff --check`와 untracked 문서/fixture trailing whitespace를 확인합니다. 전체 pre-push와 production build는 Task 10이 소유하므로 이번 Task 3에서 임의로 확대하지 않습니다.

## NFR blocking rules

- queue, worker, acquisition batch, page, payload, retry, jitter, TTL, provider inflight와 helper body는 모두 bounded합니다.
- provider request 수와 helper RPC 수는 input size/config의 명시적 상한을 가집니다.
- helper/HTTP/DB error를 complete-empty 또는 negative evidence로 변환하지 않습니다.
- provider timeout은 lease context cancellation을 보존하고 detached retry를 만들지 않습니다.
- high-cardinality metric label, raw response/body, cursor, credential과 full description logging을 금지합니다.
- response body, DB rows, ticker, timer, semaphore permit, worker, renewer와 helper process owner를 명확히 합니다.
- parser drift와 unknown schema는 permanent collection error이며 no publish입니다.
- 동일 response multi-kind publish는 atomic이고 stale target/epoch/contract failure 때 checkpoint/job state까지 rollback됩니다.
- 성능 개선 주장은 동일 workload의 benchmark/request/query evidence 없이 하지 않습니다.

## Tech-debt blocking rules

Touched scope에는 다음을 남기지 마십시오.

- Community compatibility runner나 duplicate scheduler
- provider별 `if/switch`가 계속 증가하는 orchestration 대신 없는 typed registry
- fallback provider 결과를 원 provider evidence로 표시하는 path
- untyped universal payload/map
- fixture 없는 enabled parser field/kind
- local hard-coded lease/provider budget
- TODO/FIXME, skipped test, test-only production bypass
- broad lint/race/NilAway/perf/architecture suppression
- TypeScript canonical writer 또는 premature collector language split
- temporary duplicate observation publisher

Task 3에서 의도적으로 남기는 항목은 owner, trigger와 exit condition을 final handoff에 기록하십시오. 실행을 막는 debt는 같은 task에서 해결합니다.

## Stop conditions

다음 중 하나면 code로 우회하지 말고 file/section evidence와 최소 선택지를 보고하십시오.

- fixture가 provider pagination, completeness, field 의미 또는 absence semantics를 증명하지 못함
- Task 3 correctness에 migration/schema/job vocabulary 변경이 필요함
- 새 runtime dependency 또는 dependency version upgrade가 필요함
- provider credential, live external request 또는 production data가 있어야만 parser semantics를 확정할 수 있음
- current target projection이 required subject/interval을 표현하지 못함
- one response multi-kind atomic publish가 기존 repository contract로 불가능함
- Task 4 API plane/reconciler 구현 없이는 adapter correctness 자체를 검증할 수 없음
- 둘 이상의 materially different architecture가 있고 선택이 public contract, cost 또는 rollback을 바꿈
- 기존 WIP와 write boundary가 충돌해 사용자 변경을 덮어써야 함

불확실한 provider field는 추측해 normalize하지 말고 fixture-backed subset으로 좁힙니다. 그러나 compile-time job과 projection이 실제로 enable한 kind를 runner 없이 silent skip하는 상태로 Task 3 complete를 주장하지 마십시오.

## 완료 판정

다음을 모두 만족해야 `Task 3 complete`라고 보고할 수 있습니다.

1. Holodex, Official Schedule, YouTube.js 세 provider가 collector-owned adapter와 fixture를 가짐
2. Community 포함 8개 YouTube.js kind가 fixture-backed typed helper/Go mapper를 가지며 enabled job에 runner가 있음
3. Community result-length exhaustion 추론이 제거되고 page/cursor/exhausted/continuity가 실제 metadata에서 옴
4. typed registry가 `InitialJobContracts`와 startup에서 일치하며 Community-only compatibility path가 없음
5. total/provider concurrency, queue, lease, cadence, page와 payload config가 bounded하고 override test를 통과함
6. 한 response의 multi-kind observations/checkpoints가 한 `PublishBatch`로 atomic commit됨
7. partial/gap/timeout/parser failure가 complete-empty나 negative evidence를 만들지 않음
8. Holodex fallback provenance 오염이 없고 Official `isLive`가 schedule evidence로만 남음
9. required collection/lease/publish metrics와 bounded log contract test가 통과함
10. lease loss cancellation, stale fence rollback, request/RPC bound와 shutdown race가 통과함
11. Canonical JSON 1 MiB benchmark가 재실행 가능하고 `ns/op`, `B/op`, `allocs/op` 결과를 기록함; baseline이 없는 성능 개선 주장은 하지 않음
12. canonical Task 3 Go/Node/race validation과 scoped hygiene가 final state에서 통과함
13. Task 4–10, producer 제거, migration과 production operation을 수행하지 않음

## 최종 보고 형식

최종 응답과 이 handoff 상단 완료 업데이트에는 다음만 간결히 기록합니다.

- Outcome: `complete` 또는 concrete blocker
- 실제 enabled provider/kind와 fixture 목록
- typed registry/config/publish/observability 변경 요약
- 실제 실행한 validation command와 결과
- NFR/tech-debt 판정과 남은 Task 4 진입 조건
- production migration/deploy/restart/secret/live data 변경 여부
