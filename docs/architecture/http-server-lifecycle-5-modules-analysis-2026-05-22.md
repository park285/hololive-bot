# 2026-05-22 — HTTP server lifecycle 5 모듈 차이 분석 (Phase 2.B.3 진입 전)

> Historical document (퇴역한 5-runtime 모듈 기준 기록). Do not use as the current source of truth. See `docs/current/PROJECT_MAP.md`.

본 문서는 5 모듈의 listen+shutdown+signal 패턴을 비교하고 hololive-shared 의 기존 라우터 helper (`httpserver.RuntimeRouterOptions`, `NewHealthOnlyRuntimeRouter`) 를 어디까지 확장하면 단일 helper 로 흡수 가능한지 평가한다. 본 문서는 decision 만, helper 구현은 별도 task.

## 1. 5 모듈 lifecycle 카탈로그

| 모듈 | listen | shutdown | signal | router helper | LOC |
|------|--------|----------|--------|---------------|-----|
| admin-api | `StartHTTPServer` 가 goroutine 에서 `server.ListenAndServe()` 실행, `http.ErrServerClosed` 무시 (`hololive/hololive-admin-api/internal/app/runtime/http_server.go:31`, `hololive/hololive-admin-api/internal/app/runtime/http_server.go:39`) | `ShutdownHTTPServer` 가 전달받은 shutdown ctx 로 `server.Shutdown(ctx)` 호출 후 wrap (`hololive/hololive-admin-api/internal/app/runtime/http_server.go:66`) | module-local `runtime.Run` 이 `shared-go/pkg/runtime/lifecycle.Run` 에 `SIGINT`, `SIGTERM` 기본 signal 처리를 위임 (`hololive/hololive-admin-api/internal/app/runtime/lifecycle.go:75`, `shared-go/pkg/runtime/lifecycle/skeleton.go:53`) | `NewRuntimeRouter` + `NewH2CServer` 사용 (`hololive/hololive-admin-api/internal/app/http/router.go:139`, `hololive/hololive-admin-api/internal/app/build_runtime.go:132`) | `http_server.go` 76 |
| kakao-bot | H2C `*http.Server` 와 HTTP/3 `*http3.Server` 를 각각 goroutine 으로 `ListenAndServe()` 실행 (`hololive/hololive-kakao-bot-go/internal/app/runtime/http_server.go:33`, `hololive/hololive-kakao-bot-go/internal/app/runtime/http_server.go:45`) | `ShutdownHTTPServer` / `ShutdownHTTP3Server` 를 `errors.Join` 으로 묶어 같은 ctx 로 종료 (`hololive/hololive-kakao-bot-go/internal/app/internal/botruntime/runtime_http_server.go:39`) | module-local `runtime.Run` 이 `shared-go/pkg/runtime/lifecycle.Run` 에 signal 처리를 위임 (`hololive/hololive-kakao-bot-go/internal/app/runtime/lifecycle.go:75`, `shared-go/pkg/runtime/lifecycle/skeleton.go:53`) | `NewRuntimeRouter`; H2C 는 `NewH2CServer`, H3 는 별도 `http3.Server` 구성 (`hololive/hololive-kakao-bot-go/internal/app/http/bot_router.go:43`, `hololive/hololive-kakao-bot-go/internal/app/bootstrap/bot_server.go:36`, `hololive/hololive-kakao-bot-go/internal/app/bootstrap/bot_server.go:62`) | `http_server.go` 102 |
| llm-sched | `startHTTPServer` 가 goroutine 에서 `r.httpServer.ListenAndServe()` 실행, `ErrServerClosed` 외 error 를 `errCh` 로 전달 (`hololive/hololive-llm-sched/internal/app/internal/runtime/bootstrap_llm_scheduler.go:97`) | scheduler stop 후 `r.httpServer.Shutdown(ctx)` 호출, error 를 `errors.Join` 으로 반환 (`hololive/hololive-llm-sched/internal/app/internal/runtime/bootstrap_llm_scheduler.go:170`) | runtime 이 직접 `shared-go/pkg/runtime/lifecycle.Run` 을 호출하고 `OnSignal` 에서 signal 로그 기록 (`hololive/hololive-llm-sched/internal/app/internal/runtime/bootstrap_llm_scheduler.go:68`, `hololive/hololive-llm-sched/internal/app/internal/runtime/bootstrap_llm_scheduler.go:80`) | `NewRuntimeRouter` + `NewH2CServer`; trigger route 조건부 등록 (`hololive/hololive-llm-sched/internal/app/internal/runtime/api_router.go:45`, `hololive/hololive-llm-sched/internal/app/internal/runtime/bootstrap_llm_scheduler.go:335`, `hololive/hololive-llm-sched/internal/app/internal/runtime/bootstrap_llm_scheduler.go:346`) | lifecycle in `bootstrap_llm_scheduler.go` 378 |
| alarm-worker | `StartHTTPServer` 가 goroutine 에서 `server.ListenAndServe()` 실행, `ErrServerClosed` 외 error 를 `errCh` 로 전달 (`hololive/hololive-alarm-worker/internal/app/runtime/http_server.go:31`, `hololive/hololive-alarm-worker/internal/app/runtime/http_server.go:39`) | `ShutdownHTTPServer` 가 같은 ctx 로 `server.Shutdown(ctx)` 호출 (`hololive/hololive-alarm-worker/internal/app/runtime/http_server.go:56`) | module-local `runtime.Run` 이 `shared-go/pkg/runtime/lifecycle.Run` 에 signal 처리를 위임 (`hololive/hololive-alarm-worker/internal/app/runtime/lifecycle.go:76`, `shared-go/pkg/runtime/lifecycle/skeleton.go:53`) | `NewRuntimeRouter` + `NewH2CServer` 사용 (`hololive/hololive-alarm-worker/internal/app/internal/workerapp/build_runtime.go:78`, `hololive/hololive-alarm-worker/internal/app/internal/workerapp/build_runtime.go:92`) | `http_server.go` 66 |
| youtube-producer | `startHTTPServer` 가 goroutine 에서 `r.HttpServer.ListenAndServe()` 실행, `ErrServerClosed` 외 error 를 `errCh` 로 전달 (`hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/youtube_producer_runtime_lifecycle.go:59`) | readiness stopping 표시, scheduler stop, `r.HttpServer.Shutdown(ctx)`, ingestion lease release 순서 (`hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/youtube_producer_runtime_lifecycle.go:71`, `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/youtube_producer_runtime_lifecycle.go:93`) | runtime 이 직접 `shared-go/pkg/runtime/lifecycle.Run` 을 호출하고 `OnSignal` 에서 readiness 를 stopping 으로 전환 (`hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/youtube_producer_runtime_runner.go:79`, `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/youtube_producer_runtime_runner.go:106`) | `NewHealthOnlyRuntimeRouter` + `NewH2CServer`; custom ready responder 사용 (`hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/bootstrap_youtube_producer.go:222`, `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/bootstrap_youtube_producer.go:232`) | lifecycle+runner 242 |

