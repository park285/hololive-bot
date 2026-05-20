# P3-1 ~ P3-6: 서비스별 독립 모듈 분리 실행 플랜

> **상태 주의: 역사적/폐기 문서**
> 이 문서는 과거 계획/참조 기록으로만 유지되며 현재 저장소 구조나 완료 상태와 다를 수 있다.
> 최신 기준은 `docs/current/PROJECT_MAP.md`, 현재 runtime entrypoint, `go.work`이다.

> **선행 조건**: Phase 0~2 + P3-0 완료.
> 이 문서만 읽으면 바로 구현 가능하다. 메모리/이전 세션 컨텍스트 불필요.
> `MULTIMODULE_MIGRATION_PHASE3.md`는 **결정사항 참조용**으로 유지하고, 실제 구현 순서/체크는 이 문서를 기준으로 진행한다.
> **상태 업데이트 (2026-02-28)**: P3-1 ~ P3-6 구현/검증 완료. 현재 운영 구조는 `hololive-bot` + `hololive-alarm` + `hololive-admin` + `hololive-llm-sched` + `hololive-youtube-producer` + `hololive-shared` 다중 모듈 기준.

## 경로 정리 결과 (최종)

| 이전 경로 | 현재 경로 |
|---|---|
| `hololive-kakao-bot-go/cmd/alarm-dispatcher` | `hololive-alarm/cmd/alarm-dispatcher` |
| `hololive-kakao-bot-go/cmd/admin-api` | `hololive-admin/cmd/admin-api` |
| `hololive-kakao-bot-go/cmd/llm-scheduler` | `hololive-llm-sched/cmd/llm-scheduler` |
| `hololive-kakao-bot-go/cmd/youtube-producer` | `hololive-youtube-producer/cmd/youtube-producer` |
| `hololive-kakao-bot-go/Dockerfile.alarm-dispatcher` | `hololive-alarm/Dockerfile` |
| `hololive-kakao-bot-go/Dockerfile.admin-api` | `hololive-admin/Dockerfile` |
| `hololive-kakao-bot-go/Dockerfile.llm-scheduler` | `hololive-llm-sched/Dockerfile` |
| `hololive-kakao-bot-go/Dockerfile.youtube-producer` | `hololive-youtube-producer/Dockerfile` |
| `hololive-kakao-bot-go/internal/service/configpub` | `hololive-admin/internal/service/configpub` |
| `hololive-kakao-bot-go/internal/service/trigger` | `hololive-admin/internal/service/trigger` |

---

## 현재 상태 (P3-0 완료 기준)

```
llm/
├── go.work
├── shared-go/                       # 범용 유틸 (httpclient, workerpool, json, retry 등)
├── hololive-shared/                 # 공유 도메인+서비스+providers (959+ tests)
│   └── pkg/
│       ├── providers/               # ← P3-0에서 생성 (44 Provide* 함수)
│       ├── domain/ constants/ errors/ util/ iris/ health/ config/ llm/
│       ├── platform/bootstrap/ adapter/ repository/
│       └── service/{cache,database,member,holodex,template,configsub,
│                    ratelimit,notification,chzzk,twitch,matcher,alarm,
│                    youtube,membernews,delivery,settings,majorevent}
└── hololive-kakao-bot-go/           # 아직 5개 바이너리가 한 모듈에 공존
    ├── cmd/
    │   ├── bot/                     # 포트 30001
    │   ├── admin-api/               # 포트 30002
    │   ├── alarm-dispatcher/        # 포트 30010
    │   ├── llm-scheduler/           # 포트 없음 (cron worker)
    │   ├── youtube-producer/         # 포트 없음 (cron worker)
    │   ├── test_db_integration/     # 도구
    │   └── tools/                   # fetch_profiles, warm_member_cache
    ├── internal/
    │   ├── app/                     # 22 files — bootstrap + providers + runtime
    │   │   ├── providers_bot.go     # bot 전용 3개 (ProvideBotDependencies, ProvideACLService, ProvideActivityLogger)
    │   │   ├── providers_compat.go  # 호환 레이어 (type alias + var redirect → providers.*)
    │   │   ├── runtime_providers.go # ProvideBot, ProvideAPIHandler, ProvideAuthService 등
    │   │   ├── bootstrap.go         # initInfraResources, initCoreInfrastructure
    │   │   ├── bootstrap_runtime.go # buildBotRuntime
    │   │   ├── bootstrap_dispatcher.go
    │   │   ├── bootstrap_admin.go
    │   │   ├── bootstrap_llm_scheduler.go
    │   │   ├── bootstrap_llm.go
    │   │   ├── bootstrap_youtube_producer.go
    │   │   ├── bootstrap_alarm.go
    │   │   ├── bootstrap_youtube.go
    │   │   ├── bootstrap_tools.go
    │   │   ├── api_router.go        # ProvideAPIRouter, ProvideAPIServer, ProvideBotRouter 등
    │   │   ├── api_router_test.go
    │   │   ├── container.go         # Container DI
    │   │   ├── runtime.go           # BotRuntime struct + Run/Shutdown
    │   │   ├── runtime_shutdown_test.go
    │   │   ├── db_integration_runtime.go
    │   │   └── fetch_profiles_runtime.go
    │   ├── bot/                     # bot 전용 (command router, deps)
    │   ├── command/                 # bot 전용 (chat command handlers)
    │   ├── server/                  # 28 files — REST API + middleware
    │   └── service/
    │       ├── acl/                 # bot + admin-api 공유
    │       ├── activity/            # bot + admin-api 공유
    │       ├── auth/                # admin-api (+ bot APIHandler)
    │       ├── configpub/           # admin-api 전용
    │       ├── system/              # admin-api (APIHandler 시스템 상태)
    │       └── trigger/             # admin-api 전용
    └── Dockerfile.*                 # 5개 서비스별 Dockerfile
```

