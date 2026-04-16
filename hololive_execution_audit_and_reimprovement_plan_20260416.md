# Hololive 구조 분리 이행 감사 + 재개선 실행 플랜
작성일: 2026-04-16  
대상 번들: `hololive-bot-review-bundle-full-20260416T015339Z.tar.gz`

상태: **COMPLETED (2026-04-16)**

이 문서는 runtime split 이 아직 미완료였던 시점의 감사+실행 플랜이었고,
현재는 장기 조건까지 이행이 끝난 뒤의 **완료 기록 겸 historical implementation log** 다.
현재 source of truth 는 다음 문서다.

- `docs/current/PROJECT_MAP.md`
- `docs/current/RUNTIME_SPLIT_HANDOFF_20260416.md`
- `docs/current/RUNTIME_SPLIT_PR07_BLOCKERS_20260416.md`

본문의 patch plan / pre-extraction 경로 예시는 당시 작업 순서를 보존하기 위한 기록이며,
현행 ownership 경계 자체는 위 source-of-truth 문서를 따른다.

## 0. 판단 요약

이 플랜은 현재 기준으로 **코드/워크스페이스/문서 수준까지 이행 완료**됐다.  
다만 **프로세스/배포 분리**는 실제로 들어갔고, 이전 리뷰에서 치명적으로 봤던 다수의 correctness 이슈도 고쳐졌습니다.

현재 상태를 한 문장으로 요약하면 다음과 같습니다.

- **완료된 것**: `hololive-admin-api` / `hololive-alarm-worker` 독립 go.mod 추출, `internal/server`/alarm checker/scheduler ownership 이동, `hololive-shared/pkg/service/notification` 공용 ownership 정리, build/deploy/workspace/documentation surface 정합화.
- **정리된 것**: bot 는 ingress-only ownership 으로 고정됐고, `InitCoreInfrastructure(...)` 기반 coupling 은 admin-api/alarm-worker 경로에서 제거됐다.
- **따라서 현재 이 문서의 역할**: 새로운 실행 계획이 아니라, 당시 gap 이 무엇이었고 어떤 순서로 닫혔는지를 보존하는 감사 기록이다.

이하 본문은 historical implementation log 로 유지한다.

---

## 1. 이번 번들에서 실제로 이행된 항목

### 1.1 프로세스/배포 분리: 이행됨

다음은 실제로 확인된 상태입니다.

- `hololive-kakao-bot-go/cmd/bot`
- `hololive-kakao-bot-go/cmd/admin-api`
- `hololive-kakao-bot-go/cmd/alarm-worker`

`docker-compose.prod.yml`에도 다음 서비스가 존재합니다.

- `hololive-bot` (`30001`)
- `hololive-admin-api` (`30006`)
- `hololive-alarm-worker` (`30007`)

`admin-dashboard`도 upstream을 `hololive-admin-api:30006`으로 보고 있습니다.  
즉, **실행 단위 분리와 배포 단위 분리 자체는 들어갔습니다.**

### 1.2 이전 correctness 이슈: 상당수 해결됨

이전 리뷰에서 우선순위가 높았던 항목 중 다음은 이번 번들에서 개선된 것이 확인됐습니다.

- `alarm/targets.go` deadline 보존 helper 도입
- `bot_message_async.go`의 worker-pool reject 시 무제한 `go task()` fallback 제거
- `cache.MSet` fail-fast 및 sub-second TTL ceil 보정
- `ACL` rollback semantics 보강
- `Holodex SearchChannels`의 전체 채널 캐시 경유
- `Chzzk` logger / http client 기본값 보강
- `auth` refresh 경로의 정합성 보강
- `notification.AlarmService` 일부 책임 분리

즉, **이번 번들은 이전 치명 결함을 반복한 번들이 아닙니다.**

---

## 2. 아직 “완벽히 이행”되었다고 볼 수 없는 이유

### 2.1 bot는 아직도 코드상으로는 ingress-only가 아니다

현재 bot는 Compose에서 `BOT_ADMIN_ENABLED=false`로 돌고 있지만, 코드에서는 여전히 다음 경로를 유지합니다.

- `internal/app/bootstrap_bot_runtime_orchestration.go`
- `internal/app/bootstrap/bot_server.go`
- `hololive-shared/pkg/config/config.go`
- `hololive-shared/pkg/config/config_types.go`

즉, 지금은 **“admin을 안 켠 bot”**이지, **“admin capability가 제거된 ingress-only bot”**이 아닙니다.

이 차이는 중요합니다.  
왜냐하면 다음 두 가지가 계속 남기 때문입니다.

1. monolith fallback 경로가 살아 있어 테스트/문서/코드 소유권이 흐려짐
2. bot/admin 분리 이후에도 여전히 “한 프로세스가 다 할 수 있다”는 설계 신호가 코드에 남음

### 2.2 세 runtime이 여전히 같은 거대 초기화 그래프를 공유한다

가장 중요한 미완료 항목은 이것입니다.

현재:

- `BuildRuntime(...)`
- `BuildAdminAPIRuntime(...)`
- `BuildAlarmWorkerRuntime(...)`

모두가 결국 `appbootstrap.InitCoreInfrastructure(...)`를 타고 갑니다.

이 함수는 사실상 “bot 전체를 만들기 위한 거대 조립기”입니다.  
아래를 거의 한 번에 만들고 있습니다.

- DB/Cache infra
- Iris client
- template renderer
- message adapter / formatter
- holodex + scraper foundation
- alarm mode
- chzzk/twitch clients
- member matcher
- YouTube stack
- settings / activity / ACL / worker pool
- full `bot.Dependencies`

즉, 프로세스는 분리됐는데 **초기화 그래프와 소유권은 아직 분리되지 않았습니다.**

그 결과:

- admin-api가 bot용 messaging 계층 주변 코드까지 같이 끌고 들어옴
- alarm-worker가 bot-shaped infra를 경유함
- startup time / connection pool / cache warmup / memory footprint가 필요 이상으로 겹침
- 분리 후에도 변경 영향 범위가 여전히 큼

### 2.3 wrapper / alias layer가 여전히 남아 있다

현재 `internal/app`에는 다음처럼 wrapper/alias 성격의 레이어가 남아 있습니다.

- `bootstrap_bot_dependency_views.go`
- `bootstrap_bot_admin.go`

예를 들어:

- `type botWebhookRuntimeDependencies = appbootstrap.BotWebhookRuntimeDependencies`
- `type botConfigSubscriberDependencies = appbootstrap.BotConfigSubscriberDependencies`
- `type botAdminServerDependencies = appbootstrap.AdminServerDependencies`

이건 경계가 아니라 **중복 번역 레이어**입니다.  
실제 책임이 아니라 “예전 구조를 호환하기 위한 완충재”에 가깝습니다.

### 2.4 문서가 실제 코드 상태보다 앞서 있다

문서 드리프트가 큽니다.

대표 예시:

- `docs/MULTIMODULE_MIGRATION_P3_PLAN.md`는 `hololive-admin`, `hololive-alarm` 다중 모듈이 이미 완료된 것처럼 서술
- 실제 `go.work`에는 그런 모듈이 없음
- `SERVICE_DECOMPOSITION_ROADMAP.md`에는 `alarm-dispatcher`, `30002` 등 옛 경로/포트가 대량으로 남아 있음
- `hololive-shared/pkg/config/admin_api.go`는 아직 `ADMIN_API_PORT=30002` 기본값을 품고 있음
- 실제 `cmd/admin-api`는 이 specialized loader를 쓰지 않고 `config.Load`를 사용

즉, 문서는 “목표 상태”와 “현재 상태”가 섞여 있습니다.

---

## 3. 최종 목표 상태

이 문서는 다음 구조를 목표로 합니다.

### 3.1 런타임 경계

- `bot`
  - webhook ingress
  - command routing
  - bot health / ready
  - 설정 구독(pub/sub) 중 bot에 필요한 최소 적용만 수행

- `admin-api`
  - `/api/holo/*`
  - `/api/auth/*`
  - `/oauth/callback`
  - `/internal/alarm/*`
  - 운영/제어면 전용 HTTP

- `alarm-worker`
  - scheduler / checker / dedup / queue publish
  - 설정 구독(pub/sub)
  - worker health / ready

### 3.2 라이브러리/모듈 소유권

- `shared-go`
  - 진짜 범용 유틸만 유지

- `hololive-shared`
  - 공통 domain/contracts/constants/config/server helper 정도만 유지
  - 거대 구현 소유권은 줄임

- 새 도메인 라이브러리(권장)
  - `hololive-alarm`: alarm CRUD/state/cache ownership
  - 향후 `youtube/*`는 `stream-ingester` 쪽으로 소유권 회수

### 3.3 비목표

다음은 별도 서비스로 쪼개지 않습니다.

- `chzzk`
- `twitch`
- `matcher`
- `activity`

이들은 **서비스가 아니라 라이브러리**로 유지해야 합니다.

---

## 4. 실행 순서 요약

실행은 반드시 아래 순서를 지킵니다.

1. **PR-01**: bot를 코드상으로도 ingress-only로 고정
2. **PR-02**: leaf constructor에서 거대 infra 의존 제거
3. **PR-03**: runtime별 dedicated initializer 도입
4. **PR-04**: shared YouTube builder를 API용 / scheduler용으로 분리
5. **PR-05**: wrapper/alias layer 제거
6. **PR-06**: 문서/설정 정합성 복구
7. **PR-07**: (선행 PR 전부 merge 후) 도메인 라이브러리/멀티모듈 추출

아래부터는 각 PR을 바로 실행할 수 있도록 상세히 적습니다.

---

# PR-01. bot를 코드상으로도 ingress-only로 고정

이 PR의 목적은 **“Compose에서 admin을 끄는 상태”를 “코드가 admin capability를 더 이상 제공하지 않는 상태”로 바꾸는 것**입니다.