공통점:

- 기본 signal 은 `shared-go/pkg/runtime/lifecycle` 의 `SIGINT`, `SIGTERM` 이다 (`shared-go/pkg/runtime/lifecycle/skeleton.go:57`).
- shutdown timeout 은 5 모듈 모두 `constants.AppTimeout.Shutdown` 을 lifecycle option 으로 전달한다.
- listen 은 모두 `ListenAndServe()` 기반이며 정상 종료 error 인 `http.ErrServerClosed` 를 무시한다.
- server construction 은 대부분 `hololive-shared/pkg/server.NewH2CServer` 를 사용한다. kakao-bot 만 HTTP/3 server 를 별도로 추가한다.

차이점:

- admin-api, kakao-bot, alarm-worker 는 각 모듈 안에 거의 같은 `runtime/http_server.go` helper 를 복제한다.
- llm-sched 와 youtube-producer 는 runtime 파일 내부 method 로 HTTP server start/shutdown 을 직접 구현한다.
- youtube-producer 는 signal/error/shutdown 에 readiness 상태 전이를 결합한다.
- kakao-bot 은 `*http.Server` 와 `*http3.Server` 를 함께 종료해야 한다.
- llm-sched, alarm-worker, youtube-producer 는 HTTP server 외 background scheduler / lease / subscriber 를 같은 process lifecycle 에 포함한다.

## 2. hololive-shared 기존 helper

`hololive/hololive-shared/pkg/server/internal/httpserver/runtime_helpers.go` 가 제공하는 surface:

- `RuntimeRouterOptions` (`APIKey`, `EnableGzip`, `Operation`, `SkipLogPaths`, `PreRouteUse`, `RegisterRoutes`, `ReadyResponder`)
- `NewHealthOnlyRuntimeRouter(ctx, logger, apiKey, opts...)`
- `NewTriggerRuntimeRouter(ctx, logger, triggerHandler, apiKey, opts...)`
- `NewH2CServer(addr, handler, operation)`
- `NewRuntimeRouter(ctx, logger, opts)`

