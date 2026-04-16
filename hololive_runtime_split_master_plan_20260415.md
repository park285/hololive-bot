# hololive 구조 분리 + 잔여 리스크 종결 실행 플랜 (2026-04-15)

이 문서는 다음 두 입력을 합쳐 만든 **실행 문서**다.

1. 정적 재리뷰 결과에서 남은 리스크
2. `bot → admin-api 분리`, `bot → alarm-worker 분리`, `hololive-shared 축소`라는 구조 목표

핵심 결론은 단순하다.

- **먼저 안정화 패치**를 넣어야 한다.
- 그 다음 **프로세스 분리**를 해야 한다.
- 마지막에 **Go module / 소유권 분리**를 해야 한다.
- 반대로 하면, import graph 수술과 런타임 분리를 한 PR에 섞게 되어 실패 확률이 커진다.

즉, 이번 플랜의 기준은 “한 번에 예쁘게”가 아니라 “운영 리스크를 줄이면서도 구조를 실제로 분리하는 것”이다.

---

## 0. 최종 목표

최종 상태는 아래다.

### 런타임
- `hololive-bot`
  - `/webhook/iris`
  - command routing / message ingress
  - trigger internal route
  - 봇 전용 health / ready
- `hololive-admin-api`
  - `/api/holo/*`
  - `/api/auth/*`
  - `/oauth/callback`
  - `/internal/alarm/*` (control plane / future worker client seam)
- `hololive-alarm-worker`
  - YouTube/Chzzk/Twitch alarm checker loop
  - queue publish + dedup + mark
  - config subscriber (alarm advance minutes만)
  - health / metrics

### 코드 소유권
- `hololive-kakao-bot-go`
  - ingress / command / chat transport
- `hololive-admin-api`
  - admin HTTP + auth + settings/control plane
- `hololive-alarm-worker`
  - scheduler / checker / notifier
- `hololive-shared`
  - domain / contracts / small shared infra
- `hololive-stream-ingester`
  - YouTube ingestion / poller / scraper / outbox / tracking owner

---

## 1. 왜 이 순서가 맞는가

붙여주신 구조 방향은 정확하다. 우선순위는 `bot 안의 admin API`, `bot 안의 알림 스케줄러/체커`, `hololive-shared 안의 거대 공용 서비스층` 순으로 잡는 것이 맞다. 다만 **실행 순서**는 “서비스 이름부터 새로 만드는 것”이 아니라 “운영 폭발 반경을 줄이는 것” 기준으로 잡아야 한다. fileciteturn0file0

그 이유는 다음과 같다.

1. `admin-api`와 `alarm-worker`는 **실패 성격이 다르다**.
2. 둘 다 지금 `hololive-kakao-bot-go` 안에 있어 **webhook ingress를 같이 흔든다**.
3. 하지만 `alarm-worker`를 곧바로 새 Go module로 떼면 `internal/service/chzzk`, `internal/service/twitch`, `internal/service/alarm/*`, `internal/service/notification` 의존 때문에 **import graph surgery**까지 같이 해야 한다.
4. 따라서 **프로세스 분리**와 **module 분리**를 한 번에 하면 실패 확률이 높다.

그래서 이번 플랜은 아래 3층으로 간다.

- **Phase A**: 안정화 패치
- **Phase B**: 같은 Go module 안에서 runtime/binary 분리
- **Phase C**: 별도 Go module / 소유권 분리

이 순서가 가장 안전하다.

---

## 2. 머지 전략

이번 작업은 아래 7개 머지 단위로 끊는 것이 맞다.

1. `fix/alarm-deadline-and-backpressure`
2. `fix/auth-session-rotation-and-chzzk-client`
3. `feat/runtime-split-admin-api`
4. `feat/runtime-split-alarm-worker`
5. `refactor/bot-ingress-only`
6. `chore/compose-dashboard-routing`
7. `refactor/module-extraction-and-shared-ownership`

각 머지는 아래 조건을 만족해야 한다.

- 하나의 장애 도메인만 바꾼다.
- 테스트 범위가 명확하다.
- 롤백이 가능하다.
- 서비스 분리와 correctness fix를 같은 커밋에 섞지 않는다.

---

## 3. Phase A — 서비스 분리 전에 반드시 넣어야 할 안정화 패치

이 단계는 **선행 조건**이다.
이걸 건너뛰고 서비스를 떼면, 문제를 더 작은 프로세스에 “복제”만 하게 된다.

### A-1. `hololive-shared/pkg/service/alarm/targets.go`
### 문제
`context.WithoutCancel(ctx)`를 써서 shared query에서는 cancel뿐 아니라 deadline도 잃는다.
느린 DB 상황에서 singleflight shared query가 요청 생명주기보다 오래 붙잡힐 수 있다.

### 패치
```diff
diff --git a/hololive/hololive-shared/pkg/service/alarm/targets.go b/hololive/hololive-shared/pkg/service/alarm/targets.go
@@
 const emptyChannelSubscriberCacheTTL = 30 * time.Second
+const channelSubscriberLoadTimeout = 5 * time.Second

 var channelSubscriberLoadGroup singleflight.Group
+
+func withoutCancelPreserveDeadline(ctx context.Context, fallback time.Duration) (context.Context, context.CancelFunc) {
+    if ctx == nil {
+        return context.WithTimeout(context.Background(), fallback)
+    }
+
+    base := context.WithoutCancel(ctx)
+    if deadline, ok := ctx.Deadline(); ok {
+        return context.WithDeadline(base, deadline)
+    }
+
+    return context.WithTimeout(base, fallback)
+}
@@
 	resultCh := channelSubscriberLoadGroup.DoChan(normalizedChannelID, func() (any, error) {
-		queryCtx := context.Background()
-		if ctx != nil {
-			queryCtx = context.WithoutCancel(ctx)
-		}
+		queryCtx, cancel := withoutCancelPreserveDeadline(ctx, channelSubscriberLoadTimeout)
+		defer cancel()

 		var records []domain.Alarm
 		if err := db.WithContext(queryCtx).
 			Where("channel_id = ?", normalizedChannelID).
```

### 테스트
새 파일:
- `hololive/hololive-shared/pkg/service/alarm/targets_deadline_test.go`

테스트 항목:
- parent deadline 100ms
- DB mock이 300ms 지연
- deadline 초과 반환 확인
- same key singleflight shared result 유지 확인

---

### A-2. `hololive-kakao-bot-go/internal/bot/bot_message_async.go`
### 문제
worker pool이 reject하면 `go task()`로 우회한다.
즉, 부하가 높아질수록 backpressure가 해제된다.

### 패치
```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/bot/bot_message_async.go b/hololive/hololive-kakao-bot-go/internal/bot/bot_message_async.go
@@
-	if b.workerPool != nil {
-		submitErr := b.workerPool.Submit(task)
-		if submitErr == nil {
-			return
-		}
-
-		if b.logger != nil {
-			b.logger.Warn(
-				"Failed to submit async command task to worker pool; falling back to goroutine",
-				slog.String("command", commandType),
-				slog.Any("error", submitErr),
-			)
-		}
-	}
-
-	go task()
+	if b.workerPool == nil {
+		if b.logger != nil {
+			b.logger.Warn(
+				"Async command worker pool missing; running synchronously",
+				slog.String("command", commandType),
+			)
+		}
+		task()
+		return
+	}
+
+	if submitErr := b.workerPool.Submit(task); submitErr != nil {
+		if b.logger != nil {
+			b.logger.Warn(
+				"Async command rejected by worker pool",
+				slog.String("command", commandType),
+				slog.Any("error", submitErr),
+			)
+		}
+
+		cancel()
+
+		if chatID != "" {
+			notifyCtx, notifyCancel := context.WithTimeout(context.Background(), constants.RequestTimeout.WebhookProcessing)
+			defer notifyCancel()
+			if sendErr := b.sendError(notifyCtx, chatID, "요청이 많아 잠시 후 다시 시도해주세요."); sendErr != nil && b.logger != nil {
+				b.logger.Error("Failed to send async backpressure message", slog.Any("error", sendErr), slog.String("chat_id", chatID))
+			}
+		}
+		return
+	}
```

### 테스트
새 파일:
- `internal/bot/bot_message_async_backpressure_test.go`

필수 케이스:
- workerPool nil → 동기 실행
- workerPool reject → goroutine fallback 없음
- reject → 사용자 안내 메시지 best effort 발송

---

