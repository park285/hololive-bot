# YouTube Producer 3-Way Active-Active 다중 워커 최적화 코드레벨 결정서

작성일: 2026-06-04 KST
대상 저장소: `park285/hololive-bot`
대상 런타임: `hololive-youtube-producer`
대상 인스턴스: `youtube-producer-a`, `youtube-producer-b`, `youtube-producer-c`
상태: 구현 작업 가능 기준 문서.

---

## 1. 이 문서의 목적

현재 `youtube-producer`는 Osaka의 `youtube-producer-a`, `youtube-producer-b`와 main host의 `youtube-producer-c`가 동시에 실행되는 3-way active-active 구조입니다.

현재 구조는 이미 다음을 갖고 있습니다.

- `JobRunGuard` 기반의 `(pollerName, channelID)` 단위 Valkey lease/cooldown
- active-active fail-closed readiness
- lease renew 실패 시 poll cancel
- `youtube_notification_outbox(kind, content_id)` unique index
- `youtube_notification_delivery(outbox_id, room_id)` unique index
- Holodex 우선 live status 경로
- scraper fallback
- per-AP worker count
- poller별 worst-case request unit 메타데이터

이 문서는 이 구조를 **현재 다중 워커 구조에 맞게 더 안전하고 예측 가능하게 최적화하기 위한 코드레벨 결정사항**을 정리합니다.

핵심 목표는 다음입니다.

1. AP 수와 worker count를 늘려도 외부 요청량이 예측 가능해야 합니다.
2. 같은 `(poller, channel)` job은 중복 실행되지 않아야 합니다.
3. 서로 다른 job의 병렬성은 유지하되, Holodex/scraper/proxy/DB 부하가 폭주하지 않아야 합니다.
4. lease TTL이 실제 poll 실행 시간보다 짧아 중복 실행을 만드는 일이 없어야 합니다.
5. Holodex live path는 batch 가능한 구조로 최적화해야 합니다.
6. backfill은 primary job 예산을 침범하지 않아야 합니다.
7. 장애 시 중복 실행보다 fail-closed를 우선해야 합니다.
8. 운영자가 `/ready`, log, metric으로 현재 상태를 정확히 판단할 수 있어야 합니다.

---

## 1.1. 구현 전제와 확정 경계

이 문서로 바로 작업을 시작하려면 다음 경계를 변경하지 않습니다.

```text
1. Phase 1은 기존 scraper/Holodex per-request distributed limiter를 제거하거나 대체하지 않는다.
2. GlobalBudgetLimiter는 scheduler admission, global in-flight, burst class, priority, source cooldown gate를 담당한다.
3. sustained RPM은 startup validator와 기존 scraper/Holodex request limiter가 계속 방어한다.
4. runtime token bucket을 기존 request limiter와 통합하는 작업은 이 문서의 Phase 1 범위가 아니다.
5. active-active fail-closed 원칙은 Valkey coordination backend unavailable에만 적용한다.
6. budget exhausted와 source cooldown은 readiness failure가 아니라 scheduler admission 상태다.
```

작업자는 아래 항목을 구현 중 임의로 바꾸지 않습니다. 바꾸려면 별도 설계 결정서를 먼저 작성합니다.

```text
GlobalBudgetLimiter token/window policy
BudgetReservation Commit/Release terminal semantics
readiness payload contract
SCRAPER_SCHEDULER_WORKER_COUNT canonical env
LivePoller PollBatch partial failure contract
phase rollback flag names
```

현재 코드 기준의 작업 근거:

```text
hololive/hololive-shared/pkg/service/youtube/poller/internal/scheduler_worker.go
  현재 execute flow는 claim 이후 waitForJobRunSlot 다음에 poll context와 renew loop를 시작한다.

hololive/hololive-shared/pkg/providers/youtube_providers.go
  producer scraper scheduler는 RequestInterval=0으로 생성되며 request pacing은 scraper/Holodex client 쪽 limiter가 담당한다.

hololive/hololive-shared/pkg/service/youtube/scraper/internal/scraping/client_http.go
  scraper request path는 HTTP fetch 직전에 RateLimiter.WaitWithBucket을 호출한다.

hololive/hololive-shared/pkg/service/holodex/internal/holodexprovider/apiclient/api_client_rate_limit.go
  Holodex API client도 local/distributed rate limiter wait를 수행한다.

hololive/hololive-shared/pkg/service/youtube/poller/internal/pollers/live_poller.go
  LivePoller.Poll은 channel별 baseline과 ended session marking을 수행한다.

hololive/hololive-shared/pkg/config/internal/settings/config_env_loaders.go
  canonical worker env는 SCRAPER_SCHEDULER_WORKER_COUNT이고 SCRAPER_WORKER_COUNT는 alias다.
```

---

## 2. 현재 구조 요약

### 2.1 인스턴스 구성

현재 의도된 구성은 다음입니다.

| 인스턴스 | 위치 | 포트 | 역할 | PhotoSync |
|---|---:|---:|---|---|
| `youtube-producer-a` | Osaka | `30005` | scraping/polling active-active AP | 참여 |
| `youtube-producer-b` | Osaka | `30015` | scraping/polling active-active AP | 미참여 |
| `youtube-producer-c` | main host | `30025` | scraping/polling active-active AP | 참여 |

공통 요구사항은 다음입니다.

```text
YOUTUBE_PRODUCER_RUNTIME_ALLOWED=true
YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED=true
YOUTUBE_PRODUCER_INSTANCE_ID=<unique instance id>
YOUTUBE_PRODUCER_LEASE_NAMESPACE=production
SCRAPER_SCHEDULER_WORKER_COUNT=<per AP worker count>
```

중요한 점은 `YOUTUBE_PRODUCER_LEASE_NAMESPACE`가 같아야 한다는 것입니다. namespace가 다르면 서로 다른 lease key를 보게 되므로 중복 polling이 발생할 수 있습니다.

### 2.2 현재 active-active coordination 모델

현재 모델은 static sharding이 아닙니다. 즉 “a는 1~100번 채널, b는 101~200번 채널” 같은 고정 분할이 아닙니다.

현재 모델은 다음입니다.

```text
모든 AP가 같은 target set을 본다.
각 job은 (pollerName, channelID)로 식별된다.
실행 직전에 Valkey JobRunGuard로 claim한다.
한 AP만 acquired가 된다.
다른 AP는 peer_owned 또는 already_completed로 skip한다.
성공한 AP는 cooldown을 기록한다.
cooldown 동안 같은 poller+channel job은 다시 실행되지 않는다.
```

이 구조는 AP 개수가 2개든 3개든 N개든 같은 방식으로 동작합니다. 따라서 설계 자체는 수평 확장을 지원합니다.