## PR-01A. bot의 scheduler role을 명시적으로 off로 전환

현재 bot 컨테이너는 `NOTIFICATION_SCHEDULER_ROLE="worker"`입니다.  
의미상 동작은 맞지만, bot 자신이 worker가 아닌데 worker라는 값을 들고 있는 셈이라 코드 독해와 운영 의도가 맞지 않습니다.

### 수정 파일

- `docker-compose.prod.yml`

### 패치

```diff
diff --git a/docker-compose.prod.yml b/docker-compose.prod.yml
@@
   hololive-bot:
@@
-      BOT_ADMIN_ENABLED: "false"
-      NOTIFICATION_SCHEDULER_ROLE: "worker"
+      NOTIFICATION_SCHEDULER_ROLE: "off"
       SERVER_PORT: 30001
```

### 이유

- bot는 더 이상 scheduler 소유자가 아님
- `off`는 “이 runtime에서는 절대 돌지 않음”을 의미하므로 운영 의도가 선명함
- `runtime_split_test.go`에도 이미 `off` semantic이 존재함

---

## PR-01B. `BOT_ADMIN_ENABLED` 제거

### 수정 파일

- `hololive/hololive-shared/pkg/config/config.go`
- `hololive/hololive-shared/pkg/config/config_types.go`
- `hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_runtime_orchestration.go`
- `hololive/hololive-kakao-bot-go/internal/app/bootstrap/bot_server.go`

### 패치 1: config에서 제거

```diff
diff --git a/hololive/hololive-shared/pkg/config/config_types.go b/hololive/hololive-shared/pkg/config/config_types.go
@@
 type BotConfig struct {
     Prefix        string
     SelfUser      string
-    AdminEnabled  bool
     MentionPrefix string // 멘션 기반 명령어 접두사 (예: @카푸봇)
 }
```

```diff
diff --git a/hololive/hololive-shared/pkg/config/config.go b/hololive/hololive-shared/pkg/config/config.go
@@
         Bot: BotConfig{
             Prefix:        sharedenv.String("BOT_PREFIX", "!"),
             SelfUser:      sharedenv.String("BOT_SELF_USER", "iris"),
-            AdminEnabled:  sharedenv.Bool("BOT_ADMIN_ENABLED", true),
             MentionPrefix: sharedenv.String("BOT_MENTION_PREFIX", "#kapu봇"),
         },
```

### 패치 2: bot runtime orchestration에서 admin branch 제거

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_runtime_orchestration.go b/hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_runtime_orchestration.go
@@
 import (
     "context"
     "fmt"
     "log/slog"
-    "os"

     "github.com/kapu/hololive-shared/pkg/config"
-    "github.com/kapu/hololive-shared/pkg/domain"

     appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
     "github.com/kapu/hololive-kakao-bot-go/internal/bot"
 )
@@
 func buildBotRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger, infra *appbootstrap.CoreInfrastructure) (*BotRuntime, error) {
     runtimeViews := buildBotRuntimeDependencyViews(infra)
@@
-    alarmScheduler, err := buildRuntimeAlarmScheduler(runtimeRoleBot, cfg, infra, logger, os.Getenv(notificationSchedulerRoleEnv))
-    if err != nil {
-        return nil, fmt.Errorf("build bot runtime: alarm runtime scheduler: %w", err)
-    }
-
     // ConfigSubscriber: Valkey Pub/Sub를 통해 설정 변경을 수신하여 적용
     configSubscriber := appbootstrap.BuildBotConfigSubscriber(ctx, runtimeViews.configSubscriber, runtimeViews.configSubscriberRuntime, nil, logger)
-
-    var adminServerDeps *botAdminServerDependencies
-
-    if cfg.Bot.AdminEnabled {
-        adminServerDeps, err = buildBotAdminServerDependencies(ctx, cfg, runtimeViews.adminRuntime, nil, logger)
-        if err != nil {
-            return nil, fmt.Errorf("build bot runtime: admin server dependencies: %w", err)
-        }
-    }
-
-    var internalAlarmCRUD domain.AlarmCRUD
-    if cfg.Bot.AdminEnabled {
-        internalAlarmCRUD = runtimeViews.serverRuntime.alarmCRUD
-    }
-
-    botServer, err := appbootstrap.BuildBotServer(ctx, cfg, webhookHandler, nil, internalAlarmCRUD, adminServerDeps, logger)
+    botServer, err := appbootstrap.BuildBotServer(ctx, cfg, webhookHandler, nil, logger)
     if err != nil {
         return nil, err
     }
@@
         Config:               cfg,
         Logger:               logger,
         Bot:                  botBot,
-        AlarmScheduler:       alarmScheduler,
         ConfigSubscriber:     configSubscriber,
         ServerAddr:           fmt.Sprintf(":%d", cfg.Server.Port),
         HttpServer:           botServer,
         webhookHandlerCloser: webhookHandler,
     }, nil
 }
```

### 패치 3: bot server를 ingress-only로 단순화

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/bootstrap/bot_server.go b/hololive/hololive-kakao-bot-go/internal/app/bootstrap/bot_server.go
@@
 import (
     "context"
-    "errors"
     "fmt"
     "log/slog"
     "net/http"
-    "strings"

-    "github.com/gin-gonic/gin"
     "github.com/kapu/hololive-shared/pkg/config"
-    "github.com/kapu/hololive-shared/pkg/domain"
     sharedserver "github.com/kapu/hololive-shared/pkg/server"
-    "github.com/kapu/hololive-shared/pkg/server/middleware"
-    "github.com/kapu/hololive-shared/pkg/service/alarm"
     "github.com/park285/iris-client-go/iris"

     apphttp "github.com/kapu/hololive-kakao-bot-go/internal/app/http"
 )
@@
 func BuildBotServer(
     ctx context.Context,
     cfg *config.Config,
     webhookHandler *iris.WebhookHandler,
     triggerHandler *sharedserver.TriggerHandler,
-    alarmCRUD domain.AlarmCRUD,
-    adminDeps *AdminServerDependencies,
     logger *slog.Logger,
 ) (*http.Server, error) {
-    var (
-        botRouter *gin.Engine
-        err       error
-    )
-
-    if cfg.Bot.AdminEnabled {
-        if adminDeps == nil || adminDeps.DomainHandlers == nil || adminDeps.AuthHandler == nil {
-            return nil, errors.New("build bot server: admin routes enabled but dependencies are incomplete")
-        }
-
-        botRouter, err = apphttp.ProvideAPIRouter(
-            ctx,
-            cfg,
-            logger,
-            adminDeps.DomainHandlers,
-            adminDeps.AuthHandler,
-            webhookHandler,
-            triggerHandler,
-            adminDeps.Cache,
-        )
-        if err != nil {
-            return nil, fmt.Errorf("build bot server: provide api router: %w", err)
-        }
-    } else {
-        botRouter, err = apphttp.ProvideBotRouter(ctx, cfg, logger, webhookHandler, triggerHandler)
-        if err != nil {
-            return nil, fmt.Errorf("build bot server: provide bot router: %w", err)
-        }
-    }
-
-    if alarmCRUD != nil {
-        if strings.TrimSpace(cfg.Server.APIKey) == "" {
-            return nil, errors.New("build bot server: internal alarm API requires API_SECRET_KEY")
-        }
-
-        alarmAPI := alarm.NewAPIHandler(alarmCRUD, logger)
-        internalAlarmGroup := botRouter.Group("")
-        internalAlarmGroup.Use(middleware.APIKeyAuthMiddleware(cfg.Server.APIKey))
-        alarmAPI.RegisterInternalRoutes(internalAlarmGroup)
-    }
+    botRouter, err := apphttp.ProvideBotRouter(ctx, cfg, logger, webhookHandler, triggerHandler)
+    if err != nil {
+        return nil, fmt.Errorf("build bot server: provide bot router: %w", err)
+    }

     addr := fmt.Sprintf(":%d", cfg.Server.Port)
     return sharedserver.NewH2CServer(addr, botRouter, "hololive-bot.http"), nil
 }
```

### 후속 정리

이 PR이 끝나면 다음 grep가 0건이어야 합니다.

```bash
rg -n "BOT_ADMIN_ENABLED|cfg\.Bot\.AdminEnabled" hololive/hololive-kakao-bot-go hololive/hololive-shared
```

---

## PR-01C. BotRuntime에서 alarm scheduler lifecycle 제거

bot는 더 이상 scheduler를 소유하지 않으므로 `BotRuntime`에서 관련 필드를 지웁니다.

### 수정 파일

- `hololive/hololive-kakao-bot-go/internal/app/runtime.go`
- `hololive/hololive-kakao-bot-go/internal/app/runtime_start.go`
- `hololive/hololive-kakao-bot-go/internal/app/runtime_shutdown.go`