### A-3. `hololive-shared/pkg/service/auth/service.go`
### 문제
`createSession()`이 세션 키 생성 후 user-session index 반영 실패를 무시한다.
`Refresh()`도 기존 세션 무효화 실패를 무시한다.

이건 단순 캐시 경고가 아니라, 세션 rotate / password reset 후 전체 세션 폐기 의미를 흐린다.

### 패치 원칙
- session key 쓰기와 user-session index 반영을 **원자적으로 실패 처리**
- rotate 중 old session invalidation 실패 시 **new session rollback**
- invalidate helper를 공용화

### 권장 구조 변경
`createSession()`을 내부 helper 2개로 쪼갠다.

새 helper:
- `createSessionRecord(ctx, userID) (*Session, string, error)`  
  - return: session, sessionHash, error
- `trackSessionHash(ctx, userID, sessionHash string) error`
- `invalidateSession(ctx, userID, sessionHash string) error`
- `rollbackCreatedSession(ctx, userID, sessionHash string) error`

### diff
```diff
diff --git a/hololive/hololive-shared/pkg/service/auth/service.go b/hololive/hololive-shared/pkg/service/auth/service.go
@@
 import (
 	"context"
+	"errors"
 	stdErrors "errors"
 	"fmt"
@@
 func (s *Service) Refresh(ctx context.Context, token string) (*Session, error) {
@@
-	newSession, err := s.createSession(ctx, data.UserID)
+	newSession, newHash, err := s.createSessionRecord(ctx, data.UserID)
 	if err != nil {
 		return nil, err
 	}

-	// 기존 세션 무효화
-	_ = s.cacheSvc.Del(ctx, oldKey)
-	_, _ = s.cacheSvc.SRem(ctx, userSessionsKeyPrefix+data.UserID, []string{sessionHash})
+	if err := s.invalidateSession(ctx, data.UserID, sessionHash); err != nil {
+		rollbackErr := s.rollbackCreatedSession(ctx, data.UserID, newHash)
+		return nil, newError(
+			CodeInternal,
+			"failed to rotate session",
+			errors.Join(err, rollbackErr),
+		)
+	}

 	return newSession, nil
 }
@@
-func (s *Service) createSession(ctx context.Context, userID string) (*Session, error) {
+func (s *Service) createSession(ctx context.Context, userID string) (*Session, error) {
+	session, _, err := s.createSessionRecord(ctx, userID)
+	return session, err
+}
+
+func (s *Service) createSessionRecord(ctx context.Context, userID string) (*Session, string, error) {
 	if s.cacheSvc == nil {
-		return nil, newError(CodeInternal, "cache service not configured", nil)
+		return nil, "", newError(CodeInternal, "cache service not configured", nil)
 	}
@@
-			return nil, newError(CodeInternal, "failed to generate session token", err)
+			return nil, "", newError(CodeInternal, "failed to generate session token", err)
@@
-			return nil, newError(CodeInternal, "failed to store session", err)
+			return nil, "", newError(CodeInternal, "failed to store session", err)
 		}
@@
-		return nil, newError(CodeInternal, "failed to allocate unique session token", nil)
+		return nil, "", newError(CodeInternal, "failed to allocate unique session token", nil)
 	}

-	// 유저별 세션 인덱스 유지 (비밀번호 변경 시 전체 폐기 용도)
-	userSessionsKey := userSessionsKeyPrefix + userID
-	_, _ = s.cacheSvc.SAdd(ctx, userSessionsKey, []string{sessionHash})
-	_ = s.cacheSvc.Expire(ctx, userSessionsKey, s.cfg.UserSessionsTTL)
+	if err := s.trackSessionHash(ctx, userID, sessionHash); err != nil {
+		rollbackErr := s.rollbackCreatedSession(ctx, userID, sessionHash)
+		return nil, "", newError(
+			CodeInternal,
+			"failed to index session",
+			errors.Join(err, rollbackErr),
+		)
+	}

 	return &Session{
 		Token:     token,
 		ExpiresAt: expiresAt,
-	}, nil
+	}, sessionHash, nil
 }
+
+func (s *Service) trackSessionHash(ctx context.Context, userID, sessionHash string) error {
+	if userID == "" || sessionHash == "" {
+		return fmt.Errorf("track session hash: empty userID/sessionHash")
+	}
+	userSessionsKey := userSessionsKeyPrefix + userID
+	if _, err := s.cacheSvc.SAdd(ctx, userSessionsKey, []string{sessionHash}); err != nil {
+		return fmt.Errorf("track session hash: sadd: %w", err)
+	}
+	if err := s.cacheSvc.Expire(ctx, userSessionsKey, s.cfg.UserSessionsTTL); err != nil {
+		_, _ = s.cacheSvc.SRem(ctx, userSessionsKey, []string{sessionHash})
+		return fmt.Errorf("track session hash: expire: %w", err)
+	}
+	return nil
+}
+
+func (s *Service) invalidateSession(ctx context.Context, userID, sessionHash string) error {
+	var joined error
+	if err := s.cacheSvc.Del(ctx, sessionKeyPrefix+sessionHash); err != nil {
+		joined = errors.Join(joined, fmt.Errorf("invalidate session key: %w", err))
+	}
+	if _, err := s.cacheSvc.SRem(ctx, userSessionsKeyPrefix+userID, []string{sessionHash}); err != nil {
+		joined = errors.Join(joined, fmt.Errorf("invalidate session index: %w", err))
+	}
+	return joined
+}
+
+func (s *Service) rollbackCreatedSession(ctx context.Context, userID, sessionHash string) error {
+	return s.invalidateSession(ctx, userID, sessionHash)
+}
```

### 추가 후속
`incrWithTTL()`는 `INCR`와 `EXPIRE`를 분리 호출하지 말고 Lua 또는 단일 abstraction으로 바꾸는 것이 맞다.
이건 세션 correctness보다 우선순위는 낮지만, 같은 auth 패치 묶음에서 같이 처리해도 된다.

---

### A-4. `hololive-kakao-bot-go/internal/service/chzzk/client.go`
### 문제
- `httpClient == nil` 방어가 없다.
- `GetLiveStatus`, `GetScheduledLives`, `GetLives`, `GetChannels` 네트워크 플로우가 반복된다.
- logger 기본값은 이미 보강됐지만, public constructor로서 아직 덜 안전하다.

### 패치
#### 1) nil client 방어
```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/service/chzzk/client.go b/hololive/hololive-kakao-bot-go/internal/service/chzzk/client.go
@@
 import (
 	"context"
 	"errors"
 	"fmt"
 	"log/slog"
 	"net/http"
@@
 	"github.com/kapu/hololive-shared/pkg/constants"
+	sharedhttputil "github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
@@
 func NewClient(httpClient *http.Client, baseURL string, logger *slog.Logger) *Client {
+	if httpClient == nil {
+		httpClient = sharedhttputil.NewExternalAPIClient(constants.APIConfig.ChzzkTimeout)
+	}
 	return &Client{
 		httpClient:     httpClient,
@@
 func NewClientWithConfig(cfg ClientConfig) *Client {
@@
+	httpClient := cfg.HTTPClient
+	if httpClient == nil {
+		httpClient = sharedhttputil.NewExternalAPIClient(constants.APIConfig.ChzzkTimeout)
+	}
+
 	return &Client{
-		httpClient:     cfg.HTTPClient,
+		httpClient:     httpClient,
```

#### 2) 공통 request executor 추출
새 helper를 만든다.
```go
func (c *Client) doJSON(ctx context.Context, op string, reqURL string, out any) error
```

이 helper에서 다음을 한 번만 처리한다.
- request 생성
- `httpClient.Do`
- status code 처리
- body read limit
- JSON unmarshal
- circuit reset/failureCount update

그리고 각 public method는 URL/path만 조립하게 만든다.

### 테스트
- nil HTTP client일 때 panic 없이 동작하는지
- circuit open / 5xx / invalid JSON / read limit branch 유지되는지

---

### A-5. `hololive-shared/pkg/service/cache/service.go`
### 문제
sub-second TTL이 `int64(ttl.Seconds())`로 0초가 될 수 있다.
shared library에서 0초 TTL은 조용한 의미 오염이다.

### 패치
```diff
diff --git a/hololive/hololive-shared/pkg/service/cache/service.go b/hololive/hololive-shared/pkg/service/cache/service.go
@@
 import (
 	"context"
 	"encoding/json"
 	"fmt"
 	"log/slog"
+	"math"
 	"time"
@@
+func ttlSecondsCeil(ttl time.Duration) int64 {
+	if ttl <= 0 {
+		return 1
+	}
+	return int64(math.Ceil(ttl.Seconds()))
+}
@@
-	cmd := c.client.B().Set().Key(key).Value(string(jsonData)).ExSeconds(int64(ttl.Seconds())).Build()
+	cmd := c.client.B().Set().Key(key).Value(string(jsonData)).ExSeconds(ttlSecondsCeil(ttl)).Build()
```