현재 helper 는 router/server construction 까지다. `NewRuntimeRouter` 는 Gin mode, trusted proxy, base middleware, `/health`, `/ready`, `/metrics`, caller route registration 을 담당한다. `NewH2CServer` 는 timeout 과 H2C wrapping 이 포함된 `*http.Server` 를 만든다. 하지만 goroutine start, `ListenAndServe`, error channel 전달, signal wait, graceful shutdown 은 담당하지 않는다.

결정: `hololive-shared/pkg/server/internal/httpserver` 는 router/H2C construction helper 로 유지한다. Phase 2.B.3 lifecycle helper 는 이 패키지를 확장하지 않는다.

이유:

- package path 가 `internal` 을 포함하므로 cross-module lifecycle helper 본거지로 쓰면 재사용 범위가 좁다.
- router helper 는 Gin, Prometheus, hololive health contract 에 의존한다.
- lifecycle helper 는 `net/http`, `context`, `log/slog`, `http.ErrServerClosed`, `shared-go/pkg/runtime/lifecycle` 만으로 표현 가능하며 hololive domain dependency 가 없다.

## 3. 통합 가능성

결정: (b) 부분 통합.

통합 가능한 부분:

- HTTP/1.1/H2C server start: goroutine 에서 `ListenAndServe()` 실행, `http.ErrServerClosed` 무시, 나머지 error 는 buffered `errCh` 로 전달하거나 logger 로 기록.
- HTTP server shutdown: `Shutdown(ctx)`, context 전달, wrap message 표준화. (helper 가 nil server 가드를 강제하지 않음 — 호출부가 server 생성 보장 또는 nil check 책임을 가짐.)
- `*http.Server` 와 `*http3.Server` 를 모두 수용하는 최소 interface:

```go
type Server interface {
    ListenAndServe() error
    Shutdown(context.Context) error
}
```

보존해야 하는 부분:

- process-level signal orchestration 은 이미 `shared-go/pkg/runtime/lifecycle.Run` 이 담당하므로 새 helper 가 다시 signal loop 를 소유하면 책임이 겹친다.
- shutdown 순서는 runtime 별 정책이다. youtube-producer 는 readiness marking 과 lease release 가 필요하고, llm-sched/alarm-worker 는 scheduler stop 이 필요하며, kakao-bot 은 webhook handler / bot shutdown 이 필요하다.
- kakao-bot HTTP/3 는 `http.ErrServerClosed` 와 `Shutdown(ctx)` semantics 는 맞지만 type 이 `*http.Server` 가 아니다.

옵션 trade-off:

| 옵션 | 평가 | 장점 | 단점 |
|------|------|------|------|
| (a) 가능 — 전체 `Run(ctx, opts)` 로 통합 | 비권장 | 호출부 한 줄화 가능 | signal/runtime/background shutdown 정책까지 HTTP helper 가 소유해 `lifecycle.Run` 과 중복, youtube readiness/lease 순서가 흐려짐 |
| (b) 부분 — HTTP start/shutdown 만 통합 | 권장 | 중복 LOC 제거, `http.ErrServerClosed` / error wrap 표준화, 기존 `lifecycle.Run` 과 조합 가능 | runtime 별 `Run` / shutdown hook 은 남음 |
| (c) 어려움 — 모듈별 보존 | 비권장 | diff 없음 | admin/kakao/alarm 의 복제 helper 유지, llm/youtube 직접 구현 유지로 Phase 2.B.3 목표 미달 |

## 4. 본거지 결정

cross-cutting helper doc 의 본거지 권장인 `shared-go/pkg/runtime/httpserver` 는 본 분석 후에도 유효하다.

결정:

- 신규 lifecycle helper 본거지: `shared-go/pkg/runtime/httpserver`
- 기존 router/helper 본거지: `hololive/hololive-shared/pkg/server/internal/httpserver`
- 관계: 공존. `shared-go` 는 lifecycle orchestration helper, `hololive-shared` 는 Gin router / H2C server construction helper.

근거:

- `shared-go/pkg/runtime/lifecycle` 가 이미 signal, runtime error, shutdown timeout 을 제공한다.
- HTTP server lifecycle helper 는 hololive-specific route, API key, Gin middleware, health payload 에 의존하지 않아도 된다.
- `hololive-shared/pkg/server/internal/httpserver` 를 대체하면 import 방향과 `internal` 경계가 나빠진다.

## 5. 시그니처 후보

