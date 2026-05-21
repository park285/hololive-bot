# 2026-05-21 — Cross-cutting helper 시그니처 결정 (Phase 2.B 진입 가이드)

본 문서는 `2026-05-21-monorepo-refactor-master.md` 의 Phase 2.B 진입 전 cross-cutting helper 의 시그니처 / 본거지 / 마이그레이션 경로를 확정한다. 구현은 Phase 2.B.* 의 일이며, 본 문서는 결정만 한다.

## RunTickerLoop (Phase 2.B.1)

### 목적

`time.NewTicker` 생성, `defer ticker.Stop()`, `ctx.Done()` 종료, `onTick` 위임으로 반복되는 루프 패턴을 단일화한다. 초기 1회 실행, 별도 `stopCh`, 에러 채널 전파는 호출부 정책 차이가 크므로 Phase 2.B.1 첫 단계에서는 helper 바깥에 남긴다.

### 권장 시그니처

```go
func RunTickerLoop(ctx context.Context, interval time.Duration, onTick func(context.Context) error) error
```

결정: `shared-go/pkg/runtime/loop` 에 exported helper 로 둔다. cross-module 사용을 위해 exported 이름을 쓰되, 기존 파일 내부의 `runDispatcherTickerLoop` 같은 local wrapper 는 점진적으로 제거한다.

대안:

```go
func RunTickerLoop(ctx context.Context, interval time.Duration, runImmediately bool, onTick func(context.Context) error) error
```

`runImmediately` 는 호출부별 초기 실행 정책을 숨겨 diff 를 줄일 수 있으나, helper 의 책임이 커진다. Phase 2.B.1 에서는 호출부가 `RunTickerLoop` 호출 전 초기 실행을 명시하는 쪽을 우선한다.

### 본거지 후보

- 권장: `shared-go/pkg/runtime/loop` — ticker loop 는 Go runtime lifecycle 전반의 공통 패턴이며 현재 `shared-go/pkg/runtime/lifecycle` 와 책임 경계가 맞다.
- 보류: `hololive/hololive-shared/pkg/util` — hololive 전용 의존성이 없으므로 Phase 2.B 이후 재사용성이 낮아진다.

### 호출부 sample

- `hololive/hololive-shared/pkg/service/delivery/dispatcher.go:121` — `ticker := time.NewTicker(d.cfg.PollInterval)` 후 `waitNextDispatchTick` 로 `ctx.Done()` 처리.
- `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatcher.go:276` — local `runDispatcherTickerLoop(ctx, interval, before, onTick)` 가 이미 helper 형태로 존재.
- `hololive/hololive-admin-api/internal/server/internal/api/api_stats.go:126` — websocket stats stream 이 `systemStatsStreamInterval` ticker 로 주기 전송.
- `hololive/hololive-shared/pkg/service/holodex/internal/holodexprovider/photo_sync.go:82` — `ps.syncInterval` ticker 로 `syncWithRetry` 호출.
- `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/scheduler_worker.go:221` — job claim renew loop 가 ticker channel 을 step 함수에 전달.
- `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatcher_telemetry.go:15` — telemetry loop 가 초기 1회 실행 후 ticker 반복.
- `hololive/hololive-shared/pkg/service/youtube/internal/milestonescheduler/scheduler_batch.go:19` — scheduler ticker 를 struct field 로 보관하고 `stopCh` 도 함께 select.
- `hololive/hololive-llm-sched/internal/service/majorevent/scraper/maintenance_scheduler.go:119` — link check ticker 와 daily timer 를 같은 select 에서 다룸.

### 기존 helper 와의 호환성

`hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatcher.go:276` 의 `runDispatcherTickerLoop` 는 권장 시그니처와 가장 가깝지만 `before func()` 와 `onTick func()` 형태다. Phase 2.B.1 에서는 `before` 를 호출부의 명시적 선행 실행으로 분리하고, `onTick` 은 `func(context.Context) error` 로 바꿔 에러 반환이 필요한 호출부를 수용한다.

`maintenance_scheduler.go` 처럼 ticker 외에 timer / `stopCh` 를 함께 select 하는 루프는 1차 마이그레이션 대상에서 제외한다. helper 적용으로 제어 흐름이 흐려지는 호출부는 local loop 를 보존한다.