### 테스트
- `100 * time.Millisecond` → `1s`
- `1500 * time.Millisecond` → `2s`

---

## 4. Phase B — 먼저 “같은 Go module 안에서” runtime/binary 분리

이 단계가 이번 플랜의 핵심이다.

여기서 중요한 결정은 하나다.

> **새 서비스는 만들되, 첫 컷에서는 새 Go module까지 만들지 않는다.**

이유:
- admin-api는 바로 새 module로 가도 비교적 쉽다.
- alarm-worker는 그렇지 않다.
- `internal/service/chzzk`, `internal/service/twitch`, `internal/service/alarm/*`, `internal/service/notification` 때문에 새 module extraction과 process split을 한 PR에 섞으면 너무 크다.

따라서 1차는 **같은 `hololive-kakao-bot-go` module 안에 새 binary**를 만든다.

### 새 바이너리
- `hololive/hololive-kakao-bot-go/cmd/admin-api/main.go`
- `hololive/hololive-kakao-bot-go/cmd/alarm-worker/main.go`

### 기존 바이너리 축소
- `cmd/bot`는 webhook ingress만 남긴다.

---

## 5. Phase B-1 — `admin-api` runtime 추가

### 5-1. 새 runtime 타입 추가
새 파일:
- `hololive/hololive-kakao-bot-go/internal/app/runtime_admin_api.go`
- `hololive/hololive-kakao-bot-go/internal/app/runtime_admin_api_runner.go`
- `hololive/hololive-kakao-bot-go/internal/app/runtime_admin_api_shutdown.go`

### 구현 형태
`BotRuntime`를 복붙하지 말고, 필요한 필드만 둔다.

```go
type AdminAPIRuntime struct {
    Config     *config.Config
    Logger     *slog.Logger
    ServerAddr string
    HttpServer *http.Server
    lifecycle.Managed
}
```

### builder 추가
새 파일:
- `hololive/hololive-kakao-bot-go/internal/app/build_admin_api_runtime.go`

```go
package app

import (
    "context"
    "fmt"
    "log/slog"
    "net/http"

    "github.com/kapu/hololive-shared/pkg/config"
    sharedserver "github.com/kapu/hololive-shared/pkg/server"
    "github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"

    apphttp "github.com/kapu/hololive-kakao-bot-go/internal/app/http"
    appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
    alarmsvc "github.com/kapu/hololive-shared/pkg/service/alarm"
)

func BuildAdminAPIRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*AdminAPIRuntime, error) {
    infra, err := appbootstrap.InitCoreInfrastructure(ctx, cfg, logger)
    if err != nil {
        return nil, err
    }

    runtimeViews := buildBotRuntimeDependencyViews(infra)

    adminDeps, err := buildBotAdminServerDependencies(ctx, cfg, runtimeViews.adminRuntime, nil, logger)
    if err != nil {
        infra.Cleanup()
        return nil, fmt.Errorf("build admin api runtime: admin deps: %w", err)
    }

    router, err := apphttp.ProvideAPIRouter(
        ctx,
        cfg,
        logger,
        adminDeps.DomainHandlers,
        adminDeps.AuthHandler,
        nil, // webhook 없음
        nil, // trigger 없음
        adminDeps.Cache,
    )
    if err != nil {
        infra.Cleanup()
        return nil, fmt.Errorf("build admin api runtime: router: %w", err)
    }

    // /internal/alarm/* 는 bot이 아니라 admin-api가 소유
    alarmAPI := alarmsvc.NewAPIHandler(runtimeViews.serverRuntime.alarmCRUD, logger)
    internalAlarm := router.Group("")
    internalAlarm.Use(middleware.APIKeyAuthMiddleware(cfg.Server.APIKey))
    alarmAPI.RegisterInternalRoutes(internalAlarm)

    server := sharedserver.NewH2CServer(fmt.Sprintf(":%d", cfg.Server.Port), router, "hololive-admin-api.http")

    rt := &AdminAPIRuntime{
        Config:     cfg,
        Logger:     logger,
        ServerAddr: fmt.Sprintf(":%d", cfg.Server.Port),
        HttpServer: server,
        Managed:    lifecycle.NewManaged(infra.Cleanup),
    }
    return rt, nil
}
```

> 주의: 위 코드에는 `middleware` import가 필요하다. `BuildBotServer`에서 alarm internal route 등록하던 로직을 여기로 옮긴다.

### runner / shutdown
`BotRuntime` helper를 재사용하되, bot/alarmScheduler/configSubscriber 훅 없이 HTTP만 다룬다.

```go
func (r *AdminAPIRuntime) Run() {
    appruntime.Run(r.Logger, r.Start, r.Shutdown)
}

func (r *AdminAPIRuntime) Start(ctx context.Context, errCh chan<- error) {
    if r == nil {
        return
    }
    appruntime.Start(ctx, errCh, appruntime.StartHooks{
        Logger:          r.Logger,
        ServerAddr:      r.ServerAddr,
        StartHTTPServer: r.StartHTTPServer,
    })
}

func (r *AdminAPIRuntime) StartHTTPServer(errCh chan<- error) {
    appruntime.StartHTTPServer(r.HttpServer, r.Logger, errCh)
}

func (r *AdminAPIRuntime) Shutdown(ctx context.Context) {
    if r == nil {
        return
    }
    if r.HttpServer != nil {
        _ = appruntime.ShutdownHTTPServer(r.HttpServer, ctx)
    }
}
```

---

### 5-2. `cmd/admin-api/main.go` 추가
```go
package main

import (
    "os"

    "github.com/kapu/hololive-shared/pkg/config"
    "github.com/kapu/hololive-shared/pkg/constants"
    "github.com/kapu/hololive-shared/pkg/health"
    sharedlogging "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
    "github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/automaxprocs"
    "github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/bootstrap"

    "github.com/kapu/hololive-kakao-bot-go/internal/app"
)

var Version = "dev"

func main() {
    os.Exit(bootstrap.Run(bootstrap.Options[*config.Config, *app.AdminAPIRuntime]{
        Version: Version,
        Initialize: func(version string) {
            automaxprocs.Init(nil)
            health.Init(version)
        },
        LoadConfig:             config.Load,
        LoadConfigErrorMessage: "Failed to load config",
        LoggerConfig: func(cfg *config.Config) sharedlogging.Config {
            return sharedlogging.Config{
                Dir:        cfg.Logging.Dir,
                MaxSizeMB:  cfg.Logging.MaxSizeMB,
                MaxBackups: cfg.Logging.MaxBackups,
                MaxAgeDays: cfg.Logging.MaxAgeDays,
                Compress:   cfg.Logging.Compress,
            }
        },
        LoggerFileName:    "admin-api.log",
        LoggerLevel:       func(cfg *config.Config) string { return cfg.Logging.Level },
        StartupMessage:    "Hololive Admin API starting...",
        BuildTimeout:      constants.AppTimeout.Build,
        BuildRuntime:      app.BuildAdminAPIRuntime,
        BuildErrorMessage: "Failed to assemble admin api runtime",
    }))
}
```

---

### 5-3. `cmd/bot`에서 admin 제거

현재 `buildBotRuntime()`는 webhook + admin + alarm scheduler + config subscriber를 다 같이 올린다.
이걸 ingress-only로 줄여야 한다.

#### 수정 파일
- `internal/app/bootstrap_bot_runtime_orchestration.go`

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_runtime_orchestration.go b/hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_runtime_orchestration.go
@@
 	webhookHandler, err := appbootstrap.BuildBotWebhookHandler(cfg, botBot, runtimeViews.webhook, logger)
 	if err != nil {
 		return nil, fmt.Errorf("build bot runtime: webhook handler: %w", err)
 	}