### 핵심 의존성 그래프

```
                    ┌──────────────────┐
                    │  hololive-shared │
                    │  (providers, domain, service/*)
                    └────────┬─────────┘
                             │
        ┌────────┬───────────┼───────────┬──────────┐
        ▼        ▼           ▼           ▼          ▼
   alarm-disp  admin-api  llm-sched  stream-ing   bot
   (가장 단순) (server/)  (cron)     (cron+lease) (가장 복잡)
```

순환 의존 금지: 서비스 모듈 → hololive-shared 단방향만 허용.

---

## 전략: 점진적 분리 (Strangler Fig)

모듈을 하나씩 분리하고 `hololive-kakao-bot-go`에서 해당 코드를 삭제한다.
각 단계 후 `go build ./...` + `go test` 검증.
**가장 의존성이 적은 서비스부터 분리**한다.

> **중요**: server/ 패키지는 bot과 admin-api가 공유한다 (api.go가 acl/activity/system import).
> 따라서 server/를 먼저 분리하지 않으면 admin-api를 독립 모듈로 뺄 수 없다.
> → **P3-1에서 server/ 공유 코드를 hololive-shared로 이동** 후 서비스 분리를 진행한다.

---

## P3-1: server/ 분할 + shared 이동

### 목표
`hololive-kakao-bot-go/internal/server/`의 공유 코드를 `hololive-shared/pkg/server/`로 이동.
bot과 admin-api 모두 `hololive-shared/pkg/server`를 import하게 되어 독립 모듈화 전제조건 충족.

### 이동 대상

| 파일 | 이동 위치 | 이유 |
|------|----------|------|
| `h2c.go`, `h2c_test.go` | `shared/pkg/server/` | HTTP/2 cleartext — 모든 서비스 공통 |
| `security.go`, `security_test.go` | `shared/pkg/server/` | 보안 헤더 미들웨어 — 공통 |
| `logger.go` | `shared/pkg/server/` | 요청 로깅 미들웨어 — 공통 |
| `websocket.go` | `shared/pkg/server/` | WebSocket 유틸 — 공통 |
| `client_hints.go` | `shared/pkg/server/` | UA 파싱 — 공통 |
| `ip_allowlist.go` | `shared/pkg/server/` | IP 필터 — 공통 |
| `auth.go` | `shared/pkg/server/` | 인증 미들웨어 (gin.HandlerFunc) — 공통 |
| `oauth_proxy.go` | `shared/pkg/server/` | OAuth 프록시 — 공통 |
| `settings_applier_local.go` | `shared/pkg/server/` | bot + youtube-producer 사용 |

### 잔류 (bot-go/internal/server/)