### Phase 2.B.1 진입 시 마이그레이션 list

- step 1: `shared-go/pkg/runtime/loop` 에 `RunTickerLoop` + ctx cancel / tick error / invalid interval 단위 테스트 추가.
- step 2: `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatcher.go:276`, `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatcher_telemetry.go:15`, `hololive/hololive-shared/pkg/service/holodex/internal/holodexprovider/photo_sync.go:82` 처럼 pure ticker loop 부터 교체.
- step 3: `hololive/hololive-shared/pkg/service/delivery/dispatcher.go:121`, `hololive/hololive-admin-api/internal/server/internal/api/api_stats.go:126`, `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/scheduler_worker.go:221` 로 확장하고 기존 테스트를 보존.
- step 4: `hololive/hololive-shared/pkg/service/youtube/internal/milestonescheduler/scheduler_batch.go:19`, `hololive/hololive-llm-sched/internal/service/majorevent/scraper/maintenance_scheduler.go:119` 는 `stopCh` / timer 병합 때문에 별도 검토 후 적용하거나 local loop 유지.

## NextExponentialBackoff (Phase 2.B.1)

### 목적

retry attempt 번호로 계산하는 backoff 와 현재 대기값을 다음 값으로 키우는 recovery loop backoff 가 섞여 있다. 두 입력 모델을 분리해 이름과 시그니처로 의도를 드러낸다.

### 권장 시그니처

```go
func NextExponentialBackoff(current, maxInterval, step time.Duration) time.Duration
```

결정: current-based helper 를 `shared-go/pkg/backoff` 에 둔다. `current <= 0` 이면 `step` 을 반환하고, 다음 값은 `current * 2` 를 기본으로 하되 `step` 을 하한, `maxInterval` 을 상한으로 삼는다.

attempt-based helper 는 별도 제공한다.

```go
func ComputeExponentialBackoff(attempt int, base, maxInterval, jitter time.Duration) time.Duration
```

결정: 하나의 함수로 통합하지 않는다. `ComputeBackoffDelay(attempt int, base, jitter time.Duration)` 와 `leaseBackoffDelay(attempt int, baseDelay, jitter time.Duration)` 는 attempt-based retry 이고, `nextRecoveryBackoff(current, maxInterval time.Duration)` 는 current-based recovery loop 이므로 호출부가 보유한 상태가 다르다.

### 본거지 후보

- 권장: `shared-go/pkg/backoff` — runtime loop, retry, lease renewal 이 모두 사용할 수 있고 hololive domain 의존성이 없다.
- 보류: `shared-go/pkg/runtime/loop` — ticker helper 와 함께 두면 편하지만 retry 계산은 runtime lifecycle 보다 넓은 유틸이다.

### 호출부 sample

- `hololive/hololive-shared/pkg/service/holodex/internal/holodexprovider/photo_sync.go:117` — `delay := retry.ComputeBackoffDelay(attempt-1, 5*time.Second, 2*time.Second)` 로 attempt-based backoff 를 사용.
- `hololive/hololive-shared/pkg/service/holodex/internal/holodexprovider/api_client_rate_limit.go:42` — rate limit retry 에서 `delay := retry.ComputeBackoffDelay(attempt, constants.RetryConfig.BaseDelay, constants.RetryConfig.Jitter)` 호출.
- `hololive/hololive-shared/internal/retry/retry.go:152` — `WithRetry` 내부가 `delay := ComputeBackoffDelay(attempt, opts.BaseDelay, opts.Jitter)` 로 helper 를 실제 호출.
- `hololive/hololive-youtube-producer/internal/runtime/ingestionlease/lease.go:276` — lease retry 가 `delay := leaseBackoffDelay(attempt, opts.BaseDelay, opts.Jitter)` 로 attempt-based backoff 를 호출.
- `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/readiness_recovery_loop.go:103` — recovery 실패 시 현재 `backoff` 로 대기하고 `nextRecoveryBackoff(backoff, maxInterval)` 로 다음 값을 계산.

위 목록은 실제 호출부만 인용한다. 기존 helper 정의 위치인 `hololive/hololive-shared/internal/retry/retry.go:52`, `hololive/hololive-youtube-producer/internal/runtime/ingestionlease/lease.go:283`, `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/readiness_recovery_loop.go:141` 은 호환성 / 마이그레이션 대상 정의로만 다룬다.