### 패치

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/runtime.go b/hololive/hololive-kakao-bot-go/internal/app/runtime.go
@@
 import (
     "context"
     "fmt"
     "log/slog"
     "net/http"
-    "sync"

     "github.com/kapu/hololive-shared/pkg/config"
     "github.com/kapu/hololive-shared/pkg/service/configsub"
     "github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"
@@
-type runtimeAlarmScheduler interface {
-    Start(ctx context.Context)
-}
-
 type BotRuntime struct {
     Config *config.Config
     Logger *slog.Logger

     Bot            *bot.Bot
-    AlarmScheduler runtimeAlarmScheduler // Alarm runtime scheduler
-
     ConfigSubscriber *configsub.Subscriber // Valkey Pub/Sub 설정 구독자

     ServerAddr string
     HttpServer *http.Server

     webhookHandlerCloser interface{ Close() error }
-    alarmSchedulerMu     sync.Mutex
-    alarmSchedulerCancel context.CancelFunc
     lifecycle.Managed
 }
```

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/runtime_start.go b/hololive/hololive-kakao-bot-go/internal/app/runtime_start.go
@@
 func (r *BotRuntime) Start(ctx context.Context, errCh chan<- error) {
@@
     appruntime.Start(ctx, errCh, appruntime.StartHooks{
         Logger:     r.Logger,
         ServerAddr: r.ServerAddr,
-        StartAlarmScheduler: func(ctx context.Context) {
-            if r.AlarmScheduler != nil {
-                r.AlarmScheduler.Start(ctx)
-            }
-        },
         RunConfigSubscriber: func(ctx context.Context) {
             if r.ConfigSubscriber != nil {
                 r.ConfigSubscriber.Run(ctx)
             }
         },
         StartBot: func(ctx context.Context) error {
             if r.Bot == nil {
                 return nil
             }
             return r.Bot.Start(ctx)
         },
-        StartHTTPServer:         r.StartHTTPServer,
-        SetAlarmSchedulerCancel: r.setAlarmSchedulerCancel,
+        StartHTTPServer: r.StartHTTPServer,
     })
 }
-
-func (r *BotRuntime) setAlarmSchedulerCancel(cancel context.CancelFunc) {
-    ...
-}
-
-func (r *BotRuntime) clearAlarmSchedulerCancel() bool {
-    ...
-}
-
-func (r *BotRuntime) startSchedulers(ctx context.Context, errCh chan<- error) {
-    ...
-}
-
-func (r *BotRuntime) startAlarmScheduler(ctx context.Context) {
-    ...
-}
```

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/runtime_shutdown.go b/hololive/hololive-kakao-bot-go/internal/app/runtime_shutdown.go
@@
 func (r *BotRuntime) Shutdown(ctx context.Context) {
@@
     appruntime.Shutdown(ctx, appruntime.ShutdownHooks{
-        Logger:              r.Logger,
-        ClearAlarmScheduler: r.clearAlarmSchedulerCancel,
-        ShutdownHTTPServer:  r.ShutdownHTTPServer,
+        Logger:             r.Logger,
+        ShutdownHTTPServer: r.ShutdownHTTPServer,
         WebhookHandlerClose: func() error {
             if r.webhookHandlerCloser == nil {
                 return nil
             }
             return r.webhookHandlerCloser.Close()
         },
         ShutdownAlarmServices: notification.CloseAllAlarmServices,
         ShutdownBot: func(ctx context.Context) error {
             if r.Bot == nil {
                 return nil
             }
             return r.Bot.Shutdown(ctx)
         },
     })
 }
```

### 이유

- bot lifecycle에서 worker 책임을 제거
- runtime graph가 역할별로 정직해짐
- 운영 중 오해를 줄임

---

# PR-02. leaf constructor에서 거대 infra 의존 제거

이 PR의 목적은 **거대한 `CoreInfrastructure`를 leaf constructor에 넘기는 패턴을 없애는 것**입니다.  
이 단계만 해도 이후 runtime-specific initializer 분리가 쉬워집니다.

## PR-02A. runtime scheduler가 concrete `*notification.AlarmService`를 몰라야 한다

현재 `RuntimeScheduler`는 이미 `GetTargetMinutes()`만 씁니다.  
그런데 생성자 시그니처는 여전히 `*notification.AlarmService` concrete 타입을 받습니다.

이건 분리 후에도 worker가 bot의 구체 구현체를 끌고 오게 만드는 원인입니다.

### 수정 파일

- `hololive/hololive-kakao-bot-go/internal/service/alarm/scheduler/runtime_scheduler.go`
- `hololive/hololive-kakao-bot-go/internal/app/bootstrap/bot_runtime_alarm.go`

### 패치 1: scheduler constructor를 interface 기반으로 전환

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/service/alarm/scheduler/runtime_scheduler.go b/hololive/hololive-kakao-bot-go/internal/service/alarm/scheduler/runtime_scheduler.go
@@
 import (
     "context"
     "errors"
     "fmt"
     "log/slog"
     "time"

     "github.com/kapu/hololive-shared/pkg/config"
+    "github.com/kapu/hololive-shared/pkg/domain"
     sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
     "github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
     "github.com/kapu/hololive-shared/pkg/service/alarm/queue"
     "github.com/kapu/hololive-shared/pkg/service/alarm/tier"
     "github.com/kapu/hololive-shared/pkg/service/cache"
     "github.com/kapu/hololive-shared/pkg/service/holodex"
     "golang.org/x/sync/errgroup"

     "github.com/kapu/hololive-kakao-bot-go/internal/service/alarm/checker"
     "github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
-    "github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
     "github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
 )
@@
-type targetMinutesSource interface {
-    GetTargetMinutes() []int
-}
-
 type targetMinutesUpdater interface {
     UpdateTargetMinutes([]int)
 }
@@
     cacheSvc cache.Client,
     holodexSvc *holodex.Service,
     chzzkClient *chzzk.Client,
     twitchClient *twitch.Client,
-    alarmSvc *notification.AlarmService,
+    alarmCRUD domain.AlarmCRUD,
     notifCfg config.NotificationConfig,
     logger *slog.Logger,
 ) (*RuntimeScheduler, error) {
@@
-    if alarmSvc == nil {
+    if alarmCRUD == nil {
         return nil, errors.New("new runtime scheduler: alarm service is nil")
     }
@@
-    targetMinutes := sharedchecker.NormalizeTargetMinutes(alarmSvc.GetTargetMinutes())
+    targetMinutes := sharedchecker.NormalizeTargetMinutes(alarmCRUD.GetTargetMinutes())
@@
         youtubeTargetUpdater: youtubeChecker,
         dedupTargetUpdater:   dedupSvc,
-        targetMinutesSource:  alarmSvc,
+        targetMinutesSource:  alarmCRUD,
```

### 패치 2: builder도 concrete `AlarmService` 대신 `AlarmCRUD`를 사용

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/bootstrap/bot_runtime_alarm.go b/hololive/hololive-kakao-bot-go/internal/app/bootstrap/bot_runtime_alarm.go
@@
 func BuildAlarmRuntimeScheduler(
     cfg *config.Config,
     infra *CoreInfrastructure,
     logger *slog.Logger,
 ) (RuntimeAlarmScheduler, error) {
@@
-    if infra.AlarmService == nil {
-        return nil, errors.New("build alarm runtime scheduler: alarm service is nil")
+    if infra.AlarmCRUD == nil {
+        return nil, errors.New("build alarm runtime scheduler: alarm CRUD is nil")
     }
@@
         infra.Deps.Cache,
         infra.HolodexService,
         infra.Deps.Chzzk,
         infra.Deps.Twitch,
-        infra.AlarmService,
+        infra.AlarmCRUD,
         cfg.Notification,
         logger,
     )
```

### 이유

- worker runtime이 `notification.AlarmService` concrete 구현을 알 필요가 없음
- 이후 `hololive-alarm` domain library 또는 별도 module로 추출하기 쉬워짐
- 테스트 대역(stub/fake) 작성이 쉬워짐

---

## PR-02B. alarm-worker config subscriber가 `*CoreInfrastructure`를 모르도록 바꾼다

현재 `BuildAlarmWorkerConfigSubscriber(...)`는 실제로 필요한 것은 cache client와 `AlarmCRUD`뿐인데, `*CoreInfrastructure` 전체를 받습니다.

### 수정 파일

- `hololive/hololive-kakao-bot-go/internal/app/build_alarm_worker_config_subscriber.go`
- `hololive/hololive-kakao-bot-go/internal/app/build_alarm_worker_runtime.go`

### 패치

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/build_alarm_worker_config_subscriber.go b/hololive/hololive-kakao-bot-go/internal/app/build_alarm_worker_config_subscriber.go
@@
 import (
     "context"
     "log/slog"

     contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
+    "github.com/kapu/hololive-shared/pkg/domain"
+    "github.com/kapu/hololive-shared/pkg/service/cache"
     "github.com/kapu/hololive-shared/pkg/service/configsub"
-
-    appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
 )

 func BuildAlarmWorkerConfigSubscriber(
     ctx context.Context,
-    infra *appbootstrap.CoreInfrastructure,
+    cacheSvc cache.Client,
+    alarmCRUD domain.AlarmCRUD,
     logger *slog.Logger,
 ) *configsub.Subscriber {
-    if infra == nil || infra.Deps == nil || infra.Deps.Cache == nil || infra.AlarmCRUD == nil {
+    if cacheSvc == nil || alarmCRUD == nil {
         return nil
     }
@@
         AlarmAdvanceMinutes: func(payload contractssettings.AlarmAdvanceMinutesPayloadV1) {
-            targets := infra.AlarmCRUD.UpdateAlarmAdvanceMinutes(ctx, payload.Minutes)
+            targets := alarmCRUD.UpdateAlarmAdvanceMinutes(ctx, payload.Minutes)
             if logger != nil {
                 logger.Info(
                     "Alarm worker applied alarm advance minutes via pub/sub",
                     slog.Int("minutes", payload.Minutes),
                     slog.Any("targets", targets),
                 )
             }
         },
     })

-    return configsub.New(infra.Deps.Cache.GetClient(), applyFn, logger)
+    return configsub.New(cacheSvc.GetClient(), applyFn, logger)
 }
```

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/build_alarm_worker_runtime.go b/hololive/hololive-kakao-bot-go/internal/app/build_alarm_worker_runtime.go
@@
     return &AlarmWorkerRuntime{
         Config:           cfg,
         Logger:           logger,
         Scheduler:        scheduler,
-        ConfigSubscriber: BuildAlarmWorkerConfigSubscriber(ctx, infra, logger),
+        ConfigSubscriber: BuildAlarmWorkerConfigSubscriber(ctx, infra.Deps.Cache, infra.AlarmCRUD, logger),
         ServerAddr:       addr,
         HttpServer:       sharedserver.NewH2CServer(addr, router, "hololive-alarm-worker.http"),
         Managed:          lifecycle.NewManaged(infra.Cleanup),
     }, nil
 }
```

### 이유

- 함수가 실제 필요 의존만 받게 함
- giant parameter / god object passing 제거
- 이후 `AlarmWorkerInfrastructure` 도입 시 시그니처 변경이 거의 없어짐

---

# PR-03. runtime별 dedicated initializer 도입

이 PR이 이번 전체 작업의 핵심입니다.  
목표는 `InitCoreInfrastructure(...)`를 더 이상 admin-api/alarm-worker가 타지 않게 만드는 것입니다.

## PR-03A. foundation를 `Holodex`와 `Profile`로 분리

alarm-worker는 profile service가 필요 없습니다.  
그러므로 foundation를 두 단계로 나눕니다.

### 수정 파일

- `hololive/hololive-kakao-bot-go/internal/app/bootstrap/services_foundation.go`
- 새 파일:
  - `hololive/hololive-kakao-bot-go/internal/app/bootstrap/types_runtime_infra.go`
  - `hololive/hololive-kakao-bot-go/internal/app/bootstrap/services_runtime_bot.go`
  - `hololive/hololive-kakao-bot-go/internal/app/bootstrap/services_runtime_admin.go`
  - `hololive/hololive-kakao-bot-go/internal/app/bootstrap/services_runtime_alarm_worker.go`

### 패치 1: foundation 분리

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/bootstrap/services_foundation.go b/hololive/hololive-kakao-bot-go/internal/app/bootstrap/services_foundation.go
@@
 type ScraperHolodexProfileFoundation struct {
     HolodexService       *holodex.Service
     MemberServiceAdapter member.DataProvider
     ProfileService       *member.ProfileService
     SharedRL             *scraper.RateLimiter
 }

+type ScraperHolodexFoundation struct {
+    HolodexService       *holodex.Service
+    MemberServiceAdapter member.DataProvider
+    SharedRL             *scraper.RateLimiter
+}
+
+func InitScraperHolodexFoundation(
+    ctx context.Context,
+    cfg *config.Config,
+    infra *sharedmodules.InfraModule,
+    logger *slog.Logger,
+) (*ScraperHolodexFoundation, error) {
+    holodexAPIKey := cfg.Holodex.APIKey
+    memberServiceAdapter := providers.ProvideMemberServiceAdapter(ctx, infra.MemberCache, logger)
+
+    scraperProxyConfig := providersScraperProxyConfig(cfg)
+
+    sharedRL, err := providers.ProvideYouTubeScraperRateLimiter(infra.Cache, logger)
+    if err != nil {
+        return nil, fmt.Errorf("provide youtube scraper rate limiter: %w", err)
+    }
+
+    scraperService := providers.ProvideScraperService(
+        infra.Cache,
+        memberServiceAdapter,
+        scraperProxyConfig,
+        sharedRL,
+        logger,
+    )
+
+    holodexService, err := providers.ProvideHolodexService(
+        cfg.Holodex.BaseURL,
+        holodexAPIKey,
+        infra.Cache,
+        scraperService,
+        logger,
+    )
+    if err != nil {
+        return nil, fmt.Errorf("provide holodex service: %w", err)
+    }
+
+    return &ScraperHolodexFoundation{
+        HolodexService:       holodexService,
+        MemberServiceAdapter: memberServiceAdapter,
+        SharedRL:             sharedRL,
+    }, nil
+}
+
 func InitScraperHolodexProfileFoundation(
     ctx context.Context,
     cfg *config.Config,
     infra *sharedmodules.InfraModule,
     logger *slog.Logger,
 ) (*ScraperHolodexProfileFoundation, error) {
-    holodexAPIKey := cfg.Holodex.APIKey
-    memberServiceAdapter := providers.ProvideMemberServiceAdapter(ctx, infra.MemberCache, logger)
-
-    scraperProxyConfig := providersScraperProxyConfig(cfg)
-
-    sharedRL, err := providers.ProvideYouTubeScraperRateLimiter(infra.Cache, logger)
-    if err != nil {
-        return nil, fmt.Errorf("provide youtube scraper rate limiter: %w", err)
-    }
-
-    scraperService := providers.ProvideScraperService(
-        infra.Cache,
-        memberServiceAdapter,
-        scraperProxyConfig,
-        sharedRL,
-        logger,
-    )
-
-    holodexService, err := providers.ProvideHolodexService(
-        cfg.Holodex.BaseURL,
-        holodexAPIKey,
-        infra.Cache,
-        scraperService,
-        logger,
-    )
-    if err != nil {
-        return nil, fmt.Errorf("provide holodex service: %w", err)
-    }
+    foundation, err := InitScraperHolodexFoundation(ctx, cfg, infra, logger)
+    if err != nil {
+        return nil, err
+    }

-    profileService, err := providers.ProvideProfileService(ctx, infra.Cache, memberServiceAdapter, logger)
+    profileService, err := providers.ProvideProfileService(ctx, infra.Cache, foundation.MemberServiceAdapter, logger)
     if err != nil {
         return nil, fmt.Errorf("provide profile service: %w", err)
     }

     return &ScraperHolodexProfileFoundation{
-        HolodexService:       holodexService,
-        MemberServiceAdapter: memberServiceAdapter,
+        HolodexService:       foundation.HolodexService,
+        MemberServiceAdapter: foundation.MemberServiceAdapter,
         ProfileService:       profileService,
-        SharedRL:             sharedRL,
+        SharedRL:             foundation.SharedRL,
     }, nil
 }
```

### 이유

- alarm-worker가 profile service까지 같이 만드는 낭비 제거
- admin-api는 profile route 때문에 profile service를 유지
- foundation 책임이 선명해짐

---

## PR-03B. runtime별 infra type 도입

### 새 파일: `types_runtime_infra.go`

```go
package bootstrap

import (
    "github.com/kapu/hololive-shared/pkg/domain"
    "github.com/kapu/hololive-shared/pkg/service/cache"
    "github.com/kapu/hololive-shared/pkg/service/database"
    "github.com/kapu/hololive-shared/pkg/service/holodex"
    "github.com/kapu/hololive-shared/pkg/service/member"
    "github.com/kapu/hololive-shared/pkg/service/settings"
    "github.com/kapu/hololive-shared/pkg/service/template"
    "github.com/kapu/hololive-shared/pkg/service/youtube"
    ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"

    "github.com/kapu/hololive-kakao-bot-go/internal/bot"
    "github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
    "github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
    "github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
    "github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

type BotInfrastructure struct {
    Deps           *bot.Dependencies
    AlarmCRUD      domain.AlarmCRUD
    HolodexService *holodex.Service
    Cleanup        func()
}

type AdminAPIInfrastructure struct {
    Cache            cache.Client
    Postgres         database.Client
    MemberRepo       *member.Repository
    MemberCache      *member.Cache
    Profiles         *member.ProfileService
    AlarmCRUD        domain.AlarmCRUD
    HolodexService   *holodex.Service
    YouTubeService   youtube.Service
    StatsRepo        ytstats.StatsDashboardRepository
    ActivityLogger   *activity.Logger
    SettingsService  settings.ReadWriter
    ACLService       *acl.Service
    TemplateAdminSvc *template.AdminService
    Cleanup          func()
}

type AlarmWorkerInfrastructure struct {
    Cache         cache.Client
    HolodexService *holodex.Service
    ChzzkClient   *chzzk.Client
    TwitchClient  *twitch.Client
    AlarmCRUD     domain.AlarmCRUD
    Cleanup       func()
}
```

---

## PR-03C. bot/admin-api/alarm-worker 전용 initializer 작성

### 새 파일: `services_runtime_bot.go`

```go
package bootstrap

import (
    "context"
    "fmt"
    "log/slog"

    "github.com/kapu/hololive-shared/pkg/config"
    providers "github.com/kapu/hololive-shared/pkg/providers"
    "github.com/kapu/hololive-shared/pkg/service/holodex"
    "github.com/kapu/hololive-shared/pkg/service/template"

    "github.com/kapu/hololive-kakao-bot-go/internal/adapter"
)

func InitBotInfrastructure(ctx context.Context, cfg *config.Config, logger *slog.Logger) (_ *BotInfrastructure, retErr error) {
    infra, err := InitInfraResources(ctx, cfg, logger)
    if err != nil {
        return nil, err
    }

    irisClient, err := providers.ProvideIrisClient(logger)
    if err != nil {
        infra.Cleanup()
        return nil, err
    }

    defer func() {
        if retErr != nil {
            infra.Cleanup()
        }
    }()

    templateRenderer := template.NewRenderer(infra.Postgres.GetGormDB(), logger)
    messageAdapter := adapter.NewMessageAdapter(cfg.Bot.Prefix, cfg.Bot.MentionPrefix)
    formatter := adapter.NewResponseFormatter(cfg.Bot.Prefix, templateRenderer)

    foundation, err := InitScraperHolodexProfileFoundation(ctx, cfg, infra, logger)
    if err != nil {
        return nil, err
    }

    alarmStack, err := InitAlarmYouTubeStack(ctx, cfg, infra, foundation, irisClient, formatter, logger)
    if err != nil {
        return nil, err
    }

    integrations, err := InitCoreIntegrationServices(ctx, cfg, infra, logger)
    if err != nil {
        return nil, err
    }

    modules := BuildBotDependencyModules(
        cfg,
        infra,
        alarmStack.AlarmMode,
        foundation.HolodexService,
        messageAdapter,
        formatter,
        irisClient,
        foundation.ProfileService,
        alarmStack.MemberMatcher,
        alarmStack.YouTubeStack,
        alarmStack.ActivityLogger,
        alarmStack.SettingsService,
        integrations.ACLService,
        integrations.MajorEventRepo,
        integrations.MemberNewsService,
        integrations.CommandBuilders,
        integrations.WorkerPool,
        logger,
    )
    deps := ProvideBotDependencies(modules)

    return &BotInfrastructure{
        Deps:           deps,
        AlarmCRUD:      alarmStack.AlarmMode.AlarmCRUD,
        HolodexService: foundation.HolodexService,
        Cleanup:        infra.Cleanup,
    }, nil
}
```

### 새 파일: `services_runtime_admin.go`

중요: admin-api는 bot dependency graph 전체를 만들지 않습니다.

```go
package bootstrap

import (
    "context"
    "log/slog"

    "github.com/kapu/hololive-shared/pkg/config"
    sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
    "github.com/kapu/hololive-shared/pkg/service/template"
    ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
)

func InitAdminAPIInfrastructure(ctx context.Context, cfg *config.Config, logger *slog.Logger) (_ *AdminAPIInfrastructure, retErr error) {
    infra, err := InitInfraResources(ctx, cfg, logger)
    if err != nil {
        return nil, err
    }

    defer func() {
        if retErr != nil {
            infra.Cleanup()
        }
    }()

    foundation, err := InitScraperHolodexProfileFoundation(ctx, cfg, infra, logger)
    if err != nil {
        return nil, err
    }

    alarmRepo := ProvideAlarmRepository(infra.Postgres, logger)
    alarmMode, err := InitAlarmModeComponents(
        ctx,
        cfg,
        infra,
        foundation.HolodexService,
        foundation.MemberServiceAdapter,
        alarmRepo,
        logger,
    )
    if err != nil {
        return nil, err
    }

    aclService, err := ProvideACLService(
        ctx,
        cfg.Kakao.ACLEnabled,
        acl.ParseACLMode(cfg.Kakao.ACLMode),
        cfg.Kakao.Rooms,
        infra.Postgres,
        infra.Cache,
        logger,
    )
    if err != nil {
        return nil, err
    }

    statsRepo := ytstats.NewYouTubeStatsRepository(infra.Postgres, logger)
    ytStack := sharedmodules.BuildYouTubeAPIStack(ctx, sharedmodules.YouTubeAPIStackParams{
        YouTubeConfig:   cfg.YouTube,
        ScraperConfig:   cfg.Scraper,
        CacheService:    infra.Cache,
        StatsRepo:       statsRepo,
        SharedRateLimit: foundation.SharedRL,
        Logger:          logger,
    })

    templateRenderer := template.NewRenderer(infra.Postgres.GetGormDB(), logger)

    return &AdminAPIInfrastructure{
        Cache:            infra.Cache,
        Postgres:         infra.Postgres,
        MemberRepo:       infra.MemberRepo,
        MemberCache:      infra.MemberCache,
        Profiles:         foundation.ProfileService,
        AlarmCRUD:        alarmMode.AlarmCRUD,
        HolodexService:   foundation.HolodexService,
        YouTubeService:   ytStack.GetService(),
        StatsRepo:        ytStack.GetStatsRepo(),
        ActivityLogger:   ProvideActivityLogger(logger),
        SettingsService:  sharedmodules.BuildSettingsService(cfg.Notification.AdvanceMinutes, cfg.Scraper.ProxyEnabled, logger),
        ACLService:       aclService,
        TemplateAdminSvc: BuildTemplateAdminService(infra, templateRenderer, logger),
        Cleanup:          infra.Cleanup,
    }, nil
}
```

### 새 파일: `services_runtime_alarm_worker.go`

중요: alarm-worker는 message adapter / template renderer / member matcher / worker pool / bot deps를 만들지 않습니다.

```go
package bootstrap

import (
    "context"
    "log/slog"

    "github.com/kapu/hololive-shared/pkg/config"
)

func InitAlarmWorkerInfrastructure(ctx context.Context, cfg *config.Config, logger *slog.Logger) (_ *AlarmWorkerInfrastructure, retErr error) {
    infra, err := InitInfraResources(ctx, cfg, logger)
    if err != nil {
        return nil, err
    }

    defer func() {
        if retErr != nil {
            infra.Cleanup()
        }
    }()

    foundation, err := InitScraperHolodexFoundation(ctx, cfg, infra, logger)
    if err != nil {
        return nil, err
    }

    alarmRepo := ProvideAlarmRepository(infra.Postgres, logger)
    alarmMode, err := InitAlarmModeComponents(
        ctx,
        cfg,
        infra,
        foundation.HolodexService,
        foundation.MemberServiceAdapter,
        alarmRepo,
        logger,
    )
    if err != nil {
        return nil, err
    }

    return &AlarmWorkerInfrastructure{
        Cache:          infra.Cache,
        HolodexService: foundation.HolodexService,
        ChzzkClient:    alarmMode.ChzzkClient,
        TwitchClient:   alarmMode.TwitchClient,
        AlarmCRUD:      alarmMode.AlarmCRUD,
        Cleanup:        infra.Cleanup,
    }, nil
}
```

---

## PR-03D. runtime builder를 dedicated initializer로 전환

### 수정 파일

- `hololive/hololive-kakao-bot-go/internal/app/build_admin_api_runtime.go`
- `hololive/hololive-kakao-bot-go/internal/app/build_alarm_worker_runtime.go`
- `hololive/hololive-kakao-bot-go/internal/app/runtime.go`

### 패치 1: bot runtime은 `InitBotInfrastructure(...)` 사용

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/runtime.go b/hololive/hololive-kakao-bot-go/internal/app/runtime.go
@@
 func BuildRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*BotRuntime, error) {
@@
-    runtime, cleanup, err := InitializeBotRuntime(ctx, cfg, logger)
+    runtime, cleanup, err := InitializeBotRuntime(ctx, cfg, logger)
@@
 }