| 파일 | 이유 |
|------|------|
| `api.go` (APIHandler) | `acl`, `activity`, `system` import → bot 전용 |
| `api_alarm.go` | bot REST API |
| `api_auth.go` | `auth` service import → bot/admin 공유이지만 auth도 아래에서 처리 |
| `api_member.go`, `api_member_test.go` | bot REST API |
| `api_stream.go`, `api_stream_test.go` | bot REST API |
| `api_profile.go` | bot REST API |
| `api_stats.go` | bot REST API |
| `api_milestone.go` | bot REST API |
| `api_majorevent.go` | bot REST API |
| `api_template.go` | bot REST API |
| `api_room.go` | bot REST API |
| `api_settings.go` | bot REST API |
| `api_trigger.go`, `api_trigger_test.go` | trigger 핸들러 — bot에서 사용 |
| `settings_applier_pubsub.go` | `configpub` import → admin 전용 |

### 이동 결정이 필요한 파일

- **`api.go`** (`APIHandler`): `acl`/`activity`/`system` 3개 local service 의존.
  - **결정**: 잔류. acl/activity/system이 bot+admin 공유이므로 서비스 분리 시 이 3개 패키지도 shared 이동 필요하지만, 현재 범위 외.
- **`api_auth.go`**: `internal/service/auth` import. auth 서비스는 bot+admin 공유.
  - **결정**: 잔류. auth를 shared로 옮기면 이것도 이동 가능하지만 현재 범위 외.

### 작업 순서

```
1. hololive-shared/pkg/server/ 디렉토리 생성
2. 공통 파일 9개 + 테스트 2개 이동 (package명 server 유지)
3. bot-go/internal/server/ 잔류 파일에서 import 경로 변경
   (로컬 server 패키지가 shared/pkg/server를 embed하거나 직접 참조)
4. api_router.go: ProvideAPIRouter 등이 server 패키지 사용 → import 경로 갱신
5. go build ./... && go test ./hololive-shared/... && go test ./hololive-kakao-bot-go/...
```

### 검증

```bash
go build ./...
go test ./hololive-shared/pkg/server/...
go test ./hololive-kakao-bot-go/...
cd hololive-kakao-bot-go && golangci-lint run ./...
```

---

## P3-2: alarm-dispatcher 독립 모듈 (가장 단순)

### 목표

```
llm/hololive-alarm/
├── go.mod                          # module github.com/kapu/hololive-alarm
├── cmd/alarm-dispatcher/main.go
└── internal/app/
    └── bootstrap_dispatcher.go
```

### 작업

```
1. llm/hololive-alarm/ 디렉토리 + go.mod 생성
   - require: hololive-shared, shared-go
   - replace: ../hololive-shared, ../shared-go
2. hololive-kakao-bot-go/cmd/alarm-dispatcher/ → hololive-alarm/cmd/alarm-dispatcher/ 이동
3. hololive-kakao-bot-go/internal/app/bootstrap_dispatcher.go → hololive-alarm/internal/app/ 이동
   - package 변경 불필요 (app 유지)
   - providers_compat.go 의존 제거 → providers.* 직접 참조로 전환
4. Dockerfile.alarm-dispatcher: 빌드 컨텍스트 경로 변경
5. go.work에 ./hololive-alarm 추가
6. hololive-kakao-bot-go에서 alarm-dispatcher 관련 코드 삭제
7. go build ./hololive-alarm/... && go test ./hololive-alarm/...
```

### 의존성 확인