### 기존 helper 와의 호환성

`hololive/hololive-shared/internal/retry/retry.go:52` 의 `ComputeBackoffDelay` 는 `internal` 패키지라 다른 module 이 직접 재사용할 수 없다. Phase 2.B.1 에서는 `shared-go/pkg/backoff.ComputeExponentialBackoff` 로 동일 모델을 노출하고, `hololive-shared/internal/retry` 는 wrapper 또는 직접 호출로 전환한다.

`nextRecoveryBackoff` 의 `10s -> 30s` 점프는 일반 exponential helper 의 기본 동작이 아니다. Phase 2.B.1 에서 이 정책을 유지해야 한다면 `step=30*time.Second` 로 표현 가능한지 테스트로 확인하고, 표현이 불충분하면 recovery loop 에 local 정책 wrapper 를 남긴다.

### Phase 2.B.1 진입 시 마이그레이션 list

- step 1: `shared-go/pkg/backoff` 에 current-based `NextExponentialBackoff` 와 attempt-based `ComputeExponentialBackoff` 를 함께 추가하고 boundary case 테스트 작성.
- step 2: `hololive/hololive-shared/internal/retry/retry.go:52` 를 shared helper wrapper 로 바꿔 기존 `WithRetry` 테스트를 보존.
- step 3: `hololive/hololive-youtube-producer/internal/runtime/ingestionlease/lease.go:276` 와 `hololive/hololive-youtube-producer/internal/runtime/ingestionlease/lease.go:283` 의 별도 구현을 shared attempt-based helper 로 대체.
- step 4: `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/readiness_recovery_loop.go:103` / `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/readiness_recovery_loop.go:141` 은 current-based helper 로 대체하되 `10s -> 30s` 정책 보존 테스트를 먼저 작성.

## Lease/claim cache (Phase 2.B.2)

### 목적

claim token 생성, claim 결과 캐시, `CompareAndExpire` 기반 renew/release, retry-after/backoff 처리가 여러 위치에 흩어져 있다. 공통 claim lifecycle 은 추출하되 저장소별 의미 차이인 DB alarm state claim 과 Valkey lease claim 은 adapter 로 분리한다.

### 권장 시그니처

```go
type ClaimKey struct {
	Scope   string // 예: "youtube_outbox_delivery", "alarm_state"
	Subject string // 예: video_id, alarm_id
}

type ClaimStatus struct {
	Holder     string
	AcquiredAt time.Time
	ExpiresAt  time.Time
	RetryAfter time.Duration
}

type ReuseCache interface {
	Claim(ctx context.Context, key ClaimKey, holder string, ttl time.Duration) (ClaimStatus, error)
	Release(ctx context.Context, key ClaimKey, holder string) error
}

type ClaimStore interface {
	TryClaim(ctx context.Context, key ClaimKey, ownerToken string, leaseTTL time.Duration, cooldownTTL time.Duration) (ClaimStatus, error)
	Renew(ctx context.Context, key ClaimKey, ownerToken string, ttl time.Duration) (bool, error)
	Release(ctx context.Context, key ClaimKey, ownerToken string) (bool, error)
}

func ResolveClaim(ctx context.Context, cache ReuseCache, identity string, claim func(context.Context) (ClaimStatus, error)) (ClaimStatus, bool, error)
```

결정: 공통 helper 는 claim decision reuse 와 token lifecycle orchestration 까지만 맡고, Valkey Lua script / SQL upsert 는 adapter 가 맡는다. `bool` 반환값은 cache 재사용 여부다. `ClaimKey`, `ClaimStatus`, `ReuseCache` 의 세부 필드는 후속 brainstorming 에서 조정할 수 있으나, Phase 2.B.2 진입 전 최소 형태는 위 시그니처로 고정한다.

대안:

```go
func TryClaimWithCache(ctx context.Context, cache ReuseCache, identity string, ttl time.Duration, claim func(context.Context) (ClaimStatus, ClaimToken, error)) (ClaimStatus, ClaimToken, bool, error)
```