-	alarmScheduler, err := appbootstrap.BuildAlarmRuntimeScheduler(cfg, infra, logger)
-	if err != nil {
-		return nil, fmt.Errorf("build bot runtime: alarm runtime scheduler: %w", err)
-	}
-
-	configSubscriber := appbootstrap.BuildBotConfigSubscriber(ctx, runtimeViews.configSubscriber, runtimeViews.configSubscriberRuntime, nil, logger)
-
-	var adminServerDeps *botAdminServerDependencies
-
-	if cfg.Bot.AdminEnabled {
-		adminServerDeps, err = buildBotAdminServerDependencies(ctx, cfg, runtimeViews.adminRuntime, nil, logger)
-		if err != nil {
-			return nil, fmt.Errorf("build bot runtime: admin server dependencies: %w", err)
-		}
-	}
-
-	botServer, err := appbootstrap.BuildBotServer(ctx, cfg, webhookHandler, nil, runtimeViews.serverRuntime.alarmCRUD, adminServerDeps, logger)
+	botServer, err := appbootstrap.BuildBotServer(ctx, cfg, webhookHandler, nil, nil, nil, logger)
 	if err != nil {
 		return nil, err
 	}
@@
 		Logger:               logger,
 		Bot:                  botBot,
-		AlarmScheduler:       alarmScheduler,
-		ConfigSubscriber:     configSubscriber,
 		ServerAddr:           fmt.Sprintf(":%d", cfg.Server.Port),
 		HttpServer:           botServer,
 		webhookHandlerCloser: webhookHandler,
 	}, nil
 }
```

#### 의미
- bot은 더 이상 admin route를 조립하지 않는다.
- bot은 더 이상 alarm scheduler를 띄우지 않는다.
- bot은 더 이상 internal alarm API를 소유하지 않는다.
- bot은 ingress 전용으로 축소된다.

---

## 6. Phase B-2 — `alarm-worker` runtime 추가

### 6-1. `Notifier`에서 concrete `AlarmService` 의존 제거

이 단계가 핵심이다.

지금 `checker.Notifier`는 `*notification.AlarmService`에 직접 묶여 있다.
하지만 실제로 사용하는 메서드는 `MarkUpcomingEventNotified`뿐이고, 그것도 이미 `dedup.Service`에 같은 기능이 있다.

즉, 이 의존은 제거할 수 있다.

### 패치
```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/service/alarm/checker/notifier.go b/hololive/hololive-kakao-bot-go/internal/service/alarm/checker/notifier.go
@@
 	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
 	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
 	"github.com/kapu/hololive-shared/pkg/service/alarm/tier"
-
-	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
 )
@@
 type Notifier struct {
 	dedupSvc       *dedup.Service
 	queuePublisher *queue.Publisher
-	alarmSvc       *notification.AlarmService
 	tierScheduler  *tier.TieredScheduler
 	logger         *slog.Logger
 }
@@
 func NewNotifier(
 	dedupSvc *dedup.Service,
 	queuePublisher *queue.Publisher,
-	alarmSvc *notification.AlarmService,
 	tierScheduler *tier.TieredScheduler,
 	logger *slog.Logger,
 ) (*Notifier, error) {
@@
-	if alarmSvc == nil {
-		return nil, errors.New("new notifier: alarm service is nil")
-	}
-
 	return &Notifier{
 		dedupSvc:       dedupSvc,
 		queuePublisher: queuePublisher,
-		alarmSvc:       alarmSvc,
 		tierScheduler:  tierScheduler,
 		logger:         safeLogger(logger),
 	}, nil
 }