```

이 파일 자체의 호출 루트는 그대로 두되, `InitializeBotRuntime(...)` 내부가 `InitBotInfrastructure(...)`를 사용하도록 바꿉니다.

### 패치 2: admin-api runtime

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/build_admin_api_runtime.go b/hololive/hololive-kakao-bot-go/internal/app/build_admin_api_runtime.go
@@
-    infra, err := appbootstrap.InitCoreInfrastructure(ctx, cfg, logger)
+    infra, err := appbootstrap.InitAdminAPIInfrastructure(ctx, cfg, logger)
     if err != nil {
         return nil, fmt.Errorf("build admin api runtime: init core infrastructure: %w", err)
     }

-    runtimeViews := buildBotRuntimeDependencyViews(infra)
-    adminDeps, err := buildBotAdminServerDependencies(ctx, cfg, runtimeViews.adminRuntime, nil, logger)
+    adminDeps, err := buildAdminServerDependencies(ctx, cfg, infra, nil, logger)
     if err != nil {
         infra.Cleanup()
         return nil, fmt.Errorf("build admin api runtime: admin dependencies: %w", err)
     }
@@
-    if runtimeViews.serverRuntime.alarmCRUD != nil {
-        alarmAPI := alarmsvc.NewAPIHandler(runtimeViews.serverRuntime.alarmCRUD, logger)
+    if infra.AlarmCRUD != nil {
+        alarmAPI := alarmsvc.NewAPIHandler(infra.AlarmCRUD, logger)
         internalAlarm := router.Group("")
         internalAlarm.Use(middleware.APIKeyAuthMiddleware(cfg.Server.APIKey))
         alarmAPI.RegisterInternalRoutes(internalAlarm)
     }
```