token 을 value 로 직접 반환하면 outbox 호출부 diff 는 줄지만 DB claim 처럼 token 이 `authorizedAt` 인 경우와 Valkey owner token 인 경우를 하나의 struct 로 뭉개게 된다. Phase 2.B.2 에서는 `ClaimStatus` 중심으로 adapter 를 두는 쪽을 권장한다.

### 본거지 후보

- 권장: `hololive/hololive-shared/pkg/service/cache/claim` — 기존 `SetNX`, `CompareAndExpire`, Valkey script helper 와 가까우며 youtube-producer 가 이미 `hololive-shared` 를 의존한다.
- 보류: `shared-go/pkg/cacheclaim` — pure interface 로는 가능하지만 실제 호출부는 `hololive-shared/pkg/service/cache` 의 계약과 Valkey Lua script 에 묶여 있어 adapter 가 과도해진다.

### 호출부 sample

- `hololive/hololive-youtube-producer/internal/runtime/ingestionlease/job_run_guard.go:137` — `TryClaim` 이 owner token 을 생성하고 lease / cooldown TTL 로 Lua claim 을 수행.
- `hololive/hololive-youtube-producer/internal/runtime/ingestionlease/job_run_guard.go:228` — `JobRunClaim.Renew` 가 owner token 으로 Lua renew 를 수행.
- `hololive/hololive-youtube-producer/internal/runtime/ingestionlease/lease.go:185` — ingestion lease renew 가 `CompareAndExpire(ctx, l.key, l.owner, l.ttl)` 로 ownership 을 확인.
- `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatcher_claim_gate.go:49` — `deliveryClaimReuseCache` 가 동일 identity 의 claim decision 을 batch 안에서 재사용.
- `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatcher_claim_gate.go:160` — `resolve` 가 cache miss 일 때만 `compute` claim 을 호출.
- `hololive/hololive-shared/pkg/service/youtube/tracking/internal/observation/alarm_state_repository_claim.go:19` — DB alarm state claim 이 `TryClaimAlarmState` 로 `authorized_at` 을 claim token 처럼 사용.
- `hololive/hololive-shared/pkg/service/youtube/tracking/internal/observation/alarm_state_repository_claim.go:98` — `ReleaseAlarmStateClaim` 이 `authorizedAt` 일치 조건으로 claim 을 해제.

### 기존 helper 와의 호환성

`hololive-shared/pkg/service/cache/interface.go` 의 `SetNX` / `CompareAndExpire` 계약은 유지한다. Phase 2.B.2 helper 는 cache client 를 대체하지 않고, claim lifecycle 을 반복 구현하는 얇은 orchestration 계층으로 둔다.

`dispatcher_claim_gate.go` 의 `deliveryClaimReuseCache` 는 가장 작은 추출 단위다. 다만 DB claim 은 성공 token 이 `authorizedAt` 이고 Valkey claim 은 `ownerToken` 이므로 token 타입을 하나로 강제하지 않는다.

### Phase 2.B.2 진입 시 마이그레이션 list

- step 1: 세 호출부의 TTL, retry-after, release 조건을 표로 고정하고 동작 보존 단언 테스트를 먼저 작성.
- step 2: `hololive-shared/pkg/service/cache/claim` 에 `ReuseCache`, `ClaimStatus`, `ResolveClaim` 을 추가.
- step 3: `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatcher_claim_gate.go:49` / `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatcher_claim_gate.go:160` 을 `ReuseCache` 로 교체.
- step 4: `hololive/hololive-youtube-producer/internal/runtime/ingestionlease/job_run_guard.go:137` / `hololive/hololive-youtube-producer/internal/runtime/ingestionlease/job_run_guard.go:228`, `hololive/hololive-youtube-producer/internal/runtime/ingestionlease/lease.go:185` 는 adapter 로 감싸되 Lua script 의미는 유지.
- step 5: `hololive/hololive-shared/pkg/service/youtube/tracking/internal/observation/alarm_state_repository_claim.go:19` / `hololive/hololive-shared/pkg/service/youtube/tracking/internal/observation/alarm_state_repository_claim.go:98` 은 DB adapter 로 감싸고 `authorized_at` 비교 semantics 를 테스트로 고정.

## HTTP server lifecycle helper (Phase 2.B.3)

### 목적