`bootstrap_dispatcher.go`는 hololive-shared 패키지만 import (alarm, cache, database, holodex, member, notification, template, configsub 등).
**hololive-kakao-bot-go/internal/** 패키지 import 없음** → 깔끔하게 분리 가능.

### 검증

```bash
go build ./hololive-alarm/...
go build ./hololive-kakao-bot-go/...  # alarm-dispatcher 삭제 후에도 나머지 정상
```

---

## P3-3: llm-scheduler 독립 모듈

### 목표

```
llm/hololive-llm-sched/
├── go.mod
├── cmd/llm-scheduler/main.go
└── internal/app/
    ├── bootstrap_llm_scheduler.go
    └── bootstrap_llm.go
```

### 작업

```
1. llm/hololive-llm-sched/ 디렉토리 + go.mod 생성
2. cmd/llm-scheduler/ 이동
3. bootstrap_llm_scheduler.go + bootstrap_llm.go 이동
   - providers_compat.go 의존 제거 → providers.* 직접 참조
   - server.TriggerHandler 참조 → shared/pkg/server/ 또는 인라인 구현
4. Dockerfile.llm-scheduler: 빌드 컨텍스트 변경
5. go.work 업데이트
6. hololive-kakao-bot-go에서 llm-scheduler 관련 코드 삭제
```

### 의존성 확인

`bootstrap_llm_scheduler.go` import:
- hololive-shared: config, providers, majorevent, membernews, delivery, configsub, template, cache, database, member 등
- `hololive-kakao-bot-go/internal/server`: `server.TriggerHandler` 참조
  → **P3-1에서 server/ 공유 코드를 shared로 이동해야 해결됨**
  → 또는 TriggerHandler를 shared/pkg/server/로 이동

### 검증

```bash
go build ./hololive-llm-sched/...
```

---

## P3-4: youtube-producer 독립 모듈

### 목표

```
llm/hololive-youtube-producer/
├── go.mod
├── cmd/youtube-producer/main.go
└── internal/app/
    └── bootstrap_youtube_producer.go
```

### 작업

```
1. llm/hololive-youtube-producer/ 디렉토리 + go.mod 생성
2. cmd/youtube-producer/ 이동
3. bootstrap_youtube_producer.go 이동
   - 이미 providers.* 직접 참조 사용 (P3-0에서 전환 완료)
   - youtube-producer internal ingestionlease package 직접 사용
4. Dockerfile.youtube-producer: 빌드 컨텍스트 변경
5. go.work 업데이트
6. hololive-kakao-bot-go에서 youtube-producer 관련 코드 삭제
```

### 의존성 확인

`bootstrap_youtube_producer.go` import:
- hololive-shared: config, constants, domain, providers, iris, adapter 등 — 모두 shared
- `hololive-kakao-bot-go/internal/` 참조 **없음** → 깔끔하게 분리 가능

### 검증

```bash
go build ./hololive-youtube-producer/...
```

---

## P3-5: admin-api 독립 모듈 (server/ 의존)

### 목표

```
llm/hololive-admin/
├── go.mod
├── cmd/admin-api/main.go
└── internal/
    ├── app/bootstrap_admin.go
    ├── server/
    │   └── settings_applier_pubsub.go
    └── service/
        ├── configpub/
        └── trigger/
```

### 작업

```
1. llm/hololive-admin/ 디렉토리 + go.mod 생성
2. cmd/admin-api/ 이동
3. bootstrap_admin.go 이동
   - server.* 참조 → hololive-shared/pkg/server/ (P3-1 이후)
   - configpub, trigger → hololive-admin/internal/service/로 이동
4. settings_applier_pubsub.go → hololive-admin/internal/server/ 이동
5. service/configpub/ → hololive-admin/internal/service/configpub/ 이동
6. service/trigger/ → hololive-admin/internal/service/trigger/ 이동
7. Dockerfile.admin-api: 빌드 컨텍스트 변경
8. go.work 업데이트
9. hololive-kakao-bot-go에서 admin-api 관련 코드 삭제
```

### 의존성 확인

`bootstrap_admin.go` import:
- hololive-shared: config, providers, alarm, cache, database, holodex, member, template, ratelimit, scraper 등
- `hololive-kakao-bot-go/internal/server`: `server.*` — **P3-1에서 shared 이동 필요**
- `hololive-kakao-bot-go/internal/service/configpub`: admin 전용 → admin으로 이동
- `hololive-kakao-bot-go/internal/service/trigger`: admin 전용 → admin으로 이동

### 주의

admin-api가 bot의 `server.APIHandler`, `server.AuthHandler`를 사용하는지 확인 필요.
`bootstrap_admin.go` 내에서 직접 라우터를 구성하므로 `api.go`(APIHandler) 자체는 불필요할 수 있음.
실제 import를 확인 후 결정.

### 검증

```bash
go build ./hololive-admin/...
```

---

## P3-6: bot 슬리밍 + 정리 (마지막)

### 목표

`hololive-kakao-bot-go`에 bot 전용 코드만 남긴다.

### 작업

```
1. providers_compat.go 삭제
   - P3-2~P3-5에서 모든 bootstrap가 providers.* 직접 참조로 전환됨
   - bot bootstrap.go도 providers.* 직접 참조로 전환
2. 불필요해진 파일 삭제:
   - Dockerfile.alarm-dispatcher, Dockerfile.llm-scheduler, Dockerfile.youtube-producer, Dockerfile.admin-api
   - cmd/alarm-dispatcher/, cmd/admin-api/, cmd/llm-scheduler/, cmd/youtube-producer/
   - service/configpub/, service/trigger/
   - server/settings_applier_pubsub.go (admin으로 이동 완료)
3. Phase 2 잔존 원본 삭제 (이미 shared로 복사 완료된 것들):
   rm -rf hololive-kakao-bot-go/internal/service/{cache,database,member,holodex,template,
     configsub,ratelimit,notification,chzzk,twitch,matcher,alarm,youtube,membernews,
     delivery,settings,majorevent}
   rm -rf hololive-kakao-bot-go/internal/{health,config,platform}
   rm -rf hololive-kakao-bot-go/internal/domain
   rm -rf hololive-kakao-bot-go/internal/constants
   rm -rf hololive-kakao-bot-go/internal/util
   rm -rf hololive-kakao-bot-go/internal/iris
   rm -rf hololive-kakao-bot-go/internal/llm
   rm -rf hololive-kakao-bot-go/internal/repository
   rm -rf hololive-kakao-bot-go/internal/adapter
   rm -rf hololive-kakao-bot-go/pkg/errors
4. go.work 최종 업데이트:
   use (
     ./shared-go
     ./hololive-shared
     ./hololive-kakao-bot-go    # bot 전용 (rename 가능)
     ./hololive-alarm
     ./hololive-admin
     ./hololive-llm-sched
     ./hololive-youtube-producer
     ./game-bot-go
     ./mcp-llm-server-go
     ./admin-dashboard/backend
   )
5. go build ./... && go test ./...
6. golangci-lint (각 모듈)
```

### 최종 구조

```
llm/
├── go.work
├── shared-go/
├── hololive-shared/
│   └── pkg/{providers,server,domain,service/*,...}
├── hololive-kakao-bot-go/          # bot 전용
│   ├── cmd/bot/
│   ├── cmd/tools/
│   └── internal/
│       ├── app/                    # bootstrap.go, runtime.go, container.go 등
│       ├── bot/
│       ├── command/
│       ├── server/                 # bot REST API handlers (api_*.go)
│       └── service/{acl,activity,auth,system}/
├── hololive-alarm/                 # alarm-dispatcher 전용
├── hololive-admin/                 # admin-api 전용
├── hololive-llm-sched/             # llm-scheduler 전용
├── hololive-youtube-producer/       # youtube-producer 전용
├── game-bot-go/
├── mcp-llm-server-go/
└── admin-dashboard/backend/
```

---

## 실행 순서 및 의존성

```
P3-1 (server/ 분할)
  │
  ├──→ P3-2 (alarm-dispatcher)     # server/ 비의존, 병렬 가능
  │
  ├──→ P3-4 (youtube-producer)      # server/ 비의존, 병렬 가능
  │
  ├──→ P3-3 (llm-scheduler)        # server.TriggerHandler 의존 → P3-1 후
  │
  └──→ P3-5 (admin-api)            # server/ + configpub/trigger 의존 → P3-1 후
          │
          └──→ P3-6 (bot 슬리밍)   # 모든 서비스 분리 완료 후
```

**병렬화 가능**:
- P3-1 완료 후, P3-2 + P3-4는 병렬 실행 가능 (서로 의존 없음)
- P3-3과 P3-5는 P3-1 완료 필요 (server/ shared 의존)

---

## TODO 체크리스트

### P3-1: server/ 분할
- [x] `hololive-shared/pkg/server/` 디렉토리 생성
- [x] 공통 미들웨어 9개 파일 이동 (h2c, security, logger, websocket, client_hints, ip_allowlist, auth, oauth_proxy, settings_applier_local)
- [x] 테스트 파일 2개 이동 (h2c_test, security_test)
- [x] bot-go/internal/server/ 잔류 파일 import 경로 갱신
- [x] api_router.go import 경로 갱신
- [x] `go build ./...` + `go test` 통과 확인
- [x] `golangci-lint` 0 issues

### P3-2: alarm-dispatcher 분리
- [x] `llm/hololive-alarm/` 디렉토리 + `go.mod` 생성
- [x] `go.work`에 `./hololive-alarm` 추가
- [x] `cmd/alarm-dispatcher/` 이동
- [x] `bootstrap_dispatcher.go` 이동 + providers_compat 의존 제거
- [x] Dockerfile 빌드 컨텍스트 변경
- [x] `go build ./hololive-alarm/...` 통과
- [x] bot-go에서 alarm-dispatcher 관련 코드 삭제
- [x] `go build ./hololive-kakao-bot-go/...` 통과 (삭제 후 검증)

### P3-3: llm-scheduler 분리
- [x] `llm/hololive-llm-sched/` 디렉토리 + `go.mod` 생성
- [x] `go.work`에 `./hololive-llm-sched` 추가
- [x] `cmd/llm-scheduler/` 이동
- [x] `bootstrap_llm_scheduler.go` + `bootstrap_llm.go` 이동
- [x] server.TriggerHandler 참조 해결 (shared 또는 인라인)
- [x] Dockerfile 빌드 컨텍스트 변경
- [x] `go build ./hololive-llm-sched/...` 통과
- [x] bot-go에서 llm-scheduler 관련 코드 삭제

### P3-4: youtube-producer 분리
- [x] `llm/hololive-youtube-producer/` 디렉토리 + `go.mod` 생성
- [x] `go.work`에 `./hololive-youtube-producer` 추가
- [x] `cmd/youtube-producer/` 이동
- [x] `bootstrap_youtube_producer.go` 이동 (이미 providers.* 직접 참조)
- [x] Dockerfile 빌드 컨텍스트 변경
- [x] `go build ./hololive-youtube-producer/...` 통과
- [x] bot-go에서 youtube-producer 관련 코드 삭제

### P3-5: admin-api 분리
- [x] `llm/hololive-admin/` 디렉토리 + `go.mod` 생성
- [x] `go.work`에 `./hololive-admin` 추가
- [x] `cmd/admin-api/` 이동
- [x] `bootstrap_admin.go` 이동
- [x] `service/configpub/`, `service/trigger/` 이동
- [x] `server/settings_applier_pubsub.go` 이동
- [x] Dockerfile 빌드 컨텍스트 변경
- [x] `go build ./hololive-admin/...` 통과
- [x] bot-go에서 admin-api 관련 코드 삭제

### P3-6: bot 슬리밍 + 최종 정리
- [x] `providers_compat.go` 삭제 (bootstrap.go → providers.* 직접 참조 전환)
- [x] 불필요 Dockerfile 삭제 (alarm-dispatcher, admin-api, llm-scheduler, youtube-producer)
- [x] Phase 2 잔존 원본 삭제 (internal/service/{cache,...}, internal/{health,config,platform,...})
- [x] `go.work` 최종 업데이트 (7개 hololive 모듈)
- [x] `go build ./...` 전체 통과
- [x] `go test ./...` 전체 통과
- [x] 각 모듈 `golangci-lint` 0 issues
- [x] 5개 바이너리 빌드 확인 (각 모듈의 cmd/)
- [ ] Docker 빌드 확인 (선택)

---

## 리스크 및 완화

| 리스크 | 완화 |
|--------|------|
| server/ 패키지 분할 시 순환 의존 | shared/pkg/server/는 hololive-shared만 import, bot/admin 역방향 금지 |
| api.go (APIHandler)가 acl/activity/system 의존 | APIHandler는 bot-go에 잔류, shared 이동 불가 |
| auth 서비스 공유 (bot + admin-api) | auth를 shared로 이동하면 깔끔하지만 현재 범위 외. bot-go에 잔류, admin이 필요 시 import |
| providers_compat.go 제거 시 호출부 깨짐 | P3-6에서 마지막에 일괄 전환 (안전) |
| go.mod 의존성 동기화 | `go mod tidy` 후 `go.work.sum` 재생성 |
| Dockerfile 빌드 컨텍스트 | 각 서비스 Dockerfile을 자체 디렉토리 기준으로 변경, `COPY go.work* ./` 패턴 유지 |
| rtk hook 간섭 (admin-dashboard generated hooks) | `find -exec` 대신 `grep -rl | xargs sed` 패턴 사용, 변경 범위를 `hololive-*` 모듈로 제한 |

---

## 각 단계별 예상 규모

| 단계 | 파일 이동 | 새 파일 | 삭제 | 난이도 |
|------|----------|---------|------|--------|
| P3-1 | ~11 | 0 | 0 | 중 (import 경로 변경 다수) |
| P3-2 | 2 | 1 (go.mod) | 2 | 하 |
| P3-3 | 3 | 1 (go.mod) | 3 | 하~중 (TriggerHandler 해결) |
| P3-4 | 2 | 1 (go.mod) | 2 | 하 |
| P3-5 | 5 | 1 (go.mod) | 5 | 중 (configpub/trigger 이동) |
| P3-6 | 0 | 0 | 30+ | 중 (잔존 원본 대량 삭제) |