### 2.3 현재 poller 데이터 소스

현재 producer의 poller set은 다음처럼 동작합니다.

| Poller | 주 데이터 소스 | fallback 또는 보조 경로 |
|---|---|---|
| `videos` | YouTube scraper | retry/backoff, DB idempotency |
| `shorts` | YouTube scraper | published_at resolver 가능 |
| `community` | YouTube scraper | published_at resolver 가능 |
| `stats` | YouTube scraper | 없음 |
| `live` | Holodex `/users/live` 우선 | Holodex 실패 시 scraper fallback 가능 |
| `shorts_backfill` | YouTube scraper | 별도 pollerName, 별도 cooldown |
| `community_backfill` | YouTube scraper | 별도 pollerName, 별도 cooldown |
| `live_backfill` | live poller base | 별도 pollerName, 별도 cooldown |

따라서 여기서 말하는 request budget은 공식 YouTube Data API quota 하나가 아닙니다.

정확히는 다음을 합친 운영 예산입니다.

```text
YouTube scraper 요청
Holodex /users/live 요청
Holodex 실패 시 scraper fallback 요청
browser snapshot 요청
proxy 요청
PostgreSQL write/read 부하
Valkey coordination 부하
```

---

## 3. 현재 구조에서 남아 있는 최적화 문제

### 3.1 worker count는 로컬 값이고, source-aware global budget은 아직 약합니다

현재 각 AP는 자기 `SCRAPER_SCHEDULER_WORKER_COUNT`만 봅니다.

예를 들어 AP 3개가 있고 각 AP worker count가 2라면, 전체적으로 최대 6개의 서로 다른 job이 동시에 실행될 수 있습니다.

`JobRunGuard`는 같은 `(pollerName, channelID)` 중복 실행은 막습니다. 하지만 서로 다른 channel이나 서로 다른 poller는 동시에 실행될 수 있습니다.

따라서 다음은 막습니다.

```text
producer-a: live / channel-X 실행
producer-b: live / channel-X 실행
producer-c: live / channel-X 실행
```

하지만 다음은 허용됩니다.

```text
producer-a: live / channel-A 실행
producer-a: shorts / channel-B 실행
producer-b: videos / channel-C 실행
producer-b: community / channel-D 실행
producer-c: live / channel-E 실행
producer-c: stats / channel-F 실행
```

이 병렬성 자체는 원하는 동작입니다. 문제는 이 병렬성이 Holodex, YouTube scraper, proxy, browser snapshot, PostgreSQL에 미치는 부하가 전역적으로 제어되지 않는다는 점입니다.

### 3.2 sustained request rate와 burst concurrency를 분리해서 봐야 합니다

active-active에서 AP 수를 늘려도 같은 `(poller, channel)`의 sustained rate는 늘어나면 안 됩니다. cooldown이 전역이기 때문입니다.

하지만 burst concurrency는 늘어날 수 있습니다. AP가 늘면 동시에 실행 가능한 서로 다른 job 수가 늘기 때문입니다.

따라서 budget 계산은 두 축으로 나누어야 합니다.

```text
sustained_rpm:
  전체 target identity 수와 interval로 계산한다.
  active-active AP 수를 곱하지 않는다.
  이유: 같은 identity는 JobRunGuard cooldown으로 전역 1회만 실행된다.

burst_inflight:
  AP 수 × AP별 worker count로 계산한다.
  active-active AP 수를 반영한다.
  이유: 서로 다른 identity는 동시에 실행될 수 있다.
```

이 구분 없이 “AP가 3개니까 request rate도 3배”라고 보면 과대평가입니다. 반대로 “JobRunGuard가 있으니까 worker count는 상관없다”라고 보면 과소평가입니다.

둘 다 틀립니다.

---

## 4. 최종 목표 아키텍처

최종 목표는 다음 구조입니다.

```text
Scheduler
  ├─ JobRunGuard: 같은 poller+channel 중복 실행 방지
  ├─ GlobalBudgetLimiter: source별 전역 request budget 제어
  ├─ SourceConcurrencyLimiter: source별 global in-flight 제어
  ├─ LeaseRenewLoop: claim 이후 즉시 lease renew
  ├─ OptionalBatchPoller: batch 가능한 poller는 micro-batch 실행
  └─ Metrics/Readiness: claim, budget, renew, fallback, batch 상태 노출
```

핵심은 `JobRunGuard`와 `GlobalBudgetLimiter`의 역할을 분리하는 것입니다.

`JobRunGuard`는 “누가 이 job을 실행할 것인가”를 정합니다.

`GlobalBudgetLimiter`는 “지금 이 source에 요청을 더 보내도 되는가”를 정합니다.

둘은 서로 다른 문제를 해결합니다. 하나로 합치면 안 됩니다.

---

## 5. 결정사항

## D-001. Request budget의 단위를 `BudgetSource`로 명시한다

### 결정

기존 `WorstCaseRequestUnitsPerRun`은 단일 숫자입니다. 이를 확장하여 source별 budget profile을 갖게 합니다.

추가할 타입은 다음과 같습니다.

```go
package poller

type BudgetSource string

const (
    BudgetSourceYouTubeScraper  BudgetSource = "youtube_scraper"
    BudgetSourceHolodexLive     BudgetSource = "holodex_live"
    BudgetSourceBrowserSnapshot BudgetSource = "browser_snapshot"
    BudgetSourceProxy           BudgetSource = "proxy"
    BudgetSourcePostgresWrite   BudgetSource = "postgres_write"
)

type BudgetProfile struct {
    SourceUnits map[BudgetSource]float64
    BurstClass  BudgetBurstClass
    Priority    BudgetPriority
}

type BudgetBurstClass string

const (
    BudgetBurstPrimary  BudgetBurstClass = "primary"
    BudgetBurstBackfill BudgetBurstClass = "backfill"
    BudgetBurstFallback BudgetBurstClass = "fallback"
)

type BudgetPriority string

const (
    BudgetPriorityHigh   BudgetPriority = "high"
    BudgetPriorityNormal BudgetPriority = "normal"
    BudgetPriorityLow    BudgetPriority = "low"
)
```

`ChannelPollerRegistration`에는 다음 필드를 추가합니다.

```go
type ChannelPollerRegistration struct {
    ...
    BudgetProfile BudgetProfile
}
```

그리고 builder method를 추가합니다.

```go
func (r ChannelPollerRegistration) WithBudgetProfile(profile BudgetProfile) ChannelPollerRegistration
```

### 이유

현재 worst-case request unit은 “요청량 대략치”로는 유용하지만, Holodex와 YouTube scraper를 구분하지 못합니다.