`ListenAndServe`, `http.ErrServerClosed` 무시, error channel 전파, `Shutdown(ctx)` wrap, signal 기반 shutdown 흐름이 여러 runtime 에 중복되어 있다. 이미 존재하는 `shared-go/pkg/runtime/lifecycle.Run` 을 중심으로 HTTP server start/shutdown helper 를 얇게 얹는다.

### 권장 시그니처

```go
type Server interface {
	ListenAndServe() error
	Shutdown(context.Context) error
}

type ServerLifecycleOptions struct {
	BaseContext     context.Context
	Server          Server
	Logger          *slog.Logger
	ShutdownTimeout time.Duration
	Operation       string
	OnStarted       func(context.Context)
	BeforeShutdown  func()
}

func RunServerLifecycle(opts ServerLifecycleOptions) error
```

결정: `Server` interface 로 `*http.Server` 와 `*http3.Server` 를 모두 수용한다. signal orchestration 은 새로 만들지 않고 `shared-go/pkg/runtime/lifecycle.Run` 을 내부에서 사용한다.

대안:

```go
func StartHTTPServer(server *http.Server, logger *slog.Logger, errCh chan<- error)
func ShutdownHTTPServer(ctx context.Context, server *http.Server, op string) error
```

이 대안은 admin-api / kakao-bot 의 현재 helper 와 가까워 diff 가 작지만 signal lifecycle 통합 효과가 약하다. Phase 2.B.3 에서는 `RunServerLifecycle` 을 권장하고, 큰 diff 가 생기는 모듈은 start/shutdown helper 를 중간 단계로 둔다.

### 본거지 후보

- 권장: `shared-go/pkg/runtime/httpserver` — `shared-go/pkg/runtime/lifecycle` 와 같은 module 에 두어 signal / shutdown helper 와 조합한다.
- 보조: `hololive/hololive-shared/pkg/server/internal/httpserver` — router / H2C 생성 helper 는 여기에 남긴다. lifecycle helper 의 본거지로 삼으면 `internal` 제약 때문에 admin-api 외 module 확장이 어렵다.

### 호출부 sample

- `shared-go/pkg/runtime/lifecycle/skeleton.go:25` — signal, runtime error, shutdown timeout 을 이미 `Run(opts Options)` 로 제공.
- `hololive/hololive-shared/pkg/server/internal/httpserver/runtime_helpers.go:74` — `NewH2CServer` 가 router 를 `*http.Server` 로 구성하지만 start/shutdown 은 담당하지 않음.
- `hololive/hololive-admin-api/internal/app/runtime/http_server.go:31` — `StartHTTPServer` 가 goroutine 으로 `ListenAndServe` 를 실행.
- `hololive/hololive-admin-api/internal/app/runtime/http_server.go:66` — `ShutdownHTTPServer` 가 `server.Shutdown(ctx)` 를 wrap.
- `hololive/hololive-kakao-bot-go/internal/app/runtime/http_server.go:45` — `StartHTTP3Server` 로 HTTP/3 server 도 별도 처리.
- `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/youtube_producer_runtime_runner.go:80` — runtime 전체는 `lifecycle.Run` 을 이미 사용.
- `hololive/hololive-llm-sched/internal/app/internal/runtime/bootstrap_llm_scheduler.go:69` — scheduler runtime 도 `lifecycle.Run` 으로 signal 과 shutdown 을 묶음.
- `hololive/hololive-llm-sched/internal/app/internal/runtime/bootstrap_llm_scheduler.go:97` — `startHTTPServer` 가 `ListenAndServe` error 를 `errCh` 로 전달.

### 기존 helper 와의 호환성

`hololive-shared/pkg/server/internal/httpserver/runtime_helpers.go` 는 router / server construction helper 로 유지한다. Phase 2.B.3 의 lifecycle helper 는 server construction 이 아니라 start / signal / shutdown orchestration 을 담당한다.

admin-api 와 kakao-bot 의 `StartHTTPServer` / `ShutdownHTTPServer` 는 권장 helper 로 흡수 가능하다. kakao-bot 의 HTTP/3 경로 때문에 concrete `*http.Server` 시그니처는 피한다.

### Phase 2.B.3 진입 시 마이그레이션 list