권장 1차 시그니처:

```go
package httpserver

import (
    "context"
    "log/slog"
)

type Server interface {
    ListenAndServe() error
    Shutdown(context.Context) error
}

type Options struct {
    Server    Server
    Logger    *slog.Logger
    ErrorText string
}

func Start(server Server, logger *slog.Logger, errCh chan<- error)
func Shutdown(ctx context.Context, server Server, errorText string) error
```

또는 `Start`/`Shutdown` 을 하나의 option struct 로 묶는다.

```go
type LifecycleOptions struct {
    Server    Server
    Logger    *slog.Logger
    ErrorText string
    OnStarted func()
}

func Start(ctx context.Context, opts LifecycleOptions, errCh chan<- error)
func Shutdown(ctx context.Context, opts LifecycleOptions) error
```

보류한 시그니처:

```go
type LifecycleOptions struct {
    Addr            string
    Handler         http.Handler
    Logger          *slog.Logger
    ShutdownTimeout time.Duration
    OnStarted       func(addr string)
}

func Run(ctx context.Context, opts LifecycleOptions) error
```

이 형태는 helper 가 server construction 과 process signal lifecycle 을 함께 소유한다. 현재 5 모듈은 이미 server construction 을 `hololive-shared.NewH2CServer` 로 수행하고 process lifecycle 은 `shared-go/pkg/runtime/lifecycle.Run` 으로 수행한다. 따라서 Phase 2.B.3 의 최소 helper 로는 과하다. 단일 HTTP-only binary 에는 맞지만, 이 저장소의 5 runtime 은 HTTP server 외 background scheduler, bot, lease, subscriber 를 함께 다룬다.

확장 후보:

```go
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

이 형태는 cross-cutting helper doc 의 기존 후보와 호환된다. 다만 1차 마이그레이션에서는 `Start`/`Shutdown` 조합을 먼저 도입하고, HTTP-only runtime 이 생기거나 process-level 통합 이득이 확인될 때 `RunServerLifecycle` 로 확장하는 편이 안전하다.

## 6. 마이그레이션 step list

1. `shared-go/pkg/runtime/httpserver` 에 `Server` interface, `Start`, `Shutdown` helper 와 unit test 를 추가한다.
2. unit test 는 `http.ErrServerClosed` 무시, non-closed error 전달, nil `errCh` logger fallback, shutdown error wrap 을 고정한다. nil server / `*http3.Server` shape compatibility 는 helper 의 interface 계약(`ListenAndServe() error` + `Shutdown(ctx) error`)을 만족하는 호출부가 책임지며 helper 단위 테스트의 직접 대상이 아니다.
3. 가장 단순한 admin-api 의 `runtime/http_server.go` 부터 helper wrapper 로 교체한다.
4. alarm-worker 의 `runtime/http_server.go` 를 같은 helper wrapper 로 교체한다.
5. kakao-bot 은 `*http.Server` 와 `*http3.Server` 를 같은 interface helper 로 처리하고 기존 `errors.Join` shutdown semantics 를 보존한다.
6. llm-sched 는 `startHTTPServer` / `Shutdown` 안의 HTTP server start/shutdown 부분만 helper 로 축소하고 scheduler stop 순서는 유지한다.
7. youtube-producer 는 HTTP start/shutdown 부분만 helper 로 축소하고 readiness stopping, scheduler stop, lease release 순서는 유지한다.
8. 기존 `hololive-shared` router helper 는 보존한다. `RuntimeRouterOptions`, `NewRuntimeRouter`, `NewHealthOnlyRuntimeRouter`, `NewH2CServer` 는 construction 책임으로 유지한다.

## 7. 결론

Phase 2.B.3 은 `shared-go/pkg/runtime/httpserver` 에 HTTP server start/shutdown helper 를 신설하는 방향으로 진입한다.

통합 가능성은 (b) 부분 통합이다. 5 모듈의 listen/shutdown error handling 은 단일 helper 로 흡수 가능하지만, signal 이후 shutdown 순서와 background service 정리는 runtime 별 정책으로 남긴다. 기존 `shared-go/pkg/runtime/lifecycle.Run` 이 signal 과 shutdown timeout 을 이미 담당하므로 새 HTTP helper 가 process-level `Run(ctx, opts)` 를 다시 제공하는 것은 1차 범위에서 제외한다.

`hololive-shared/pkg/server/internal/httpserver` 는 router / H2C construction helper 로 계속 사용한다. 이를 lifecycle helper 본거지로 확장하지 않는다.