`live`는 평상시 Holodex를 씁니다. 하지만 fallback은 scraper를 씁니다. 이를 단일 숫자로 표현하면 어느 source가 병목인지 알 수 없습니다.

source별 profile이 있어야 다음을 제어할 수 있습니다.

```text
Holodex live API budget
YouTube scraper budget
browser snapshot budget
proxy budget
Postgres write budget
```

### 적용 예시

`videos`:

```go
WithBudgetProfile(poller.BudgetProfile{
    SourceUnits: map[poller.BudgetSource]float64{
        poller.BudgetSourceYouTubeScraper: videosWorstCaseRequestUnits(),
        poller.BudgetSourcePostgresWrite:  1,
    },
    BurstClass: poller.BudgetBurstPrimary,
    Priority:   poller.BudgetPriorityNormal,
})
```

`live`:

```go
WithBudgetProfile(poller.BudgetProfile{
    SourceUnits: map[poller.BudgetSource]float64{
        poller.BudgetSourceHolodexLive:   1,
        poller.BudgetSourcePostgresWrite: 1,
    },
    BurstClass: poller.BudgetBurstPrimary,
    Priority:   poller.BudgetPriorityHigh,
})
```

`live` fallback은 Holodex provider 내부에서 별도로 `BudgetSourceYouTubeScraper`를 debit합니다.

---

## D-002. active-active budget 계산은 sustained rate와 burst concurrency를 분리한다

### 결정

budget validator는 두 결과를 별도로 계산합니다.

```go
type BudgetEstimate struct {
    SustainedRPMBySource map[BudgetSource]float64
    BurstInflightBySource map[BudgetSource]int
}
```

`sustained_rpm`은 전체 channel identity와 interval로 계산합니다. AP 수는 곱하지 않습니다.

`burst_inflight`는 active-active AP 수와 AP별 worker count를 반영합니다.

```go
burstInflightUpperBound = activeAPCount * perAPWorkerCount
```

### 이유

active-active에서는 같은 identity가 전역 cooldown을 공유합니다. 따라서 steady-state request rate는 AP 수만큼 증가하지 않습니다.

하지만 worker count와 AP 수가 늘면 서로 다른 identity를 동시에 처리할 수 있으므로 burst concurrency는 늘어납니다.

따라서 운영 gate는 다음 두 조건을 모두 검사해야 합니다.

```text
sustained_rpm <= source별 허용 RPM
burst_inflight <= source별 허용 in-flight
```

### 코드 위치

새 validator는 다음 위치에 추가합니다.

```text
hololive/hololive-youtube-producer/internal/runtime/polling/budget_validator.go
```

기존 `validateYouTubeProducerPollerBudget`는 이 validator로 위임합니다.

---

## D-003. Valkey-backed `GlobalBudgetLimiter`를 scheduler admission gate로 추가한다

### 결정

source별 전역 admission과 in-flight budget을 Valkey로 관리합니다.

Phase 1에서 `GlobalBudgetLimiter`는 기존 request limiter를 대체하지 않습니다. 책임 분리는 다음과 같습니다.

| 계층 | 위치 | 책임 | Phase 1 변경 |
|---|---|---|---|
| `JobRunGuard` | scheduler claim 전후 | `(pollerName, channelID)` owner, lease, cooldown | 유지 |
| `GlobalBudgetLimiter` | scheduler claim 이후, poll 이전 | source별 admission, global in-flight, `BurstClass`, `Priority`, source cooldown | 추가 |
| `scraper.RateLimiter` | scraper HTTP request 직전 | local/distributed per-request pacing, bucket별 wait | 유지 |
| Holodex API distributed limiter | Holodex API request 직전 | Holodex path bucket별 request pacing | 유지 |
| startup budget validator | runtime bootstrap | sustained RPM, burst upper bound, config gate | source-aware로 확장 |

따라서 Phase 1 구현은 `scraper.RateLimiter`, Holodex distributed limiter, `ratelimit.SlidingWindowLimiter`를 제거하지 않습니다. `GlobalBudgetLimiter`가 긴 sleep을 수행해서 claim을 붙잡는 구조도 금지합니다. 허용 가능하면 즉시 reservation을 반환하고, 불가능하면 `BudgetDecision{Allowed:false, RetryAfter:...}`를 반환합니다.

추가할 인터페이스:

```go
package poller

type BudgetReservation interface {
    Commit(ctx context.Context) error
    Release(ctx context.Context) error
}

type GlobalBudgetLimiter interface {
    TryReserve(
        ctx context.Context,
        job BudgetJob,
        profile BudgetProfile,
        ttl time.Duration,
    ) (BudgetReservation, BudgetDecision, error)
}

type BudgetDecision struct {
    Allowed    bool
    RetryAfter time.Duration
    Reason     string
}

type BudgetJob struct {
    Namespace  string
    InstanceID string
    PollerName string
    ChannelID  string
    JobKey     string
}
```

`BudgetDecision.Allowed=false`는 정상 admission denial입니다. 이 경우 error를 반환하지 않습니다. error는 Valkey backend unavailable, Lua script parse failure, invalid config처럼 fail-closed 판단이 필요한 경우에만 반환합니다.

Valkey key는 source와 class별로 분리합니다.

```text
hololive:<namespace>:youtube-producer:budget:{youtube_scraper}:primary:inflight
hololive:<namespace>:youtube-producer:budget:{youtube_scraper}:backfill:inflight
hololive:<namespace>:youtube-producer:budget:{youtube_scraper}:fallback:inflight
hololive:<namespace>:youtube-producer:budget:{youtube_scraper}:global:inflight
hololive:<namespace>:youtube-producer:budget:{holodex_live}:primary:inflight
hololive:<namespace>:youtube-producer:budget:{reservation:<ownerToken>}
```

`tokens` 또는 `window` key는 Phase 1에서 startup validator가 통과한 sustained RPM을 runtime에서 추가 방어해야 할 때만 사용합니다. 기존 scraper/Holodex distributed limiter가 활성인 source에서 runtime token bucket을 켜려면 double debit을 피하기 위한 별도 migration decision이 필요합니다.

### 이유

현재 `JobRunGuard`는 job 중복 실행을 막지만, source별 outbound budget을 전역으로 제어하지 않습니다.

AP 3개가 각자 worker 2개를 가지면 최대 6개 job이 동시에 외부 요청을 만들 수 있습니다. worker count를 4로 늘리면 최대 12개입니다.

source-aware 전역 limiter가 있어야 다음이 가능합니다.

```text
Holodex live 요청은 전역 최대 N in-flight
YouTube scraper 요청은 전역 최대 M in-flight
browser snapshot은 전역 최대 K in-flight
backfill은 primary budget을 침범하지 않음
```