- step 1: `shared-go/pkg/runtime/httpserver` 에 `RunServerLifecycle` + `http.ErrServerClosed` 무시 / shutdown wrap / nil server 테스트 작성.
- step 2: `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/youtube_producer_runtime_runner.go:80` 과 `hololive/hololive-llm-sched/internal/app/internal/runtime/bootstrap_llm_scheduler.go:69` 처럼 이미 `lifecycle.Run` 을 쓰는 runtime 은 start/shutdown 부분만 helper 로 축소.
- step 3: `hololive/hololive-admin-api/internal/app/runtime/http_server.go:31` / `hololive/hololive-admin-api/internal/app/runtime/http_server.go:66` 과 `hololive/hololive-kakao-bot-go/internal/app/runtime/http_server.go:33` / `hololive/hololive-kakao-bot-go/internal/app/runtime/http_server.go:45` / `hololive/hololive-kakao-bot-go/internal/app/runtime/http_server.go:80` / `hololive/hololive-kakao-bot-go/internal/app/runtime/http_server.go:92` 를 새 helper 로 교체.
- step 4: `hololive/hololive-shared/pkg/server/internal/httpserver/runtime_helpers.go:74` 는 construction helper 로 남기고 import 방향을 유지.

## LogAndWrapError (Phase 2.B.4)

### 목적

`logger.Error(...)` 로 에러를 기록한 뒤 `fmt.Errorf("op: %w", err)` 로 wrap 하는 패턴을 줄인다. 동시에 `error_type`, `error_message`, `error_code`, `retryable` 같은 structured log key schema 를 보존한다.

### 권장 시그니처

```go
func LogAndWrapError(ctx context.Context, logger *slog.Logger, op string, err error, attrs ...slog.Attr) error
```

결정: `shared-go/pkg/logging` 에 둔다. 내부 동작은 `ErrorAttrs(err)` 를 추가하고 `Error(ctx, logger, op+".failed", op+" failed", attrs...)` 로 기록한 뒤 `fmt.Errorf("%s: %w", op, err)` 를 반환한다.

대안:

```go
func LogAndWrapError(ctx context.Context, logger *slog.Logger, event, message, op string, err error, attrs ...slog.Attr) error
```

기존 log message 와 event 를 더 정확히 보존할 수 있으나 호출부가 길어진다. alert/grep rule 이 message 텍스트에 묶인 구역에서는 이 대안을 wrapper 로 제한 적용한다.

### 본거지 후보

- 권장: `shared-go/pkg/logging` — 이미 `ErrorAttrs(err)`, `Error(ctx, logger, event, message, attrs...)`, `RunOperation` 이 있다.
- 보류: `hololive/hololive-shared/pkg/logschema` — YouTube service log schema 에 가까운 호출부에는 유용하지만 cross-runtime helper 로는 좁다.

호환성 검증: `hololive/hololive-shared/go.mod` 에 `github.com/park285/llm-kakao-bots/shared-go` 의존성이 이미 있음 (`grep "shared-go" hololive/hololive-shared/go.mod` 결과). 따라서 `hololive-shared` 측 1378 회 패턴이 `shared-go/pkg/logging` 의 `LogAndWrapError` 를 import 가능.

### 호출부 sample

- `hololive/hololive-shared/pkg/service/notification/internal/alarmservice/alarm_service_mutation.go:99` / `:101` — `as.logger.Error("Failed to add alarm", slog.Any("error", opErr))` 직후 `return 0, fmt.Errorf("rebuild add cache from repository: %w", opErr)` 로 wrap.
- `hololive/hololive-shared/pkg/service/notification/internal/alarmservice/alarm_service_mutation.go:160` / `:162` — persist 실패를 `as.logger.Error("Failed to persist alarm before cache write", slog.Any("error", err))` 로 기록한 뒤 `return fmt.Errorf("persist alarm before cache write: %w", err)` 반환.
- `hololive/hololive-shared/pkg/service/notification/internal/alarmservice/alarm_service_mutation.go:270` / `:272` — alarm removal persist 실패를 log 후 `return fmt.Errorf("delete alarm before cache removal: %w", err)` 로 wrap.
- `hololive/hololive-shared/pkg/service/notification/internal/alarmservice/alarm_service_mutation.go:367` / `:369` — room alarm clear persist 실패를 log 후 `return fmt.Errorf("delete room alarms before cache clear: %w", err)` 로 wrap.