@@
-	if err := n.alarmSvc.MarkUpcomingEventNotified(
+	if err := n.dedupSvc.MarkUpcomingEventNotified(
 		ctx,
 		payload.notification.RoomID,
 		payload.channelID,
 		payload.notification.Stream,
 	); err != nil {
```

#### 관련 호출부 변경
```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/service/alarm/scheduler/runtime_scheduler.go b/hololive/hololive-kakao-bot-go/internal/service/alarm/scheduler/runtime_scheduler.go
@@
-	notifierSvc, err := checker.NewNotifier(dedupSvc, queuePublisher, alarmSvc, tierScheduler, logger)
+	notifierSvc, err := checker.NewNotifier(dedupSvc, queuePublisher, tierScheduler, logger)
```

이 변경으로 alarm-worker 분리 난도가 크게 내려간다.

---

### 6-2. alarm cache key 상수의 shared 승격

worker를 나중에 module로 분리하려면 현재 `notification` 내부 상수를 밖으로 빼야 한다.

#### 파일
- `hololive/hololive-shared/pkg/service/alarm/keys/keys.go`
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_types.go`
- `internal/service/alarm/checker/*.go`

#### shared keys 추가
```diff
diff --git a/hololive/hololive-shared/pkg/service/alarm/keys/keys.go b/hololive/hololive-shared/pkg/service/alarm/keys/keys.go
@@
 const (
 	AlarmKeyPrefix                    = "alarm:"
 	AlarmRegistryKey                  = "alarm:registry"
 	AlarmChannelRegistryKey           = "alarm:channel_registry"
+	ChzzkChannelMapKey                = "alarm:chzzk_channels"
+	TwitchLoginMapKey                 = "alarm:twitch_logins"
+	TwitchChannelLoginMapKey          = "alarm:twitch_channel_logins"
+	NextStreamKeyPrefix               = "alarm:next_stream:"
 	ChannelSubscribersKeyPrefix       = "alarm:channel_subscribers:"
@@
 	UpcomingEventKeyPrefix            = "notified:upcoming:event:"
 	ScheduleTransitionKeyPrefix       = "notified:schedule:transition:"
+	ChzzkLiveNotifiedKeyPrefix        = "notified:chzzk:live:"
+	IntegratedNotifiedKeyPrefix       = "notified:integrated:"
 )
```

#### notification 상수는 alias만 남김
```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/service/notification/alarm_types.go b/hololive/hololive-kakao-bot-go/internal/service/notification/alarm_types.go
@@
 	AlarmKeyPrefix              = sharedalarmkeys.AlarmKeyPrefix
 	AlarmRegistryKey            = sharedalarmkeys.AlarmRegistryKey
 	AlarmChannelRegistryKey     = sharedalarmkeys.AlarmChannelRegistryKey
 	ChannelSubscribersKeyPrefix = sharedalarmkeys.ChannelSubscribersKeyPrefix
-	ChzzkChannelMapKey          = "alarm:chzzk_channels"
-	TwitchLoginMapKey           = "alarm:twitch_logins"
-	TwitchChannelLoginMapKey    = "alarm:twitch_channel_logins"
+	ChzzkChannelMapKey          = sharedalarmkeys.ChzzkChannelMapKey
+	TwitchLoginMapKey           = sharedalarmkeys.TwitchLoginMapKey
+	TwitchChannelLoginMapKey    = sharedalarmkeys.TwitchChannelLoginMapKey
@@
-	NextStreamKeyPrefix         = "alarm:next_stream:"
+	NextStreamKeyPrefix         = sharedalarmkeys.NextStreamKeyPrefix
@@
-	ChzzkLiveNotifiedKeyPrefix  = "notified:chzzk:live:"
-	IntegratedNotifiedKeyPrefix = "notified:integrated:"
+	ChzzkLiveNotifiedKeyPrefix  = sharedalarmkeys.ChzzkLiveNotifiedKeyPrefix
+	IntegratedNotifiedKeyPrefix = sharedalarmkeys.IntegratedNotifiedKeyPrefix
```

#### checker는 notification 상수 대신 shared keys 사용
예:
```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/service/alarm/checker/chzzk_checker.go b/hololive/hololive-kakao-bot-go/internal/service/alarm/checker/chzzk_checker.go
@@
 	sharedconstants "github.com/kapu/hololive-shared/pkg/constants"
 	"github.com/kapu/hololive-shared/pkg/domain"
+	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
 	"github.com/kapu/hololive-shared/pkg/service/cache"
@@
-	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
 )
@@
-	channelMappings, err := c.cacheSvc.HGetAll(ctx, notification.ChzzkChannelMapKey)
+	channelMappings, err := c.cacheSvc.HGetAll(ctx, sharedalarmkeys.ChzzkChannelMapKey)
@@
-	return fmt.Sprintf("%s%s:%s", notification.ChzzkLiveNotifiedKeyPrefix, chzzkChannelID, bucket.Format("20060102T1504"))
+	return fmt.Sprintf("%s%s:%s", sharedalarmkeys.ChzzkLiveNotifiedKeyPrefix, chzzkChannelID, bucket.Format("20060102T1504"))
```

같은 방식으로
- `youtube_checker.go`
- `twitch_checker.go`
- `common.go`
의 notification 상수 참조를 제거한다.

---

### 6-3. `AlarmWorkerRuntime` 추가

#### 새 파일
- `internal/app/runtime_alarm_worker.go`
- `internal/app/runtime_alarm_worker_runner.go`
- `internal/app/runtime_alarm_worker_shutdown.go`
- `internal/app/build_alarm_worker_runtime.go`

#### 런타임 타입
```go
type AlarmWorkerRuntime struct {
    Config           *config.Config
    Logger           *slog.Logger
    Scheduler        runtimeAlarmScheduler
    ConfigSubscriber *configsub.Subscriber
    ServerAddr       string
    HttpServer       *http.Server

    schedulerMu     sync.Mutex
    schedulerCancel context.CancelFunc
    lifecycle.Managed
}
```

#### builder
```go
func BuildAlarmWorkerRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*AlarmWorkerRuntime, error) {
    infra, err := appbootstrap.InitCoreInfrastructure(ctx, cfg, logger)
    if err != nil {
        return nil, err
    }

    scheduler, err := appbootstrap.BuildAlarmRuntimeScheduler(cfg, infra, logger)
    if err != nil {
        infra.Cleanup()
        return nil, fmt.Errorf("build alarm worker runtime: scheduler: %w", err)
    }

    // alarm-worker 전용 config subscriber:
    // - alarm advance minutes만 반영
    // - settings.Update() 같은 persistence side effect는 하지 않음
    configSubscriber := BuildAlarmWorkerConfigSubscriber(ctx, infra, logger)

    router, err := sharedserver.NewRuntimeRouter(ctx, logger, sharedserver.RuntimeRouterOptions{
        APIKey: cfg.Server.APIKey,
    })
    if err != nil {
        infra.Cleanup()
        return nil, fmt.Errorf("build alarm worker runtime: router: %w", err)
    }

    server := sharedserver.NewH2CServer(fmt.Sprintf(":%d", cfg.Server.Port), router, "hololive-alarm-worker.http")

    rt := &AlarmWorkerRuntime{
        Config:           cfg,
        Logger:           logger,
        Scheduler:        scheduler,
        ConfigSubscriber: configSubscriber,
        ServerAddr:       fmt.Sprintf(":%d", cfg.Server.Port),
        HttpServer:       server,
        Managed:          lifecycle.NewManaged(infra.Cleanup),
    }
    return rt, nil
}
```

#### 전용 config subscriber 추가
새 파일:
- `internal/app/build_alarm_worker_config_subscriber.go`

```go
func BuildAlarmWorkerConfigSubscriber(
    ctx context.Context,
    infra *coreInfrastructure,
    logger *slog.Logger,
) *configsub.Subscriber {
    if infra == nil || infra.Deps == nil || infra.Deps.Cache == nil || infra.AlarmCRUD == nil {
        return nil
    }

    applyFn := configsub.NewApplyFn(logger, configsub.ApplyHandlers{
        AlarmAdvanceMinutes: func(payload contractssettings.AlarmAdvanceMinutesPayloadV1) {
            targets := infra.AlarmCRUD.UpdateAlarmAdvanceMinutes(ctx, payload.Minutes)
            logger.Info("Alarm worker applied alarm advance minutes via pub/sub",
                slog.Int("minutes", payload.Minutes),
                slog.Any("targets", targets),
            )
        },
    })

    return configsub.New(infra.Deps.Cache.GetClient(), applyFn, logger)
}
```

> 중요: 기존 `BuildBotConfigSubscriber`를 재사용하지 않는다.  
> 그 함수는 runtime apply + settings persistence를 같이 한다.  
> alarm-worker는 persistence owner가 아니다.

#### runner / shutdown
`BotRuntime`와 유사하되 bot/webhook은 없다.

```go
func (r *AlarmWorkerRuntime) Start(ctx context.Context, errCh chan<- error) {
    if r == nil {
        return
    }

    appruntime.Start(ctx, errCh, appruntime.StartHooks{
        Logger:     r.Logger,
        ServerAddr: r.ServerAddr,
        StartAlarmScheduler: func(ctx context.Context) {
            if r.Scheduler != nil {
                r.Scheduler.Start(ctx)
            }
        },
        RunConfigSubscriber: func(ctx context.Context) {
            if r.ConfigSubscriber != nil {
                r.ConfigSubscriber.Run(ctx)
            }
        },
        StartHTTPServer:         r.StartHTTPServer,
        SetAlarmSchedulerCancel: r.setSchedulerCancel,
    })
}
```

---

### 6-4. `cmd/alarm-worker/main.go` 추가
```go
package main

import (
    "os"

    "github.com/kapu/hololive-shared/pkg/config"
    "github.com/kapu/hololive-shared/pkg/constants"
    "github.com/kapu/hololive-shared/pkg/health"
    sharedlogging "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
    "github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/automaxprocs"
    "github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/bootstrap"

    "github.com/kapu/hololive-kakao-bot-go/internal/app"
)

var Version = "dev"

func main() {
    os.Exit(bootstrap.Run(bootstrap.Options[*config.Config, *app.AlarmWorkerRuntime]{
        Version: Version,
        Initialize: func(version string) {
            automaxprocs.Init(nil)
            health.Init(version)
        },
        LoadConfig:             config.Load,
        LoadConfigErrorMessage: "Failed to load config",
        LoggerConfig: func(cfg *config.Config) sharedlogging.Config {
            return sharedlogging.Config{
                Dir:        cfg.Logging.Dir,
                MaxSizeMB:  cfg.Logging.MaxSizeMB,
                MaxBackups: cfg.Logging.MaxBackups,
                MaxAgeDays: cfg.Logging.MaxAgeDays,
                Compress:   cfg.Logging.Compress,
            }
        },
        LoggerFileName:    "alarm-worker.log",
        LoggerLevel:       func(cfg *config.Config) string { return cfg.Logging.Level },
        StartupMessage:    "Hololive Alarm Worker starting...",
        BuildTimeout:      constants.AppTimeout.Build,
        BuildRuntime:      app.BuildAlarmWorkerRuntime,
        BuildErrorMessage: "Failed to assemble alarm worker runtime",
    }))
}
```

---

## 7. Phase B-3 — Dockerfile / Compose / Dashboard 변경

### 7-1. 새 Dockerfile 두 개 추가
현재 `hololive-kakao-bot-go/Dockerfile`는 `cmd/bot`만 빌드한다.
동일 패턴으로 2개 더 만든다.

#### `hololive/hololive-kakao-bot-go/Dockerfile.admin-api`
핵심만 다르다.

```diff
diff --git a/hololive/hololive-kakao-bot-go/Dockerfile.admin-api b/hololive/hololive-kakao-bot-go/Dockerfile.admin-api
new file mode 100644
@@
+ARG GO_VERSION=1.26.2
+ARG ALPINE_VERSION=3.23
+
+FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder
+ARG VERSION=dev
+RUN apk add --no-cache git
+ENV GOWORK=/workspace/hololive-bot/go.work
+WORKDIR /workspace/hololive-bot
+
+COPY go.mod go.work go.work.sum ./
+COPY --from=shared_go_workspace . ./shared-go
+COPY hololive/hololive-dispatcher-go ./hololive/hololive-dispatcher-go
+COPY hololive/hololive-llm-sched ./hololive/hololive-llm-sched
+COPY hololive/hololive-stream-ingester ./hololive/hololive-stream-ingester
+COPY hololive/hololive-shared ./hololive/hololive-shared
+COPY hololive/hololive-kakao-bot-go ./hololive/hololive-kakao-bot-go
+
+WORKDIR /workspace/hololive-bot/hololive/hololive-kakao-bot-go
+
+RUN \
+    --mount=type=cache,target=/go/pkg/mod \
+    --mount=type=cache,target=/root/.cache/go-build \
+    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOEXPERIMENT=greenteagc \
+    go build -tags sonic -trimpath -buildvcs=false -ldflags="-s -w -buildid= -X main.Version=${VERSION}" -o /dist/bin/admin-api ./cmd/admin-api && \
+    cp config.example.json official_japanese_names.json /dist/
+
+FROM alpine:${ALPINE_VERSION}
+RUN apk add --no-cache ca-certificates tini tzdata
+ENV TZ=Asia/Seoul
+RUN addgroup -g 1000 appuser && adduser -D -u 1000 -G appuser appuser
+WORKDIR /app
+COPY --from=builder --link --chown=1000:1000 /dist ./
+ENV GOGC=200 GOMEMLIMIT=256MiB GIN_MODE=release
+EXPOSE 30006
+HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
+    CMD wget -q --spider http://127.0.0.1:30006/health || exit 1
+USER appuser
+ENTRYPOINT ["/sbin/tini", "--"]
+CMD ["./bin/admin-api"]
```

#### `hololive/hololive-kakao-bot-go/Dockerfile.alarm-worker`
```diff
diff --git a/hololive/hololive-kakao-bot-go/Dockerfile.alarm-worker b/hololive/hololive-kakao-bot-go/Dockerfile.alarm-worker
new file mode 100644
@@
+... 동일 ...
+go build ... -o /dist/bin/alarm-worker ./cmd/alarm-worker
+...
+EXPOSE 30007
+HEALTHCHECK ... http://127.0.0.1:30007/health ...
+CMD ["./bin/alarm-worker"]
```

---

### 7-2. `docker-compose.prod.yml` 수정

#### 1) bot는 ingress only
```diff
diff --git a/docker-compose.prod.yml b/docker-compose.prod.yml
@@
   hololive-bot:
@@
-      BOT_ADMIN_ENABLED: "true"
+      BOT_ADMIN_ENABLED: "false"
       SERVER_PORT: 30001
@@
-      SERVICES_LLM_SCHEDULER_HEALTH_URL: http://host.docker.internal:30003/health
+      SERVICES_LLM_SCHEDULER_HEALTH_URL: http://host.docker.internal:30003/health
```

#### 2) admin-api 서비스 추가
```diff
@@
+  hololive-admin-api:
+    image: hololive-admin-api:prod
+    build:
+      context: .
+      dockerfile: hololive/hololive-kakao-bot-go/Dockerfile.admin-api
+      additional_contexts:
+        shared_go_workspace: ${SHARED_GO_WORKSPACE_PATH:-./shared-go}
+      args:
+        VERSION: ${HOLO_BOT_VERSION:-2.0.0}
+    container_name: hololive-admin-api
+    restart: always
+    env_file:
+      - ${COMPOSE_ENV_FILE:-./.env}
+    environment:
+      <<: *app-file-log-env
+      CACHE_SOCKET_PATH: /var/run/valkey/valkey-cache.sock
+      CACHE_HOST: valkey-cache
+      CACHE_PORT: 6379
+      POSTGRES_HOST: host.docker.internal
+      POSTGRES_PORT: "5433"
+      POSTGRES_SOCKET_PATH: ""
+      POSTGRES_DB: hololive
+      POSTGRES_USER: ${HOLOLIVE_DB_USER:-hololive_runtime}
+      POSTGRES_PASSWORD: ${DB_PASSWORD}
+      POSTGRES_SSLMODE: ${POSTGRES_SSLMODE:-require}
+      POSTGRES_QUERY_EXEC_MODE: ${POSTGRES_QUERY_EXEC_MODE:-exec}
+      POSTGRES_AUTO_PREPARE_SCHEMA: "false"
+      SERVER_PORT: 30006
+      LOG_DIR: /app/logs
+      GOMEMLIMIT: "320MiB"
+      APP_ENV: "production"
+      LLM_SCHEDULER_INTERNAL_URL: http://llm-scheduler:30003
+      SERVICES_LLM_SCHEDULER_HEALTH_URL: http://host.docker.internal:30003/health
+      SERVICES_GAME_BOT_TWENTYQ_HEALTH_URL: http://host.docker.internal:30081/health
+      SERVICES_GAME_BOT_TURTLE_HEALTH_URL: http://host.docker.internal:30082/health
+    ports:
+      - "127.0.0.1:30006:30006"
+    volumes:
+      - ./logs:/app/logs
+      - valkey-cache-socket:/var/run/valkey:ro
+    extra_hosts:
+      - "host.docker.internal:host-gateway"
+    depends_on:
+      holo-postgres:
+        condition: service_healthy
+      hololive-db-migrate:
+        condition: service_completed_successfully
+      valkey-cache:
+        condition: service_healthy
+    healthcheck:
+      test: [ "CMD-SHELL", "wget -q --spider -T 5 http://localhost:30006/health || exit 1" ]
+      interval: 30s
+      timeout: 5s
+      retries: 3
+    logging: *default-logging
+    <<: *security-hardening
+    networks:
+      hololive-net:
+        aliases:
+          - hololive-admin-api
```

#### 3) alarm-worker 서비스 추가
```diff
@@
+  hololive-alarm-worker:
+    image: hololive-alarm-worker:prod
+    build:
+      context: .
+      dockerfile: hololive/hololive-kakao-bot-go/Dockerfile.alarm-worker
+      additional_contexts:
+        shared_go_workspace: ${SHARED_GO_WORKSPACE_PATH:-./shared-go}
+      args:
+        VERSION: ${HOLO_BOT_VERSION:-2.0.0}
+    container_name: hololive-alarm-worker
+    restart: always
+    env_file:
+      - ${COMPOSE_ENV_FILE:-./.env}
+    environment:
+      <<: *app-file-log-env
+      CACHE_SOCKET_PATH: /var/run/valkey/valkey-cache.sock
+      CACHE_HOST: valkey-cache
+      CACHE_PORT: 6379
+      POSTGRES_HOST: host.docker.internal
+      POSTGRES_PORT: "5433"
+      POSTGRES_SOCKET_PATH: ""
+      POSTGRES_DB: hololive
+      POSTGRES_USER: ${HOLOLIVE_DB_USER:-hololive_runtime}
+      POSTGRES_PASSWORD: ${DB_PASSWORD}
+      POSTGRES_SSLMODE: ${POSTGRES_SSLMODE:-require}
+      POSTGRES_QUERY_EXEC_MODE: ${POSTGRES_QUERY_EXEC_MODE:-exec}
+      POSTGRES_AUTO_PREPARE_SCHEMA: "false"
+      SERVER_PORT: 30007
+      LOG_DIR: /app/logs
+      GOMEMLIMIT: "320MiB"
+      APP_ENV: "production"
+    ports:
+      - "127.0.0.1:30007:30007"
+    volumes:
+      - ./logs:/app/logs
+      - valkey-cache-socket:/var/run/valkey:ro
+    extra_hosts:
+      - "host.docker.internal:host-gateway"
+    depends_on:
+      holo-postgres:
+        condition: service_healthy
+      hololive-db-migrate:
+        condition: service_completed_successfully
+      valkey-cache:
+        condition: service_healthy
+    healthcheck:
+      test: [ "CMD-SHELL", "wget -q --spider -T 5 http://localhost:30007/health || exit 1" ]
+      interval: 30s
+      timeout: 5s
+      retries: 3
+    logging: *default-logging
+    <<: *security-hardening
+    networks:
+      hololive-net:
+        aliases:
+          - hololive-alarm-worker
```

#### 4) admin-dashboard가 더 이상 bot를 직접 치지 않게 변경
```diff
diff --git a/docker-compose.prod.yml b/docker-compose.prod.yml
@@
   admin-dashboard:
@@
-      HOLO_BOT_URL: http://hololive-kakao-bot-go:30001
+      HOLO_ADMIN_API_URL: http://hololive-admin-api:30006
+      HOLO_BOT_URL: http://hololive-admin-api:30006
@@
-      hololive-bot:
+      hololive-admin-api:
         condition: service_healthy
```

> `HOLO_BOT_URL`은 당장 하위 호환 alias로 남기고, backend code가 바뀐 뒤 삭제한다.

---

### 7-3. admin-dashboard backend config 변경

#### 파일
- `admin-dashboard/backend/src/config.rs`

현재는 `holo_bot_url`만 읽는다.
이를 `holo_admin_api_url`로 승격하고 기존 값은 alias로 둔다.

```diff
diff --git a/admin-dashboard/backend/src/config.rs b/admin-dashboard/backend/src/config.rs
@@
 pub struct Config {
-    pub holo_bot_url: String,
+    pub holo_admin_api_url: String,
     pub holo_bot_api_key: Option<String>,
@@
-            holo_bot_url: env_string("HOLO_BOT_URL", "http://hololive-kakao-bot-go:30001"),
+            holo_admin_api_url: optional_alias(&["HOLO_ADMIN_API_URL", "HOLO_BOT_URL"])
+                .unwrap_or_else(|| "http://hololive-admin-api:30006".to_string()),
             holo_bot_api_key: optional_alias(&["HOLO_BOT_API_KEY", "API_SECRET_KEY"])
```

#### 파일
- `admin-dashboard/backend/src/holo/client.rs`

```diff
diff --git a/admin-dashboard/backend/src/holo/client.rs b/admin-dashboard/backend/src/holo/client.rs
@@
-        let base_url = state.config.holo_bot_url.clone();
+        let base_url = state.config.holo_admin_api_url.clone();
```

이 패치 후 dashboard는 control plane만 친다.
즉, dashboard 트래픽이 ingress bot에 영향을 주지 않는다.

---

## 8. Phase B-4 — 프로젝트 맵과 문서 갱신

### 수정 파일
- `docs/current/PROJECT_MAP.md`
- `docs/current/README.md`
- `scripts/architecture/check-project-map.sh` 결과 확인
- 필요 시 `.github/workflows/*` build matrix

### `PROJECT_MAP.md` diff
```diff
diff --git a/docs/current/PROJECT_MAP.md b/docs/current/PROJECT_MAP.md
@@
-| `hololive-kakao-bot-go` | Go 1.26.2 | `hololive/hololive-kakao-bot-go/` | Main bot (webhook + command routing + admin API) | 30001 |
+| `hololive-kakao-bot-go` | Go 1.26.2 | `hololive/hololive-kakao-bot-go/` | Main bot ingress (webhook + command routing) | 30001 |
+| `hololive-admin-api` | Go 1.26.2 | `hololive/hololive-kakao-bot-go/` | Admin HTTP control plane | 30006 |
+| `hololive-alarm-worker` | Go 1.26.2 | `hololive/hololive-kakao-bot-go/` | Alarm checker / queue publisher worker | 30007 |
```

---

## 9. Phase C — 그 다음에 Go module 분리

여기서부터가 진짜 “새 모듈”이다.
이 단계는 **Phase B가 배포되어 안정화된 뒤** 진행해야 한다.

### 결정
- `admin-api`는 먼저 module로 떼도 된다.
- `alarm-worker`는 먼저 내부 패키지 의존을 걷어낸 뒤 떼야 한다.

---

## 10. Phase C-1 — `hololive-admin-api` module 추출

이건 비교적 단순하다.
왜냐하면 admin API는 주로 다음 묶음만 옮기면 되기 때문이다.

### 이동 대상
- `hololive/hololive-kakao-bot-go/internal/server/*`
- `hololive/hololive-kakao-bot-go/internal/app/http/*`
- `hololive/hololive-kakao-bot-go/internal/service/system/*`
- `hololive/hololive-kakao-bot-go/internal/service/trigger/*`
- `hololive/hololive-kakao-bot-go/cmd/admin-api/*`
- admin runtime builder

### 새 경로
```text
hololive/
  hololive-admin-api/
    go.mod
    cmd/admin-api/main.go
    internal/app
    internal/http
    internal/server
    internal/service/system
    internal/service/trigger
```

### `go.work` diff
```diff
diff --git a/go.work b/go.work
@@
 use (
 	./
 	./hololive/hololive-dispatcher-go
+	./hololive/hololive-admin-api
 	./hololive/hololive-kakao-bot-go
 	./hololive/hololive-llm-sched
 	./hololive/hololive-shared
 	./hololive/hololive-stream-ingester
 	./shared-go
 )
```

### 새 `go.mod`
```go
module github.com/kapu/hololive-admin-api

go 1.26.2

require (
    github.com/gin-contrib/cors v1.7.7
    github.com/gin-gonic/gin v1.12.0
    github.com/kapu/hololive-shared v0.0.0
    github.com/park285/llm-kakao-bots/shared-go v0.1.2
    github.com/stretchr/testify v1.11.1
)

replace github.com/kapu/hololive-shared => ../hololive-shared
replace github.com/park285/llm-kakao-bots/shared-go => ../../shared-go
```

### import path rewrite 예시
```diff
diff --git a/hololive/hololive-admin-api/internal/app/build_runtime.go b/hololive/hololive-admin-api/internal/app/build_runtime.go
@@
-	"github.com/kapu/hololive-kakao-bot-go/internal/server"
+	"github.com/kapu/hololive-admin-api/internal/server"
```

### bot module에서 제거
- `internal/server/*` 삭제
- `internal/app/http/*` 삭제
- `internal/app/api_router.go` 삭제
- bot에서 admin 관련 dependency view 제거

---

## 11. Phase C-2 — `hololive-alarm-worker` module 추출

이 단계는 `admin-api`보다 어렵다.
이유는 현재 worker 코드가 bot module 내부 라이브러리에 기대고 있기 때문이다.

### module extraction 전 선행 조건
다음 세 조건이 먼저 만족돼야 한다.

1. `checker.Notifier`가 더 이상 `*notification.AlarmService`에 의존하지 않는다.  
   → Phase B에서 해결
2. checker/scheduler가 `notification` 내부 상수에 직접 기대지 않는다.  
   → shared keys 승격으로 해결
3. `chzzk`, `twitch` client import 경계가 정리된다.

### 여기서 선택지
#### 선택지 A — 가장 안전
`hololive-alarm-worker`를 새 module로 만들되, `chzzk` / `twitch` client를 `hololive-kakao-bot-go`에 남기지 말고 **새 공용 module 또는 hololive-shared의 작은 패키지**로 이동

#### 선택지 B — 더 빠르지만 덜 예쁨
alarm-worker는 같은 `hololive-kakao-bot-go` module에 영구적으로 남기고, binary만 분리 유지

이번 저장소 상황에서는 **선택지 B를 먼저 운영에 적용**하고, 그 다음 선택지 A로 가는 것이 현실적이다.

### 권장 최종안
- `chzzk` → `hololive/hololive-platform-clients/chzzk` 같은 새 module을 만들지 말고,
- 우선 `hololive-kakao-bot-go/internal/service/chzzk` 와 `.../twitch` 를 **`hololive-shared/pkg/platform/chzzk` / `pkg/platform/twitch`로 옮기는 작은 PR**을 따로 낸다.

이건 shared를 키우는 선택처럼 보이지만, `alarm-worker`와 `bot` 두 런타임이 같이 쓰는 thin client라면 허용 가능하다.
다만 여기서 service/business logic를 shared로 옮기면 안 된다.

### 구체 move map
```text
move:
  hololive/hololive-kakao-bot-go/internal/service/chzzk/*
  -> hololive/hololive-shared/pkg/platform/chzzk/*

move:
  hololive/hololive-kakao-bot-go/internal/service/twitch/*
  -> hololive/hololive-shared/pkg/platform/twitch/*
```

### import rewrite 예시
```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/bot/bot.go b/hololive/hololive-kakao-bot-go/internal/bot/bot.go
@@
-	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
-	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
+	"github.com/kapu/hololive-shared/pkg/platform/chzzk"
+	"github.com/kapu/hololive-shared/pkg/platform/twitch"
```

### worker module 생성 후 이동 대상
```text
hololive/
  hololive-alarm-worker/
    go.mod
    cmd/alarm-worker/main.go
    internal/app
    internal/service/alarm/checker
    internal/service/alarm/scheduler
```

### `go.work` diff
```diff
diff --git a/go.work b/go.work
@@
 use (
 	./
 	./hololive/hololive-dispatcher-go
 	./hololive/hololive-admin-api
+	./hololive/hololive-alarm-worker
 	./hololive/hololive-kakao-bot-go
 	./hololive/hololive-llm-sched
 	./hololive/hololive-shared
 	./hololive/hololive-stream-ingester
 	./shared-go
 )
```

---

## 12. Phase D — `hololive-shared`를 얇게 만드는 ownership 분리

여기서 중요한 점이 하나 있다.

> `hololive-shared`를 “예쁘게 작게” 만드는 것이 목표가 아니다.  
> **누가 이 코드를 최종 책임지는지 명확하게 만드는 것**이 목표다.

현재 `hololive-shared/pkg/service/youtube/*`는 사실상 하나의 YouTube 서브시스템이다.
이건 장기적으로 `hololive-stream-ingester`가 owner가 되는 것이 맞다.

다만 이것도 **한 번에 물리 이동하지 말고**, owner seam부터 만든 뒤 이동해야 한다.

### 권장 순서
#### D-1. owner seam 도입
`hololive-stream-ingester/internal/runtime`에서 직접 shared implementation에 기대는 대신,
owner package를 만든다.

새 경로:
```text
hololive/hololive-stream-ingester/internal/youtubeowner/
  poller/
  outbox/
  tracking/
  scraper/
  stats/
```

처음에는 wrapper만 둔다.

예:
```go
package pollerowner

import sharedpoller "github.com/kapu/hololive-shared/pkg/service/youtube/poller"

type Scheduler = sharedpoller.Scheduler

var (
    NewScheduler = sharedpoller.NewScheduler
)
```

#### D-2. stream-ingester 호출부를 owner seam으로 바꿈
```diff
diff --git a/hololive/hololive-stream-ingester/internal/runtime/bootstrap_stream_ingester_youtube.go b/hololive/hololive-stream-ingester/internal/runtime/bootstrap_stream_ingester_youtube.go
@@
-	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
+	pollerowner "github.com/kapu/hololive-stream-ingester/internal/youtubeowner/poller"
```

#### D-3. 그 다음 implementation을 shared → owner seam으로 옮김
이 단계부터 실제 파일 이동을 한다.

우선순위:
1. `poller`
2. `scraper`
3. `outbox`
4. `tracking`
5. `stats`

### 이유
- `poller/scraper`는 stream-ingester runtime이 직접 owner다.
- `outbox/tracking`은 ops/reporting 경로도 stream-ingester가 제일 많이 쓴다.
- `stats`는 마지막에 옮겨도 된다.

### move map 1차
```text
move:
  hololive/hololive-shared/pkg/service/youtube/poller/*
  -> hololive/hololive-stream-ingester/internal/youtubeowner/poller/*

move:
  hololive/hololive-shared/pkg/service/youtube/scraper/*
  -> hololive/hololive-stream-ingester/internal/youtubeowner/scraper/*
```

### move map 2차
```text
move:
  hololive/hololive-shared/pkg/service/youtube/outbox/*
  -> hololive/hololive-stream-ingester/internal/youtubeowner/outbox/*

move:
  hololive/hololive-shared/pkg/service/youtube/tracking/*
  -> hololive/hololive-stream-ingester/internal/youtubeowner/tracking/*
```

### move map 3차
```text
move:
  hololive/hololive-shared/pkg/service/youtube/stats/*
  -> hololive/hololive-stream-ingester/internal/youtubeowner/stats/*
```

### 주의
이 단계에서는 `hololive-kakao-bot-go`가 `youtube.Service`, `youtube/stats`를 아직 사용하고 있으므로,
bot이 직접 owner implementation을 가져다 쓰지 말고 **interface / thin client / provider seam**으로 바꿔야 한다.

즉, `shared`에서 제거하기 전에 bot 호출부를 얇게 만드는 작업이 먼저 필요하다.

---

## 13. 테스트 / 검증 플랜

각 단계 종료 조건은 명확해야 한다.

### Phase A 종료 조건
```bash
go test ./hololive/hololive-shared/pkg/service/alarm/... -count=1
go test ./hololive/hololive-shared/pkg/service/auth -count=1
go test ./hololive/hololive-shared/pkg/service/cache -count=1
go test ./hololive/hololive-kakao-bot-go/internal/bot -count=1
go test ./hololive/hololive-kakao-bot-go/internal/service/chzzk -count=1
git diff --check
```

### Phase B 종료 조건
```bash
go build ./hololive/hololive-kakao-bot-go/cmd/bot
go build ./hololive/hololive-kakao-bot-go/cmd/admin-api
go build ./hololive/hololive-kakao-bot-go/cmd/alarm-worker

go test ./hololive/hololive-kakao-bot-go/internal/app/... -count=1
go test ./hololive/hololive-kakao-bot-go/internal/service/alarm/... -count=1
go test ./hololive/hololive-kakao-bot-go/internal/server/... -count=1
go test ./admin-dashboard/backend/...  # 실제 명령은 repo 관례에 맞춤
```

### Compose smoke
```bash
docker compose -f docker-compose.prod.yml up -d hololive-bot hololive-admin-api hololive-alarm-worker admin-dashboard
curl -f http://127.0.0.1:30001/health
curl -f http://127.0.0.1:30006/health
curl -f http://127.0.0.1:30007/health
```

### 기능 smoke
- Kakao webhook 정상 응답
- dashboard 조회/수정 정상
- 알림 scheduler loop 정상 시작
- queue publish 정상
- bot ingress latency가 admin 호출/worker loop와 독립적으로 유지

---

## 14. 배포 순서

운영 배포는 아래 순서가 가장 안전하다.

1. **Phase A만 배포**
2. bot + admin-api + alarm-worker를 모두 띄우되,
   - bot는 ingress only
   - admin-dashboard는 admin-api를 가리키게 변경
3. 일정 기간 bot CPU / goroutine / p95 latency 비교
4. alarm-worker queue publish, dedup, sent count 확인
5. 이상 없으면 bot에서 남아 있는 admin/alarm 관련 dead code 제거
6. 그 다음에야 module 추출 시작

---

## 15. 롤백 전략

### admin-api 롤백
- dashboard env를 다시 `HOLO_BOT_URL=http://hololive-kakao-bot-go:30001`로 되돌린다.
- `hololive-admin-api` service만 내린다.
- bot 쪽 admin route 코드를 완전히 삭제하기 전까지는 하위 호환을 유지해야 한다.

### alarm-worker 롤백
- `hololive-alarm-worker`를 내리고
- bot runtime의 `BuildAlarmRuntimeScheduler(...)` 경로를 다시 켠다.
- 따라서 **bot에서 alarm runtime 삭제 커밋은 worker 안정화 뒤로 미룬다.**

이 말은 곧, Phase B 실제 구현은 더 세밀하게 두 PR로 나눠야 한다는 뜻이다.

#### B-2a
- alarm-worker 새 binary 추가
- bot에는 기존 scheduler 유지
- config flag로 only-one-owner 보장

#### B-2b
- 안정화 후 bot scheduler 제거

### only-one-owner 플래그 예시
`NOTIFICATION_SCHEDULER_ROLE=bot|worker|off`

- 첫 배포: `bot`
- 전환 배포: `worker`
- 롤백: `bot`

scheduler start 시 아래 가드 추가:
```go
role := strings.TrimSpace(os.Getenv("NOTIFICATION_SCHEDULER_ROLE"))
if role != "worker" {
    logger.Info("alarm scheduler disabled for this runtime", slog.String("role", role))
    return nil
}
```

이 플래그를 넣으면 dual-run 중복 발송 사고를 피할 수 있다.

---

## 16. 실제 실행 순서 요약

가장 현실적인 순서는 아래다.

### Step 1
Phase A 패치를 모두 넣는다.
- alarm deadline
- async backpressure
- auth session atomicity
- chzzk client hardening
- cache TTL ceil

### Step 2
admin-api binary를 **같은 module 안에** 추가한다.
- bot는 아직 기존 admin 유지 가능
- dashboard만 새 admin-api로 붙여 smoke test
- 안정화 후 bot admin 제거

### Step 3
Notifier의 concrete AlarmService 의존을 제거한다.
- shared keys 승격까지 같이 한다.

### Step 4
alarm-worker binary를 **같은 module 안에** 추가한다.
- 처음엔 feature flag로 scheduler owner 전환
- 안정화 후 bot scheduler 제거

### Step 5
bot runtime를 ingress-only로 줄인다.

### Step 6
그 다음에 `hololive-admin-api` module 추출.
- 이건 상대적으로 쉽다.

### Step 7
마지막으로 alarm-worker module 추출 여부를 결정한다.
- process split만으로 충분하면 여기서 멈춰도 된다.
- module까지 필요하면 chzzk/twitch thin client 이동 후 진행한다.

### Step 8
마지막 단계에서 shared YouTube owner transfer를 시작한다.
- stream-ingester owner seam
- implementation move
- bot 호출부 interface/thin client화

---

## 17. 이번 플랜에서 하지 말아야 할 것

1. `hololive-shared/pkg/service/youtube/*` 전체를 한 번에 옮기지 않는다.
2. admin-api와 alarm-worker를 곧바로 새 module로 만들지 않는다.
3. bot에서 admin 제거와 dashboard 라우팅 변경을 한 PR에 섞지 않는다.
4. worker dual-run 보호 없이 scheduler를 둘 다 켜지 않는다.
5. config subscriber를 모든 runtime에서 똑같이 재사용하지 않는다.
   - persistence owner와 runtime apply owner를 분리해야 한다.

---

## 18. 최종 판단

지금 저장소에서 가장 큰 구조적 문제는 “파일이 크다”가 아니다.

- ingress
- control plane
- background worker

이 세 런타임이 한 프로세스에 같이 붙어 있다는 것이 핵심 문제다.

따라서 이번 라운드의 정답은 아래다.

1. **잔여 correctness 리스크부터 닫는다.**
2. **같은 module 안에서 admin-api / alarm-worker binary를 먼저 분리한다.**
3. **bot를 ingress-only로 만든다.**
4. **그 다음에 module 분리와 shared ownership 정리를 한다.**

이 순서를 따르면, 운영 안정성과 구조 개선을 동시에 얻을 수 있다.
반대로 이 순서를 무시하면, 구조는 바뀌는데 장애는 그대로 남는다.