### `BudgetReservation` 생명주기

reservation은 terminal operation을 한 번만 적용하는 idempotent handle입니다.

```text
TryReserve allowed:
  reservation key 생성
  source/class/global inflight 증가
  optional window counter 증가
  BudgetReservation 반환

Commit:
  poll 성공 terminal 상태
  held inflight를 1회 감소
  reservation key 삭제
  이후 Release 호출은 no-op

Release:
  poll 미실행, poll 실패, context cancel, timeout terminal 상태
  held inflight를 1회 감소
  reservation key 삭제
  이후 Commit 호출은 no-op 또는 terminal conflict metric

reservation TTL 만료:
  worker crash나 process kill의 leak 방지
  다음 TryReserve script가 expired reservation을 청소하고 inflight를 보정
```

window counter를 사용하는 경우 `TryReserve` 시점에 소비한 admission unit은 기본적으로 refund하지 않습니다. Phase 1에서는 `max_inflight`, class hard cap, source cooldown이 runtime gate의 주된 목적입니다.

### 구현 방식

Valkey Lua script로 token bucket과 in-flight counter를 원자 처리합니다.

최소 기능은 다음입니다.

```text
입력:
  source
  units
  now_ms
  window_ms
  max_units_per_window (0이면 runtime window check disabled)
  max_inflight
  class_max_inflight
  global_max_inflight
  reservation_ttl_ms
  owner_token

동작:
  1. expired reservation 정리
  2. source cooldown 확인
  3. source/class/global in-flight 확인
  4. max_units_per_window > 0이면 source window counter 확인
  5. 초과면 retry_after와 reason 반환
  6. 허용이면 reservation key 기록
  7. source/class/global in-flight 증가
  8. window check가 켜져 있으면 window counter 증가

출력:
  allowed / retry_after / reason / reservation_token
```

초기 구현에서는 runtime window check를 비활성화하고 in-flight/class/source cooldown부터 적용합니다. 중요한 것은 source별 전역 admission과 기존 request limiter의 책임을 섞지 않는 것입니다.

---

## D-004. scheduler의 실행 순서를 `claim → renew start → budget reserve → poll`로 바꾼다

### 현재 문제

현재 scheduler는 claim을 얻은 뒤 rate limiter wait를 하고, 그 다음 poll context를 만들고 renew loop를 시작합니다.

즉 claim을 잡은 상태에서 wait가 길어지면 lease renew가 아직 시작되지 않을 수 있습니다.

### 결정

claim을 얻은 즉시 renew loop를 시작합니다. 그 다음 source budget을 reserve합니다. budget reserve가 denied이면 claim을 release하고 job을 `RetryAfter` 뒤로 reschedule합니다. backend error이면 claim을 release하고 readiness fail-closed 경로로 넘깁니다.

목표 순서는 다음입니다.

```text
1. JobRunGuard.TryClaim
2. acquired면 즉시 ClaimRenewLoop 시작
3. GlobalBudgetLimiter.TryReserve
4. budget denied면 claim.Release 후 budget retryAfter로 reschedule
5. local rate limiter wait
6. poll 실행
7. poll 성공이면 claim.MarkCompleted
8. poll 실패이면 claim.Release
9. budget reservation Commit 또는 Release
```

### 이유

budget reserve를 claim 전에 하면 여러 AP가 같은 job에 대해 동시에 budget을 잡고, 실제 claim은 한 AP만 성공할 수 있습니다. 그러면 budget token refund 문제가 생깁니다.

반대로 claim 후 budget reserve는 실제 실행 owner만 budget을 소비하므로 정확합니다. 단, claim을 잡고 오래 기다리면 peer가 불필요하게 막힐 수 있으므로 budget reserve는 짧은 timeout만 허용합니다.

### 새 config

```go
type SchedulerConfig struct {
    ...
    BudgetLimiter       GlobalBudgetLimiter
    BudgetAcquireTimeout time.Duration // default 3s
    ClaimLeaseSafetyMargin time.Duration // default 15s
    ClaimCompletionTimeout time.Duration // default 5s
}
```

`BudgetAcquireTimeout`은 길면 안 됩니다. 권장 기본값은 3초입니다. 최대 5초를 넘기지 않습니다. `GlobalBudgetLimiter`는 이 시간 안에 allowed/denied/error를 반환해야 하며, request limiter처럼 `RetryAfter`만큼 sleep하지 않습니다.

---

## D-005. lease TTL 계산식에 budget wait와 completion timeout을 포함한다

### 현재

현재 job claim lease TTL은 다음입니다.

```go
ttl := pollTimeout + 15*time.Second
if ttl < time.Minute {
    ttl = time.Minute
}
```

### 결정

새 계산식은 다음으로 변경합니다.

```go
func (s *Scheduler) jobClaimLeaseTTL() time.Duration {
    ttl := s.pollTimeout +
        s.budgetAcquireTimeout +
        s.claimCompletionTimeout +
        s.claimLeaseSafetyMargin

    if ttl < time.Minute {
        return time.Minute
    }
    return ttl
}
```

기본값:

```text
pollTimeout: existing config
budgetAcquireTimeout: 3s
claimCompletionTimeout: 5s
claimLeaseSafetyMargin: 15s
minimum: 1m
maximum: 없음
```

### 이유

claim 이후 poll 전에 budget wait가 추가되면 그 시간도 lease TTL 안에 들어갑니다.

기존 `pollTimeout + 15s`는 “바로 poll에 들어간다”는 가정에서는 충분합니다. 하지만 budget reserve, local rate limiter wait, completion marking time까지 고려하려면 TTL 계산에 포함해야 합니다.

maximum clamp는 추가하지 않습니다. runbook에서 이미 경고한 것처럼, clamp가 in-flight poll보다 짧으면 중복 작업을 만들 수 있습니다.

### 추가 metric

```text
youtube_poller_job_lease_ttl_seconds{poller}
youtube_poller_job_lease_elapsed_ratio{poller}
youtube_poller_job_lease_near_expiry_total{poller}
```

`lease_elapsed_ratio > 0.75`면 warn log를 남깁니다.

---

## D-006. Holodex live path는 batch 가능한 구조로 바꾼다

### 현재

현재 `LivePoller.Poll(ctx, channelID)`는 channel 하나씩 실행됩니다.

Holodex service는 `/users/live?channels=a,b,c`처럼 여러 channel을 받을 수 있지만, scheduler가 channel 단위로 job을 실행하므로 실제로는 단일 channel 호출이 많아질 수 있습니다.

### 결정

최종 목표는 scheduler에 optional batch poller를 도입하는 것입니다.

```go
type BatchPoller interface {
    Poller
    PollBatch(ctx context.Context, channelIDs []string) map[string]error
    MaxBatchSize() int
}
```