### slog 컨벤션 침해 회피 검토

- log key schema 영향: 기존 호출부에는 `"error"` / `"err"` / `"error_message"` 가 섞여 있다. helper 는 새 표준 key 로 `ErrorAttrs(err)` 를 사용하고, 기존 alert/grep 이 `"error"` key 나 message text 에 묶인 위치는 Phase 2.B.4 첫 PR 에서 제외한다.
- 기존 attr 정책: `logging.ErrorAttrs` 가 이미 `error_type`, `error_message`, `error_code`, `retryable` 를 제공하므로 helper 내부에서 이를 호출한다. 호출부는 domain attrs 만 넘긴다.
- 인자 형태: `attrs ...slog.Attr` 를 권장한다. `...any` 는 기존 `logger.Error` 와 가까우나 구조화 key 검증이 약해져 `shared-go/pkg/logging` 컨벤션과 맞지 않는다.
- wrap 정책: 반환 에러는 `fmt.Errorf("%s: %w", op, err)` 로 고정한다. `op` 는 sentence message 가 아니라 stable operation name 으로 제한한다.

### 기존 helper 와의 호환성

`shared-go/pkg/logging.RunOperation` 은 operation 전체의 start/success/failure 를 감싸는 helper 이므로 즉시 대체 대상이 아니다. `LogAndWrapError` 는 단일 error branch 의 "log then wrap" 중복만 흡수한다.

기존 `logger.Error(..., slog.Any("error", err))` 를 즉시 전부 교체하면 log key 가 바뀐다. Phase 2.B.4 는 schema-preserving wrappers 또는 opt-in migration 으로 진행한다.

### Phase 2.B.4 진입 시 마이그레이션 list

- step 1: `shared-go/pkg/logging` 에 `LogAndWrapError` + nil logger / nil err / attr 보존 / wrap 테스트 추가.
- step 2: alert/grep rule 의존 여부를 조사하고 `"error"` key 유지가 필요한 호출부 목록을 제외한다.
- step 3: `hololive-shared` 내 낮은 위험의 `logger.Error + fmt.Errorf` 인접 branch 부터 교체.
- step 4: YouTube service log schema 가 있는 호출부는 `logschema` attrs 와 `ErrorAttrs` 의 key 충돌을 테스트로 확인한 뒤 적용.
- step 5: message text 기반 모니터링이 남은 경로는 대안 시그니처 wrapper 로 별도 PR 처리.

## Phase 2.B 진입 순서 권장

1. **2.B.1 RunTickerLoop / NextExponentialBackoff** 먼저 — 다른 helper 가 이를 사용할 가능성이 있고 변경 범위가 테스트로 고정하기 쉽다.
2. **2.B.2 claim cache** — 3개 구현체의 TTL / retry-after / token semantics 차이 분석 후 별도 brainstorming 으로 진입한다.
3. **2.B.3 HTTP server lifecycle** — 이미 `shared-go/pkg/runtime/lifecycle` 와 `hololive-shared/pkg/server/internal/httpserver` 에 기반 helper 가 있어 wrapping 작업 위주다.
4. **2.B.4 LogAndWrapError** — schema 영향이 가장 큼, 별도 PR 권장.

각 sub-phase 는 한 세션 = 한 PR.

## Self-review

- `RunTickerLoop` 는 pure ticker loop 만 1차 대상으로 제한해 timer / stopCh 와 섞인 호출부의 동작 변화를 피한다.
- `NextExponentialBackoff` 는 current-based 와 attempt-based 를 분리 제공하는 결정으로 `ComputeBackoffDelay` 와 `nextRecoveryBackoff` 의 입력 모델 차이를 보존한다.
- `Lease/claim cache` 는 token 타입을 강제하지 않고 adapter 경계를 둬 DB `authorized_at` claim 과 Valkey owner token claim 을 모두 수용한다.
- `HTTP server lifecycle helper` 는 `lifecycle.Run` 과 충돌하지 않고 그 위에 HTTP server start/shutdown 책임만 얹는다.
- `LogAndWrapError` 는 `slog.Attr` 와 `ErrorAttrs(err)` 를 사용해 structured logging schema 를 우선하며, message/key 기반 alert 경로는 별도 처리로 남긴다.