### 패치 3: alarm-worker runtime

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/build_alarm_worker_runtime.go b/hololive/hololive-kakao-bot-go/internal/app/build_alarm_worker_runtime.go
@@
-    infra, err := appbootstrap.InitCoreInfrastructure(ctx, cfg, logger)
+    infra, err := appbootstrap.InitAlarmWorkerInfrastructure(ctx, cfg, logger)
     if err != nil {
         return nil, fmt.Errorf("build alarm worker runtime: init core infrastructure: %w", err)
     }
@@
-    scheduler, err := buildRuntimeAlarmScheduler(runtimeRoleWorker, cfg, infra, logger, os.Getenv(notificationSchedulerRoleEnv))
+    scheduler, err := buildAlarmWorkerRuntimeScheduler(runtimeRoleWorker, cfg, infra, logger, os.Getenv(notificationSchedulerRoleEnv))
     if err != nil {
         infra.Cleanup()
         return nil, fmt.Errorf("build alarm worker runtime: scheduler: %w", err)
     }
@@
-        ConfigSubscriber: BuildAlarmWorkerConfigSubscriber(ctx, infra.Deps.Cache, infra.AlarmCRUD, logger),
+        ConfigSubscriber: BuildAlarmWorkerConfigSubscriber(ctx, infra.Cache, infra.AlarmCRUD, logger),
```

### 추가 파일: `build_alarm_worker_scheduler.go`

```go
package app

import (
    "fmt"
    "log/slog"
    "strings"

    "github.com/kapu/hololive-shared/pkg/config"

    appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
)

func buildAlarmWorkerRuntimeScheduler(
    runtimeRole string,
    cfg *config.Config,
    infra *appbootstrap.AlarmWorkerInfrastructure,
    logger *slog.Logger,
    configuredRole string,
) (runtimeAlarmScheduler, error) {
    if !runtimeAllowsAlarmScheduler(runtimeRole, configuredRole) {
        if logger != nil {
            logger.Info(
                "Alarm runtime scheduler disabled for this runtime",
                slog.String("runtime_role", runtimeRole),
                slog.String("configured_role", strings.TrimSpace(configuredRole)),
            )
        }
        return nil, nil
    }

    scheduler, err := appbootstrap.NewAlarmWorkerRuntimeScheduler(
        cfg,
        infra.Cache,
        infra.HolodexService,
        infra.ChzzkClient,
        infra.TwitchClient,
        infra.AlarmCRUD,
        logger,
    )
    if err != nil {
        return nil, fmt.Errorf("build alarm worker runtime scheduler: %w", err)
    }
    return scheduler, nil
}
```

### 추가 파일: `bootstrap/alarm_worker_scheduler.go`

```go
package bootstrap