`LivePoller`는 `BatchPoller`를 구현합니다.

```go
func (p *LivePoller) PollBatch(ctx context.Context, channelIDs []string) map[string]error {
    streams, err := p.fetchLiveStreamsBatch(ctx, channelIDs)
    ...
}
```

Holodex provider가 있는 경우 `GetChannelsLiveStatus(ctx, channelIDs)`를 한 번 호출합니다.

provider가 없거나 fallback이 필요한 경우에는 channel별 scraper fallback을 사용하되, fallback에도 source budget을 적용합니다.

### `PollBatch` channel별 계약

`PollBatch`는 `Poll(ctx, channelID)`의 channel별 의미를 보존해야 합니다.

```text
입력 channelIDs:
  중복 제거된 channel list
  scheduler가 acquired claim만 넘긴다.

반환 map:
  입력 channelID마다 반드시 entry를 가진다.
  nil error는 해당 channel poll 성공이다.
  non-nil error는 해당 channel만 실패다.
  missing entry는 scheduler가 실패로 처리한다.

stream grouping:
  Holodex가 반환한 stream은 stream.ChannelID 기준으로 channel별 분리한다.
  ChannelID가 비어 있거나 요청 channel에 매핑할 수 없는 stream은 batch-level error가 아니라 해당 stream skip metric으로 처리한다.

baseline:
  기존 LivePoller의 channel별 baselinedChannels invariant를 유지한다.
  성공한 channel만 markBaselineComplete 한다.

ended session:
  markEndedSessions는 channel별 currentStreams만 받아야 한다.
  다른 channel의 batch 결과로 현재 channel의 session을 ended 처리하면 안 된다.

partial failure:
  한 channel 실패가 다른 channel의 MarkCompleted를 막지 않는다.
  batch-level Holodex 요청 실패 뒤 scraper fallback도 실패하면 모든 acquired channel을 실패 처리한다.
```

### scheduler 변경

scheduler는 due job을 pop할 때 같은 pollerName이고 batch 가능한 job들을 묶습니다.

```text
1. due jobs 중 live poller job을 최대 MaxBatchSize까지 모은다.
2. 각 channel job에 대해 JobRunGuard claim을 시도한다.
3. acquired job마다 즉시 renew loop를 시작한다.
4. peer_owned/already_completed는 batch에서 제외하고 claim skip retryAfter로 reschedule한다.
5. acquired channel만 batch input에 포함한다.
6. Holodex budget은 batch 단위로 reserve한다.
7. fallback scraper budget은 channel별 또는 fallback batch 단위로 별도 reserve한다.
8. PollBatch 호출
9. channel별 성공은 MarkCompleted
10. channel별 실패, renew lost, context cancel은 Release 후 error backoff
```

### 이유

Holodex `/users/live`는 batch에 적합합니다. channel별 worker 실행으로 Holodex를 여러 번 부르는 것보다, 같은 시점의 live jobs를 묶어 한 번에 요청하는 편이 좋습니다.

### 기본값

```text
YOUTUBE_PRODUCER_LIVE_BATCH_ENABLED=true
YOUTUBE_PRODUCER_LIVE_BATCH_MAX_CHANNELS=20
YOUTUBE_PRODUCER_LIVE_BATCH_WAIT_MS=100
```

`BATCH_WAIT_MS`는 너무 길면 detection latency가 늘어납니다. 기본 100ms로 시작합니다.

---

## D-007. backfill은 별도 budget class로 격리한다

### 결정

backfill poller는 primary poller와 별도 budget class를 사용합니다.

```go
BudgetBurstBackfill
BudgetPriorityLow
```

backfill source token bucket은 primary token bucket과 공유하지 않습니다. 다만 전체 source hard cap은 공유합니다.

권장 구조:

```text
youtube_scraper.primary.tokens
youtube_scraper.backfill.tokens
youtube_scraper.global.inflight
```

### 이유

backfill은 coverage 보조 수단입니다. primary live/shorts/community detection을 방해하면 안 됩니다.

현재 backfill poller는 별도 pollerName을 사용하므로 JobRunGuard cooldown도 별도입니다. 따라서 backfill을 켜면 요청량이 늘 수 있습니다.

backfill은 반드시 다음 조건에서만 켭니다.

```text
1. primary metrics에서 missed observation이 확인됨
2. request budget validator 통과
3. operator approval 존재
4. backfill metric dashboard 준비
```

### code gate

`SCRAPER_BACKFILL_ENABLED=true`일 때 startup에서 budget validator가 실패하면 runtime을 시작하지 않습니다.

---

## D-008. Holodex fallback은 별도 budget을 사용한다

### 결정

Holodex `/users/live` 실패 시 scraper fallback은 primary live budget이 아니라 fallback budget을 사용합니다.

```go
BudgetBurstFallback
BudgetPriorityHigh 또는 Normal
BudgetSourceYouTubeScraper
```

Holodex service 내부에 optional budget limiter를 주입합니다.

```go
type Service struct {
    ...
    budgetLimiter poller.GlobalBudgetLimiter
}
```

fallback 실행 전:

```go
reservation, decision, err := budgetLimiter.TryReserve(
    ctx,
    fallbackJob,
    BudgetProfile{
        SourceUnits: map[BudgetSource]float64{
            BudgetSourceYouTubeScraper: float64(len(channelIDs)),
        },
        BurstClass: BudgetBurstFallback,
        Priority: BudgetPriorityHigh,
    },
    30*time.Second,
)
```

### 이유

Holodex 장애 상황에서는 모든 live poll이 scraper fallback으로 몰릴 수 있습니다. 이때 fallback을 제한하지 않으면 Holodex 장애가 YouTube scraper/proxy 부하 폭증으로 전파됩니다.

fallback은 필요하지만, 반드시 budgeted fallback이어야 합니다.

---

## D-009. source별 cooldown을 scraper client와 연결한다

### 결정

scraper가 429, 403, transient cooldown, parser drift를 감지하면 source별 cooldown state를 Valkey에 기록합니다.

예시 key:

```text
hololive:<namespace>:youtube-producer:source-cooldown:{youtube_scraper}
hololive:<namespace>:youtube-producer:source-cooldown:{holodex_live}
hololive:<namespace>:youtube-producer:channel-cooldown:{youtube_scraper}:<channelHash>
```

### 동작

- 429: source-level cooldown
- 403: source-level 또는 route-level cooldown
- parser drift: fetcher engine/source별 cooldown
- channel unavailable/not found: channel-level cooldown
- transient transport: short cooldown

### 이유

현재 scraper는 error classification을 갖고 있습니다. 하지만 worker count가 늘어난 active-active 구조에서는 이 error classification을 전역 admission과 연결해야 합니다.

그렇지 않으면 한 AP가 429를 맞고 있어도 다른 AP들이 계속 같은 source에 요청할 수 있습니다.

---

## D-010. readiness는 budget backend unavailable에서만 fail-closed한다

### 결정

`GlobalBudgetLimiter`가 사용하는 Valkey backend 자체가 unavailable이면 active-active readiness를 fail-closed로 둡니다.

하지만 source token이 일시적으로 exhausted인 경우 `/ready`를 fail시키지 않습니다.

구분은 다음입니다.

```text
Valkey unavailable:
  /ready not_ready
  valkey_available=false
  budget_backend_available=false
  scraping_paused=true

source budget exhausted:
  /ready ready 유지
  budget_exhausted=true
  affected_sources=[...]
  scheduler는 해당 source job만 reschedule

source cooldown:
  /ready ready 유지
  source_cooldown=true
  affected_sources=[...]
  scheduler는 해당 source job만 cooldown retryAfter로 reschedule
```

구현은 기존 `readiness.State`에 budget 상태를 추가합니다. ready/not_ready 판단에는 다음만 영향을 줍니다.

```text
http_server_started
shutting_down
active-active lease/backend availability
global budget backend availability
```

`budget_exhausted`와 `source_cooldown`은 payload와 metric에만 노출하고 HTTP status를 바꾸지 않습니다.

### 이유

budget exhausted는 정상적인 admission control 상태입니다. 시스템 장애가 아닙니다.

반면 Valkey unavailable은 JobRunGuard와 global budget coordination 자체가 불가능한 상태입니다. 이 경우 중복 실행을 막기 위해 fail-closed가 맞습니다.

---

## D-011. metrics를 source-aware로 확장한다

### 추가 metric

```text
youtube_poller_budget_reserve_total{source,result,burst_class,priority}
youtube_poller_budget_reserve_wait_seconds{source}
youtube_poller_budget_retry_after_seconds{source}
youtube_poller_budget_inflight{source}
youtube_poller_budget_tokens_remaining{source}

youtube_poller_batch_size{poller}
youtube_poller_batch_duration_seconds{poller,result}
youtube_poller_batch_channel_result_total{poller,result}

youtube_poller_holodex_live_request_total{result}
youtube_poller_holodex_live_cache_total{result}
youtube_poller_holodex_live_fallback_total{result}

youtube_poller_source_cooldown_total{source,reason}
youtube_poller_source_cooldown_seconds{source}

youtube_poller_job_lease_elapsed_ratio{poller}
youtube_poller_job_lease_near_expiry_total{poller}
```

### 이유

기존 claim metric은 “누가 job을 잡았는가”를 보여줍니다.

하지만 다중 워커 최적화에서는 다음 질문도 답해야 합니다.

```text
어느 source가 병목인가?
budget 때문에 얼마나 skip/reschedule되는가?
live batch가 실제로 몇 channel씩 묶이는가?
Holodex cache hit이 충분한가?
fallback이 폭증하는가?
lease TTL이 부족한가?
```

---

## D-012. scheduler reschedule 정책을 claim skip과 budget skip으로 분리한다

### 결정

현재 `peer_owned`, `already_completed`는 claim skip입니다. 여기에 budget skip을 별도로 추가합니다.

```go
type JobSkipReason string

const (
    JobSkipPeerOwned        JobSkipReason = "peer_owned"
    JobSkipAlreadyCompleted JobSkipReason = "already_completed"
    JobSkipBudgetExhausted  JobSkipReason = "budget_exhausted"
    JobSkipSourceCooldown   JobSkipReason = "source_cooldown"
)
```

budget denied 시에는 `RetryAfter`를 사용합니다.

```go
func (s *Scheduler) rescheduleJobAfterBudgetSkip(job *Job, retryAfter time.Duration)
```

### 이유

claim skip과 budget skip은 의미가 다릅니다.

claim skip은 다른 AP가 이미 처리 중이거나 완료했다는 뜻입니다.
budget skip은 처리해야 할 job이 있지만 source 예산 때문에 미룬다는 뜻입니다.

둘을 같은 metric/log로 묶으면 운영자가 원인을 오해합니다.

---

## D-013. local worker count는 CPU/IO concurrency, global budget은 outbound safety로 역할을 나눈다

### 결정

`SCRAPER_SCHEDULER_WORKER_COUNT`는 계속 유지합니다. 하지만 의미를 명확히 바꿉니다.

```text
worker count:
  이 AP가 동시에 처리할 수 있는 job 실행 슬롯 수

global budget:
  전체 active-active cluster가 source별로 허용하는 outbound 요청량과 in-flight 수
```

worker count만으로 request safety를 보장하려 하지 않습니다. 반대로 global budget만으로 CPU/IO concurrency를 관리하려 하지 않습니다.

### 권장 기본값

현재 3 AP 기준:

```text
SCRAPER_SCHEDULER_WORKER_COUNT=2
global youtube_scraper max_inflight=4~6부터 시작
global holodex_live max_inflight=2~4부터 시작
browser_snapshot max_inflight=1
backfill max_inflight=1~2
```

정확한 수치는 운영 metric을 보고 조정합니다. 이 문서는 구조 결정서이며, 실제 숫자는 배포 전 24h baseline을 기준으로 결정해야 합니다.

---

## D-014. live batch 도입 전까지는 Holodex cache TTL과 fallback metric을 먼저 관측한다

### 결정

Phase 1에서는 scheduler batch를 바로 넣지 않아도 됩니다. 대신 다음을 먼저 넣습니다.

1. Holodex live cache hit/miss metric
2. Holodex fallback count metric
3. live poller source budget profile
4. source budget limiter
5. AP별 live acquired 분포 metric

그 후 Phase 2에서 `BatchPoller`를 도입합니다.

### 이유

scheduler batch는 변경 범위가 큽니다. claim, renew, mark completed를 batch 내 channel별로 처리해야 합니다. 잘못 구현하면 오히려 중복 실행이나 missed completion을 만들 수 있습니다.

따라서 안전한 순서는 다음입니다.

```text
Phase 1:
  source-aware budget + metric + lease TTL 안전화

Phase 2:
  live Holodex batch

Phase 3:
  tiered poller batch / backfill advanced policy
```

---

## 6. 구현 계획

## Phase 1. Source-aware budget과 lease 안전화

### 변경 파일