import (
    "errors"
    "fmt"
    "log/slog"

    "github.com/kapu/hololive-shared/pkg/config"
    "github.com/kapu/hololive-shared/pkg/domain"
    "github.com/kapu/hololive-shared/pkg/service/cache"
    "github.com/kapu/hololive-shared/pkg/service/holodex"

    alarmscheduler "github.com/kapu/hololive-kakao-bot-go/internal/service/alarm/scheduler"
    "github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
    "github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

func NewAlarmWorkerRuntimeScheduler(
    cfg *config.Config,
    cacheSvc cache.Client,
    holodexSvc *holodex.Service,
    chzzkClient *chzzk.Client,
    twitchClient *twitch.Client,
    alarmCRUD domain.AlarmCRUD,
    logger *slog.Logger,
) (RuntimeAlarmScheduler, error) {
    if cfg == nil {
        return nil, errors.New("new alarm worker runtime scheduler: config is nil")
    }
    if cacheSvc == nil {
        return nil, errors.New("new alarm worker runtime scheduler: cache is nil")
    }
    if holodexSvc == nil {
        return nil, errors.New("new alarm worker runtime scheduler: holodex service is nil")
    }
    if chzzkClient == nil {
        return nil, errors.New("new alarm worker runtime scheduler: chzzk client is nil")
    }
    if twitchClient == nil {
        return nil, errors.New("new alarm worker runtime scheduler: twitch client is nil")
    }
    if alarmCRUD == nil {
        return nil, errors.New("new alarm worker runtime scheduler: alarm CRUD is nil")
    }

    scheduler, err := alarmscheduler.NewRuntimeScheduler(
        cacheSvc,
        holodexSvc,
        chzzkClient,
        twitchClient,
        alarmCRUD,
        cfg.Notification,
        logger,
    )
    if err != nil {
        return nil, fmt.Errorf("new alarm worker runtime scheduler: %w", err)
    }

    return scheduler, nil
}
```

### 이유

- runtime마다 필요한 초기화 그래프가 달라진다
- admin-api / alarm-worker가 bot dependency graph를 더 이상 경유하지 않는다
- 이후 multi-module extraction의 난이도가 급격히 낮아진다

### 검증 grep

이 PR이 끝나면 아래 조건을 만족해야 합니다.

```bash
rg -n "InitCoreInfrastructure\(" hololive/hololive-kakao-bot-go
```

원하는 상태:

- `BuildRuntime(...)` 쪽만 남거나, 최종적으로 0건

---

# PR-04. shared YouTube builder를 API용 / scheduler용으로 분리

현재 `sharedmodules.BuildYouTubeStack(...)`는 YouTube service와 scheduler를 한 묶음으로 만듭니다.  
이 구조 때문에 bot/admin-api도 stream-ingester 소유 성격의 scheduler builder와 얽힙니다.

## PR-04A. API 전용 builder 추가

### 수정 파일

- 새 파일: `hololive/hololive-shared/pkg/providers/modules/youtube_api_stack.go`
- 수정 파일: `hololive/hololive-shared/pkg/providers/modules/youtube_stack.go`

### 새 파일: `youtube_api_stack.go`

```go
package modules

import (
    "context"
    "log/slog"

    "github.com/kapu/hololive-shared/pkg/config"
    "github.com/kapu/hololive-shared/pkg/providers"
    "github.com/kapu/hololive-shared/pkg/service/cache"
    "github.com/kapu/hololive-shared/pkg/service/youtube"
    "github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
    ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
)

type YouTubeAPIStackParams struct {
    YouTubeConfig   config.YouTubeConfig
    ScraperConfig   config.ScraperConfig
    CacheService    cache.Client
    StatsRepo       *ytstats.StatsRepository
    SharedRateLimit *scraper.RateLimiter
    Logger          *slog.Logger
}

func BuildYouTubeAPIStack(ctx context.Context, params YouTubeAPIStackParams) *providers.YouTubeStack {
    if !params.YouTubeConfig.EnableQuotaBuilding || params.YouTubeConfig.APIKey == "" {
        if params.Logger != nil {
            params.Logger.Info("YouTube quota building disabled; stats repository only")
        }
        return &providers.YouTubeStack{StatsRepo: params.StatsRepo}
    }

    svc, err := youtube.NewYouTubeService(ctx, params.YouTubeConfig.APIKey, params.CacheService, scraper.ProxyConfig{
        Enabled: params.ScraperConfig.ProxyEnabled,
        URL:     params.ScraperConfig.ProxyURL,
    }, params.SharedRateLimit, params.Logger)
    if err != nil {
        if params.Logger != nil {
            params.Logger.Warn("YouTube service init failed (optional feature)", slog.Any("error", err))
        }
        return &providers.YouTubeStack{StatsRepo: params.StatsRepo}
    }

    return &providers.YouTubeStack{
        Service:   svc,
        StatsRepo: params.StatsRepo,
    }
}
```

### 수정: `youtube_stack.go`

```diff
diff --git a/hololive/hololive-shared/pkg/providers/modules/youtube_stack.go b/hololive/hololive-shared/pkg/providers/modules/youtube_stack.go
@@
 func BuildYouTubeStack(ctx context.Context, params YouTubeStackParams) *providers.YouTubeStack {
-    if !params.YouTubeConfig.EnableQuotaBuilding || params.YouTubeConfig.APIKey == "" {
-        if params.Logger != nil {
-            params.Logger.Info("YouTube quota building disabled; stats repository only")
-        }
-        return &providers.YouTubeStack{StatsRepo: params.StatsRepo}
-    }
-
-    svc, err := youtube.NewYouTubeService(ctx, params.YouTubeConfig.APIKey, params.CacheService, scraper.ProxyConfig{
-        Enabled: params.ScraperConfig.ProxyEnabled,
-        URL:     params.ScraperConfig.ProxyURL,
-    }, params.SharedRateLimit, params.Logger)
-    if err != nil {
-        if params.Logger != nil {
-            params.Logger.Warn("YouTube service init failed (optional feature)", slog.Any("error", err))
-        }
-        return &providers.YouTubeStack{StatsRepo: params.StatsRepo}
-    }
+    stack := BuildYouTubeAPIStack(ctx, YouTubeAPIStackParams{
+        YouTubeConfig:   params.YouTubeConfig,
+        ScraperConfig:   params.ScraperConfig,
+        CacheService:    params.CacheService,
+        StatsRepo:       params.StatsRepo,
+        SharedRateLimit: params.SharedRateLimit,
+        Logger:          params.Logger,
+    })
+    if stack.Service == nil {
+        return stack
+    }

     scheduler := youtube.NewScheduler(
-        svc,
+        stack.Service,
         params.HolodexService,
         params.CacheService,
         params.StatsRepo,
@@
-    return &providers.YouTubeStack{
-        Service:   svc,
-        Scheduler: scheduler,
-        StatsRepo: params.StatsRepo,
-    }
+    stack.Scheduler = scheduler
+    return stack
 }
```

## PR-04B. bot/admin-api는 API stack만 사용

### 수정 파일

- `internal/app/bootstrap/services_runtime_admin.go`
- `internal/app/bootstrap/services_runtime_bot.go`
- `internal/app/bootstrap/services_alarm_stack.go`
- `internal/app/bootstrap/services_providers.go`
- `internal/bot/deps.go`
- `internal/app/wiring/container.go`
- `internal/app/container_accessors.go`

### 핵심 원칙

- bot는 `youtube.Service`와 `statsRepo`만 쓰고 `youtube.Scheduler`는 더 이상 들고 있지 않는다
- stream-ingester만 `BuildYouTubeStack(...)`의 full scheduler path를 유지한다

### 패치 1: `bot.Dependencies`에서 dead `Scheduler` 제거

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/bot/deps.go b/hololive/hololive-kakao-bot-go/internal/bot/deps.go
@@
     MembersData      member.DataProvider
     Service          youtube.Service
-    Scheduler        youtube.Scheduler
     YouTubeStatsRepo stats.StatsCommandRepository
@@
     membersData      member.DataProvider
     service          youtube.Service
-    scheduler        youtube.Scheduler
     youTubeStatsRepo stats.StatsCommandRepository
@@
         membersData:      d.MembersData,
         service:          d.Service,
-        scheduler:        d.Scheduler,
         youTubeStatsRepo: d.YouTubeStatsRepo,
     }
 }
```

### 패치 2: provider에서 scheduler 주입 제거

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/bootstrap/services_providers.go b/hololive/hololive-kakao-bot-go/internal/app/bootstrap/services_providers.go
@@
     var youTubeStatsRepo stats.StatsCommandRepository
     if statsRepo := modules.Stream.YTStack.GetStatsRepo(); statsRepo != nil {
         youTubeStatsRepo = statsRepo
     }

-    var (
-        youTubeService   = modules.Stream.YTStack.GetService()
-        youTubeScheduler = modules.Stream.YTStack.GetScheduler()
-    )
+    var youTubeService = modules.Stream.YTStack.GetService()
@@
         Matcher:          modules.Stream.MemberMatch,
         MembersData:      modules.Data.MembersData,
         Service:          youTubeService,
-        Scheduler:        youTubeScheduler,
         YouTubeStatsRepo: youTubeStatsRepo,
         Activity:         modules.Support.ActivityLogger,
```

### 패치 3: dead accessor 제거

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/wiring/container.go b/hololive/hololive-kakao-bot-go/internal/app/wiring/container.go
@@
-func YouTubeScheduler(deps *bot.Dependencies) youtube.Scheduler {
-    if deps == nil {
-        return nil
-    }
-    return deps.Scheduler
-}
```

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/container_accessors.go b/hololive/hololive-kakao-bot-go/internal/app/container_accessors.go
@@
-func (c *Container) GetYouTubeScheduler() youtube.Scheduler {
-    return appwiring.YouTubeScheduler(c.botDeps)
-}
```

### 이유

- scheduler ownership를 stream-ingester 쪽으로 되돌리는 첫 단계
- bot/admin-api에서 불필요한 builder/field 제거
- shared giant stack을 얇게 만드는 출발점

---

# PR-05. wrapper / alias layer 제거

이 단계는 correctness보다 유지보수 관점에서 중요합니다.  
지금 `internal/app/bootstrap_bot_dependency_views.go`는 runtime-specific abstraction이 아니라 wrapper 중복입니다.

## PR-05A. admin runtime용 wrapper를 `AdminAPIInfrastructure` 직접 사용으로 교체

### 수정 파일

- `internal/app/bootstrap_bot_admin.go`
- `internal/app/bootstrap_bot_dependency_views.go`
- `internal/app/build_admin_api_runtime.go`

### 새 시그니처

```go
func buildAdminServerDependencies(
    ctx context.Context,
    cfg *config.Config,
    infra *appbootstrap.AdminAPIInfrastructure,
    scraperScheduler *poller.Scheduler,
    logger *slog.Logger,
) (*appbootstrap.AdminServerDependencies, error)
```

### 패치

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_admin.go b/hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_admin.go
@@
-type botAdminServerDependencies = appbootstrap.AdminServerDependencies
-
 func buildBotAdminServerDependencies(
     ctx context.Context,
     cfg *config.Config,
-    deps botAdminRuntimeDependencies,
+    infra *appbootstrap.AdminAPIInfrastructure,
     scraperScheduler *poller.Scheduler,
     logger *slog.Logger,
-) (*botAdminServerDependencies, error) {
+) (*appbootstrap.AdminServerDependencies, error) {
@@
-    if deps.cache == nil || deps.postgres == nil {
-        return nil, errors.New("build bot admin server dependencies: admin dependency view is incomplete")
+    if infra == nil || infra.Cache == nil || infra.Postgres == nil {
+        return nil, errors.New("build admin server dependencies: admin infrastructure is incomplete")
     }

-    authService, err := buildBotAdminAuthService(ctx, cfg, deps, logger)
+    authService, err := buildBotAdminAuthService(ctx, cfg, infra, logger)
@@
-    settingsComponents := buildBotAdminSettingsComponents(cfg, deps, scraperScheduler, logger)
+    settingsComponents := buildBotAdminSettingsComponents(cfg, infra, scraperScheduler, logger)
@@
-    domainHandlers := buildBotAdminAPIHandlers(
-        deps,
+    domainHandlers := buildBotAdminAPIHandlers(
+        infra,
         scraperScheduler,
         settingsComponents.settingsApplier,
         settingsComponents.majorEventTriggerClient,
         systemCollector,
         logger,
     )

-    return &botAdminServerDependencies{
+    return &appbootstrap.AdminServerDependencies{
         DomainHandlers: domainHandlers,
         AuthHandler:    server.NewAuthHandler(authService, logger),
-        Cache:          deps.cache,
+        Cache:          infra.Cache,
     }, nil
 }
```

그 뒤 `deps.` 접근을 `infra.` 접근으로 치환합니다.

### PR-05B. `bootstrap_bot_dependency_views.go` 축소 또는 삭제

최종적으로 남겨야 할 것은 bot runtime에서 실제 필요한 세 묶음뿐입니다.

- webhook deps
- config subscriber deps
- config subscriber runtime deps

admin runtime 관련 view는 dedicated infra로 넘어가므로 삭제합니다.

### 패치 방향

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_dependency_views.go b/hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_dependency_views.go
@@
-type botAdminRuntimeDependencies struct {
-    ...
-}
-
-type botServerRuntimeDependencies struct {
-    ...
-}
-
 type botRuntimeDependencyViews struct {
     botDeps                 *bot.Dependencies
     webhook                 botWebhookRuntimeDependencies
     configSubscriber        botConfigSubscriberDependencies
     configSubscriberRuntime botConfigSubscriberRuntimeDependencies
-    adminRuntime            botAdminRuntimeDependencies
-    serverRuntime           botServerRuntimeDependencies
 }
@@
-func buildBotAdminRuntimeDependencies(...)
-func buildBotServerRuntimeDependencies(...)
```

그리고 `buildBotRuntimeDependencyViews(...)`에서 admin/server 관련 필드를 제거합니다.

### 이유

- compatibility wrapper 제거
- “bot 중심 view를 admin runtime이 재사용”하는 구조를 끊음
- 나중에 module extraction 시 import 그래프가 단순해짐

---

# PR-06. 문서 / 설정 / dead code 정합성 복구

이 PR은 기능보다 신뢰성 문제입니다.  
현재 문서는 실제 코드보다 앞서 있어서, 다음 작업자가 문서만 보고 잘못된 가정을 하게 됩니다.

## PR-06A. `hololive-shared/pkg/config/admin_api.go` 처리

현재 이 파일은 다음 문제가 있습니다.

- `ADMIN_API_PORT` 기본값이 `30002`
- 실제 `cmd/admin-api`는 `config.Load`를 사용하며 이 specialized loader를 쓰지 않음
- 테스트만 이 파일을 붙잡고 있음

### 선택지

#### 안전한 선택지(권장)
즉시 삭제하지 말고 **deprecated 표시 + 포트 정합성만 맞춤**

```diff
diff --git a/hololive/hololive-shared/pkg/config/admin_api.go b/hololive/hololive-shared/pkg/config/admin_api.go
@@
-// Copyright ...
+// Deprecated: current admin-api runtime uses config.Load().
+// This file remains only until dedicated admin module extraction is finished.
@@
-            Port:   sharedenv.Int("ADMIN_API_PORT", 30002),
+            Port:   sharedenv.Int("ADMIN_API_PORT", 30006),
```

#### 더 강한 선택지
Phase 7의 모듈 추출 직전에 파일과 관련 테스트를 삭제

### 이유

- 거짓 기본값 제거
- dead path를 명확히 표시
- 나중에 dedicated config loader를 다시 만들더라도 “현재 사용 여부”가 분명해짐

---

## PR-06B. 잘못된 완료 문서를 history로 이동하거나 배너를 붙인다

### 대상 문서

- `hololive-kakao-bot-go/docs/MULTIMODULE_MIGRATION_P3_PLAN.md`
- `hololive-kakao-bot-go/docs/SERVICE_DECOMPOSITION_ROADMAP.md`

### 권장 조치

1. `docs/history/` 하위로 이동
2. 문서 상단에 아래 배너 추가

```md
> 상태 주의:
> 이 문서는 “목표/설계 시점의 계획 문서”이다.
> 현재 저장소의 실제 모듈/디렉터리 구조는 이 문서의 `[x]` 표기와 다를 수 있다.
> 최신 실행 상태는 `docs/current/PROJECT_MAP.md`와 런타임 엔트리(`cmd/*`) 및 `go.work`를 기준으로 판단한다.
```

### grep 검증

```bash
rg -n "alarm-dispatcher|30002|hololive-admin/|hololive-alarm/" hololive/hololive-kakao-bot-go/docs
```

이 grep 결과가 남더라도, 적어도 **historical 문서임을 명확히 표시**해야 합니다.

---

# PR-07. 멀티모듈 추출 (선행 PR 전부 merge 후)

여기서부터가 진짜 “서비스별 독립 모듈”입니다.  
하지만 이 단계는 반드시 PR-01 ~ PR-06이 끝난 뒤에만 진행합니다.

## 7.1 왜 지금 바로 go.mod 분리를 하면 안 되는가

현재 admin-api / alarm-worker가 아직 다음에 묶여 있기 때문입니다.

- `internal/service/chzzk`
- `internal/service/twitch`
- `internal/service/activity`
- `internal/service/acl`
- `internal/service/notification`
- `internal/server`
- wrapper layer / bot-shaped infra 초기화

이 상태에서 곧바로 separate module로 찢으면, 결국 `internal` 패키지를 억지로 끌어내거나 import cycle 성격의 의존 정리를 한 번에 해야 합니다.  
실패 확률이 높습니다.

## 7.2 권장 최종 소유권

### 7.2.1 새 도메인 라이브러리 모듈: `hololive-alarm`

권장 디렉터리:

```text
hololive/
  hololive-alarm/
    go.mod
    pkg/
      domain/
      service/
      repository/
      cache/
```

### 여기로 옮길 것

- `hololive-kakao-bot-go/internal/service/notification/*`
- alarm CRUD / cache / platform mapping / dispatch state 관련 구현
- bot / admin / worker가 공통으로 참조하는 alarm domain 구현

### 여기로 옮기지 않을 것

- `internal/service/alarm/checker/*`
- `internal/service/alarm/scheduler/*`

이 둘은 worker runtime 전용 성격이 강하므로 `hololive-alarm-worker` 쪽으로 둡니다.

## 7.3 shared로 이동해도 되는 얇은 라이브러리

다음은 서비스가 아니라 library이므로 `hololive-shared`나 전용 library module로 이동 가능합니다.

- `internal/service/chzzk` -> `hololive-shared/pkg/service/chzzk`
- `internal/service/twitch` -> `hololive-shared/pkg/service/twitch`
- `internal/service/activity` -> `hololive-shared/pkg/service/activity`
- `internal/service/acl` -> `hololive-shared/pkg/service/acl`

단, 여기서 핵심은 **거대한 ownership을 shared에 더 넣지 않는 것**입니다.  
얇고 재사용적인 client/service만 이동합니다.

### 사전 패치: chzzk가 내부 errors에 의존하지 않도록 변경

현재 `chzzk/client.go`는 `internal/errors`를 import합니다.  
module extraction 전에 generic error type을 shared로 빼야 합니다.

권장 경로:

- `hololive-kakao-bot-go/internal/errors/errors.go`
- ->
- `hololive-shared/pkg/apperrors/errors.go`

### 패치 방향

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/service/chzzk/client.go b/hololive/hololive-kakao-bot-go/internal/service/chzzk/client.go
@@
-    apperrors "github.com/kapu/hololive-kakao-bot-go/internal/errors"
+    apperrors "github.com/kapu/hololive-shared/pkg/apperrors"
```

그 다음 `internal/errors` 전체를 shared로 이동하거나, 최소한 chzzk/twitch가 필요한 타입만 공용 package로 추출합니다.

## 7.4 admin-api 모듈 추출

권장 디렉터리:

```text
hololive/
  hololive-admin-api/
    go.mod
    cmd/admin-api/main.go
    internal/
      app/
      server/
      service/system/
      service/trigger/
```

### 1차 이동 원칙

여기서는 **flat package 구조를 그대로 옮기는 것**이 낫습니다.  
`internal/server`를 당장 domain subpackage로 쪼개는 것은 Go의 method/package 제약 때문에 생각보다 큰 refactor입니다.

따라서 1차는:

```bash
git mv hololive/hololive-kakao-bot-go/internal/server hololive/hololive-admin-api/internal/server
git mv hololive/hololive-kakao-bot-go/cmd/admin-api hololive/hololive-admin-api/cmd/admin-api
```

그리고 admin runtime에 필요한 `internal/app` 일부만 admin module로 이동합니다.

### 옮길 런타임 코드

- `internal/app/build_admin_api_runtime.go`
- admin runtime 관련 dependency builder
- 필요 시 `internal/app/http/*` 중 API router 관련 파일

### 남길 것

- bot 전용 runtime
- bot webhook / command wiring
- alarm worker runtime

## 7.5 alarm-worker 모듈 추출

권장 디렉터리:

```text
hololive/
  hololive-alarm-worker/
    go.mod
    cmd/alarm-worker/main.go
    internal/
      app/
      service/alarm/checker/
      service/alarm/scheduler/
```

### 이동 대상

```bash
git mv hololive/hololive-kakao-bot-go/cmd/alarm-worker hololive/hololive-alarm-worker/cmd/alarm-worker
git mv hololive/hololive-kakao-bot-go/internal/service/alarm/checker hololive/hololive-alarm-worker/internal/service/alarm/checker
git mv hololive/hololive-kakao-bot-go/internal/service/alarm/scheduler hololive/hololive-alarm-worker/internal/service/alarm/scheduler
```

그리고 runtime builder 쪽도 같이 이동합니다.

## 7.6 go.work 갱신

최종적으로는 아래처럼 되어야 합니다.

```diff
diff --git a/go.work b/go.work
@@
 use (
     ./
+    ./hololive/hololive-admin-api
+    ./hololive/hololive-alarm
+    ./hololive/hololive-alarm-worker
     ./hololive/hololive-dispatcher-go
-    ./hololive/hololive-kakao-bot-go
     ./hololive/hololive-llm-sched
     ./hololive/hololive-shared
     ./hololive/hololive-stream-ingester
     ./shared-go
 )
```

주의: `hololive-kakao-bot-go`를 바로 빼지 않습니다.  
bot가 남아 있으므로 유지합니다.

---

# 8. `internal/server`와 `internal/command`의 폴더 정리는 언제 할 것인가

업로드된 구조 방향에서는 `internal/server`와 `internal/command`를 도메인 폴더로 재정리하는 제안이 있습니다.  
방향 자체는 맞습니다.  
다만 **지금 즉시 “파일만 이동”하면 안 됩니다.**

이유는 Go의 package 제약 때문입니다.

- 지금 `internal/server`는 하나의 `APIHandler` 타입에 메서드가 파일별로 붙는 구조
- 디렉터리만 나눠서 다른 패키지로 옮기면 이 메서드 구조가 깨짐

따라서 이 작업은 **module extraction 이후**, 또는 **domain-specific handler struct를 새로 만든 뒤**에 해야 합니다.

## 권장 순서

1. 우선 `internal/server`를 admin-api module로 통째로 이동
2. 그 다음 아래처럼 domain-specific handler로 재구성

예:

```text
internal/adminapi/
  member/
    handler.go
  room/
    handler.go
  profile/
    handler.go
  stats/
    handler.go
  settings/
    handler.go
  template/
    handler.go
  stream/
    handler.go
  auth/
    handler.go
  runtime/
    router.go
    deps.go
```

이때는 공통 `BaseDeps`를 두고, 각 도메인 핸들러가 필요한 의존만 받도록 재작성합니다.

### 예시 skeleton

```go
package stream

type Handler struct {
    Logger            *slog.Logger
    Holodex           *holodex.Service
    YouTube           youtube.Service
    ValkeyCache       cache.Client
    StatsRepo         stats.StatsDashboardRepository
    MemberRepo        *member.Repository
    MemberIndexLoader func(context.Context) ([]*domain.Member, error)
    State             *sharedserver.StreamState
}
```

같은 방식으로 member/room/stats/settings를 각각 분리합니다.

`internal/command`도 동일하게, 단순 폴더 이동이 아니라 **registry interface 기반 분리**가 필요합니다.  
즉시성은 낮으므로 최종 단계에 두는 것이 맞습니다.

---

# 9. 테스트 / 검증 순서

이 환경에서는 `go1.26.2` toolchain이 없어 실제 `go test ./...`를 돌리지는 못했습니다.  
따라서 아래 순서로 로컬/CI에서 반드시 확인해야 합니다.

## PR-01 검증

```bash
rg -n "BOT_ADMIN_ENABLED|cfg\.Bot\.AdminEnabled" hololive/hololive-kakao-bot-go hololive/hololive-shared
go test ./hololive/hololive-kakao-bot-go/internal/app/...
go test ./hololive/hololive-kakao-bot-go/internal/app/bootstrap/...
```

기대 결과:

- grep 0건
- bot runtime 관련 테스트 green

## PR-02 검증

```bash
go test ./hololive/hololive-kakao-bot-go/internal/service/alarm/...
go test ./hololive/hololive-kakao-bot-go/internal/app/...
```

기대 결과:

- scheduler constructor가 concrete 구현에 의존하지 않음
- worker config subscriber 테스트 green

## PR-03 검증

```bash
rg -n "InitCoreInfrastructure\(" hololive/hololive-kakao-bot-go
go test ./hololive/hololive-kakao-bot-go/internal/app/...
go test ./hololive/hololive-kakao-bot-go/internal/app/bootstrap/...
```

기대 결과:

- `BuildAdminAPIRuntime` / `BuildAlarmWorkerRuntime`에서 `InitCoreInfrastructure` 제거
- dedicated infra initializer 기준으로 테스트 green

## PR-04 검증

```bash
rg -n "\.Scheduler\b" hololive/hololive-kakao-bot-go/internal/bot hololive/hololive-kakao-bot-go/internal/app -g '!**/*_test.go'
go test ./hololive/hololive-shared/pkg/providers/...
go test ./hololive/hololive-stream-ingester/internal/runtime/...
```

기대 결과:

- bot 쪽에서 dead YouTube scheduler field 접근 0건
- stream-ingester만 scheduler owner로 남음

## PR-06 검증

```bash
rg -n "alarm-dispatcher|30002|hololive-admin/|hololive-alarm/" hololive/hololive-kakao-bot-go/docs
```

기대 결과:

- 남아 있더라도 모두 history/deprecated banner 아래에만 존재

---

# 10. 최종 체크리스트

아래 항목이 모두 만족되어야 “이번 분리 계획이 코드 수준에서도 이행됐다”고 볼 수 있습니다.

## 반드시 만족해야 하는 조건

- [x] bot가 더 이상 admin route를 코드상으로도 제공하지 않는다
- [x] bot가 더 이상 alarm scheduler lifecycle을 소유하지 않는다
- [x] admin-api / alarm-worker가 `InitCoreInfrastructure(...)`를 사용하지 않는다
- [x] leaf constructor가 `*CoreInfrastructure` 전체를 받지 않는다
- [x] `internal/app` wrapper/alias layer가 대부분 제거된다
- [x] `BOT_ADMIN_ENABLED`가 코드와 Compose에서 사라진다
- [x] bot의 `NOTIFICATION_SCHEDULER_ROLE`이 `off` 또는 완전 제거 상태다
- [x] `internal/server`는 최소한 admin-api ownership 아래로 이동한다
- [x] 문서가 실제 코드 상태보다 앞서 있지 않다
- [x] `hololive-shared`에 거대 구현 소유권을 더 얹지 않는다

## 장기 조건

- [x] `hololive-alarm` domain library 또는 동등한 소유 모듈이 생긴다
- [x] admin-api / alarm-worker의 go.mod 추출이 끝난다
- [x] YouTube 구현 ownership가 `stream-ingester` 쪽으로 점진적으로 회수된다

---

# 11. 결론

2026-04-16 기준으로 이 플랜이 요구하던 구조 분리는 **실행 프로세스 분리 + 코드 경계 분리 + 멀티모듈 소유권 분리 + 문서 정합성**까지 마무리됐다.

최종 상태 요약:

1. bot는 ingress-only ownership 으로 고정됐다.
2. admin-api / alarm-worker는 runtime별 initializer 와 독립 go.mod 를 가진 별도 모듈이 됐다.
3. alarm domain 공용 ownership 은 `hololive-shared/pkg/service/notification` 으로 정렬됐다.
4. build/deploy/workspace/documentation surface 도 새 모듈 경계에 맞게 갱신됐다.

따라서 이 문서는 더 이상 open execution plan 이 아니라,
**runtime split 이 실제로 코드 구조까지 완결됐음을 설명하는 종료 기록**으로 보면 된다.