```text
hololive/hololive-shared/pkg/service/youtube/poller/internal/scheduler.go
hololive/hololive-shared/pkg/service/youtube/poller/internal/scheduler_worker.go
hololive/hololive-shared/pkg/providers/channel_poller_registration.go
hololive/hololive-youtube-producer/internal/runtime/polling/youtube_producer_poller_registrations.go
hololive/hololive-youtube-producer/internal/runtime/polling/budget_validator.go
hololive/hololive-youtube-producer/internal/runtime/polling/global_budget_limiter.go
hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/bootstrap_youtube_producer_youtube.go
```

### 작업

1. `BudgetSource`, `BudgetProfile`, `GlobalBudgetLimiter` 타입 추가
2. `ChannelPollerRegistration`에 `BudgetProfile` 추가
3. poller별 source budget profile 설정
4. scheduler에 `BudgetLimiter` 주입
5. claim 직후 renew loop 시작
6. budget reserve 실패 시 claim release 후 reschedule
7. lease TTL 계산식 변경
8. source-aware metrics 추가
9. budget validator 추가
10. unit test 추가

### 성공 기준

- active-active off일 때 기존 동작 유지
- active-active on일 때 JobRunGuard 동작 유지
- budget limiter nil이면 기존 동작 유지
- budget denied 시 poller가 실행되지 않음
- budget denied 시 claim은 release됨
- lease renew loop가 budget wait 중에도 동작함
- 기존 scheduler tests 통과
- 신규 budget tests 통과

---

## Phase 2. Holodex live batch 최적화

### 변경 파일

```text
hololive/hololive-shared/pkg/service/youtube/poller/internal/scheduler_batch.go
hololive/hololive-shared/pkg/service/youtube/poller/internal/pollers/live_poller.go
hololive/hololive-shared/pkg/service/holodex/internal/holodexprovider/service_channels_live.go
hololive/hololive-youtube-producer/internal/runtime/polling/youtube_producer_poller_registrations.go
```

### 작업

1. `BatchPoller` optional interface 추가
2. `LivePoller`에 `PollBatch` 구현
3. scheduler가 same poller due jobs를 micro-batch로 묶도록 변경
4. batch 내 channel별 claim/complete/release 처리
5. Holodex budget unit을 batch 기준으로 계산
6. fallback은 channel별 scraper budget으로 제한
7. batch size/duration/channel result metric 추가

### 성공 기준

- live batch size가 평균 1보다 커짐
- Holodex request count가 감소
- live detection latency가 허용 범위 내 유지
- batch 일부 실패 시 성공 channel만 MarkCompleted
- 실패 channel은 Release 후 error backoff
- JobRunGuard invariant 유지

---

## Phase 3. Backfill 격리와 source cooldown 전역화

### 변경 파일

```text
hololive/hololive-shared/pkg/service/youtube/scraper/internal/scraping/client.go
hololive/hololive-youtube-producer/internal/runtime/polling/source_cooldown.go
hololive/hololive-youtube-producer/internal/runtime/polling/youtube_producer_poller_registrations.go
```

### 작업

1. scraper error classification을 source cooldown writer와 연결
2. 429/403/parser drift/transport 별 cooldown 정책 구현
3. backfill source budget 별도 bucket 적용
4. backfill이 primary budget을 침범하지 않도록 validator 추가
5. source cooldown metric 추가

### 성공 기준

- 429 발생 시 전체 AP가 source cooldown을 인지
- cooldown 동안 해당 source job은 reschedule
- primary live/shorts/community는 backfill보다 우선권 유지
- backfill enabled 상태에서도 source hard cap 초과 없음

---

## 7. 핵심 코드 스케치

### 7.1 scheduler execute flow 목표

```go
func (s *Scheduler) executeJob(ctx context.Context, job *Job, workerID int) {
    decision := s.claimJobRun(ctx, job)
    if decision.err != nil {
        s.rescheduleJobAfterPoll(job, decision.err)
        return
    }
    if !decision.proceed {
        return
    }

    claimCtx, renewCancel, renewErrCh := s.startJobClaimRenewImmediately(ctx, job.Poller.Name(), decision.claim)
    defer renewCancel()

    reservation, budgetDecision, err := s.reserveJobBudget(claimCtx, job)
    if err != nil {
        s.releaseJobClaim(context.WithoutCancel(ctx), job, decision.claim)
        s.rescheduleJobAfterBudgetSkip(job, budgetDecision.RetryAfter)
        return
    }
    defer reservation.Release(context.WithoutCancel(ctx))

    if err := s.waitForJobRunSlot(claimCtx, job, decision); err != nil {
        reservation.Release(context.WithoutCancel(ctx))
        return
    }

    pollCtx, cancel := s.pollContext(claimCtx)
    defer cancel()

    start := time.Now()
    pollErr := job.Poller.Poll(pollCtx, job.ChannelID)
    elapsed := time.Since(start)

    if renewErr := drainJobClaimRenewError(renewErrCh); renewErr != nil && pollErr == nil {
        pollErr = renewErr
    }

    if pollErr == nil {
        reservation.Commit(context.WithoutCancel(ctx))
    }

    err = s.finishJobClaim(context.WithoutCancel(ctx), job, decision.claim, pollErr)
    status := s.logPollResult(job, workerID, pollCtx, elapsed, err)
    s.metrics.SchedulerPollDuration.WithLabelValues(job.Poller.Name(), status).Observe(elapsed.Seconds())
    s.rescheduleJobAfterPoll(job, err)
}
```

### 7.2 Budget profile registration 예시

```go
providers.NewChannelPollerRegistration(pollers.live, poller.PriorityHigh, poll.Live).
    WithChannelIDs(notificationChannelIDs).
    WithTargetGroup(providers.ChannelTargetGroupNotification).
    WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
    WithWorstCaseRequestUnitsPerRun(float64(scraper.FetchPageMaxAttempts)).
    WithBudgetProfile(poller.BudgetProfile{
        SourceUnits: map[poller.BudgetSource]float64{
            poller.BudgetSourceHolodexLive:   1,
            poller.BudgetSourcePostgresWrite: 1,
        },
        BurstClass: poller.BudgetBurstPrimary,
        Priority:   poller.BudgetPriorityHigh,
    })
```

### 7.3 Backfill profile 예시

```go
providers.NewChannelPollerRegistration(newNamedBackfillPoller("live_backfill", pollers.live), poller.PriorityLow, backfill.LiveInterval).
    WithChannelIDs(notificationChannelIDs).
    WithTargetGroup(providers.ChannelTargetGroupNotification).
    WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
    WithWorstCaseRequestUnitsPerRun(float64(scraper.FetchPageMaxAttempts)).
    WithBudgetProfile(poller.BudgetProfile{
        SourceUnits: map[poller.BudgetSource]float64{
            poller.BudgetSourceHolodexLive:   1,
            poller.BudgetSourcePostgresWrite: 1,
        },
        BurstClass: poller.BudgetBurstBackfill,
        Priority:   poller.BudgetPriorityLow,
    })
```

---

## 8. 테스트 결정사항

### 8.1 JobRunGuard invariant test

반드시 유지해야 할 테스트입니다.

```text
동일 namespace + poller + channel에서 동시에 TryClaim하면 하나만 acquired
나머지는 peer_owned
MarkCompleted 후 cooldown 동안 already_completed
cooldown 만료 후 다시 acquired 가능
namespace가 다르면 서로 독립
pollerName이 다르면 서로 독립
channelID가 다르면 서로 독립
```

### 8.2 Scheduler budget test

추가해야 할 테스트입니다.

```text
budget allowed면 Poll 실행
budget denied면 Poll 미실행
budget denied면 claim release 호출
budget denied면 reschedule retryAfter 반영
budget limiter error면 fail-closed 처리
claim renew loop가 budget wait 중에도 시작됨
lease renew lost면 poll cancel
```

### 8.3 Lease TTL test

```text
ttl = pollTimeout + budgetAcquireTimeout + completionTimeout + safetyMargin
ttl minimum 1m
ttl hard max 없음
pollTimeout 증가 시 ttl 증가
budgetAcquireTimeout 증가 시 ttl 증가
```

### 8.4 Live batch test

```text
batch 가능한 due live jobs를 묶음
acquired channel만 batch에 포함
peer_owned channel은 batch 제외
일부 channel 실패 시 실패 channel만 release
성공 channel은 MarkCompleted
Holodex provider는 channel list를 한 번만 받음
fallback은 source budget을 소비
```

### 8.5 Backfill isolation test

```text
backfill budget exhausted여도 primary live는 실행 가능
primary budget exhausted이면 primary만 reschedule
backfill enabled 시 validator가 budget 초과를 탐지
backfill pollerName이 primary pollerName과 다름
```

---

## 9. 운영 설정 결정사항

### 9.1 기본 worker count

초기값은 유지합니다.

```text
YOUTUBE_PRODUCER_AP_WORKER_COUNT=2
```

이 값은 AP당 worker count입니다.

3 AP 기준 potential local concurrency는 6입니다.

worker count를 늘리기 전 다음 metric을 확인합니다.

```text
youtube_poller_job_claim_total{result="acquired"}
youtube_poller_budget_reserve_total{result="denied"}
youtube_poller_budget_inflight
youtube_poller_holodex_live_fallback_total
youtube_poller_outbox_insert_total{result="conflict"}
PostgreSQL pool wait
Valkey latency
Holodex error/rate limit
scraper cooldown/rate limit
```

### 9.2 live interval

live interval을 줄이는 것은 Holodex와 fallback scraper에 직접 영향이 있습니다.

다음 조건 전에는 live interval을 줄이지 않습니다.

```text
Holodex live cache hit ratio 확인
fallback ratio 확인
global holodex_live budget 여유 확인
global youtube_scraper fallback budget 여유 확인
```

### 9.3 backfill

backfill은 기본 disabled 유지입니다.

```text
SCRAPER_BACKFILL_ENABLED=false
```

backfill enabled는 코드 배포가 아니라 운영 승인된 env/config 변경으로만 수행합니다.

---

## 10. 롤백 결정사항

각 phase는 독립 rollback 가능해야 합니다.

### Phase 1 rollback

```text
YOUTUBE_PRODUCER_GLOBAL_BUDGET_ENABLED=false
```

이 플래그가 false이면 scheduler는 budget limiter를 사용하지 않고 기존 JobRunGuard만 사용합니다.

### Phase 2 rollback

```text
YOUTUBE_PRODUCER_LIVE_BATCH_ENABLED=false
```

이 플래그가 false이면 `LivePoller.PollBatch`를 사용하지 않고 기존 channel 단위 `Poll`로 돌아갑니다.

### Phase 3 rollback

```text
YOUTUBE_PRODUCER_SOURCE_COOLDOWN_ENABLED=false
SCRAPER_BACKFILL_ENABLED=false
```

source cooldown 문제가 있으면 전역 cooldown을 끄고, backfill은 반드시 끕니다.

---

## 11. 최종 코드레벨 결론

현재 3-way active-active 구조는 이미 producer 수평 확장을 지원합니다. 하지만 현재 다중 워커 구조에 맞게 “완벽히 최적화”하려면 JobRunGuard만으로는 부족합니다.

최종적으로 필요한 것은 다음 네 가지입니다.

```text
1. JobRunGuard
   같은 poller+channel 중복 실행 방지

2. GlobalBudgetLimiter
   Holodex/scraper/proxy/browser/DB source별 전역 예산 제어

3. Lease TTL safety
   claim 이후 budget wait, poll, completion 시간을 모두 포함한 TTL 계산

4. Batch live polling
   Holodex /users/live를 channel 단위가 아니라 batch 단위로 사용
```

현재 구조를 유지하면서 가장 안전한 구현 순서는 다음입니다.

```text
Phase 1:
  source-aware budget + lease TTL safety + metrics

Phase 2:
  live Holodex batch

Phase 3:
  backfill isolation + source cooldown 전역화
```

이 순서가 안전한 이유는, active-active의 핵심 invariant인 “같은 job은 한 AP만 실행한다”를 깨지 않으면서, 외부 요청량과 worker 병렬성을 단계적으로 통제할 수 있기 때문입니다.

---

## 12. 최종 acceptance criteria

이 설계를 구현한 뒤 다음을 모두 만족해야 합니다.

```text
[ ] active-active off에서 기존 single-owner 동작 유지
[ ] active-active on에서 같은 poller+channel은 한 AP만 acquired
[ ] AP 수를 3개에서 4개로 늘려도 sustained_rpm 계산은 AP 수를 곱하지 않음
[ ] AP 수/worker count 증가 시 burst_inflight 계산은 증가함
[ ] source budget denied 시 poller는 실행되지 않음
[ ] source budget denied 시 claim은 release됨
[ ] lease renew loop는 budget wait 중에도 동작함
[ ] lease TTL은 pollTimeout + budgetAcquireTimeout + completionTimeout + safetyMargin
[ ] live batch enabled 시 Holodex 요청 수가 감소함
[ ] batch 일부 실패 시 channel별 MarkCompleted/Release가 정확함
[ ] backfill은 primary budget을 침범하지 않음
[ ] Holodex 실패 시 scraper fallback은 fallback budget을 사용함
[ ] Valkey unavailable 시 readiness fail-closed
[ ] source budget exhausted는 readiness failure가 아니라 scheduler admission 상태로 노출
[ ] metrics로 source별 병목을 판단할 수 있음
[ ] rollback flag로 Phase 1/2/3 각각 비활성 가능
```
