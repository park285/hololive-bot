# Hololive Bot 서비스 분리 로드맵

> **상태 주의: 역사적/폐기 문서**
> 이 문서는 과거 계획/참조 기록으로만 유지되며 현재 저장소 구조나 완료 상태와 다를 수 있다.
> 최신 기준은 `docs/current/PROJECT_MAP.md`, 현재 runtime entrypoint, `go.work`이다.

> 45K LOC 단일 바이너리를 단계적으로 분해하기 위한 전체 로드맵.
> 마지막 업데이트: 2026-03-07
> 참고: 본 문서의 일부 파일 경로 예시는 분리 이전 경로를 포함합니다. 최신 경로 매핑은 `MULTIMODULE_MIGRATION_P3_PLAN.md`의 **경로 정리 결과 (최종)** 섹션을 기준으로 합니다.

---

## 진행 상태

| Phase | 내용 | 상태 | 비고 |
|-------|------|------|------|
| P0 | 서비스 인터페이스 정의 | ✅ 완료 | 4개 인터페이스 + 컴파일 검증 |
| P1 | Bootstrap 모듈화 | ✅ 완료 | 581줄 → 6개 파일 분할 |
| P2 | Config 분할 | ✅ 완료 | 21개 Provide 함수 시그니처 분할 + bot.Dependencies Config 제거 |
| P3 | Alarm Dispatcher 분리 | ✅ 완료 | alarm-dispatcher 바이너리 + HTTP 클라이언트 + ~2,800줄 checker 제거 |
| P4 | Admin API 분리 | ✅ 완료 | admin-api 바이너리 + Valkey Pub/Sub 제어면 + Bot 슬리밍 |
| P5 | LLM Scheduler 분리 | ✅ 완료 | llm-scheduler 바이너리 + Bot 스케줄러/Delivery 분리 + Admin 트리거 경로 전환 + 운영 Runbook |
| P6 | YouTube Producer 분리 | ✅ 완료 | youtube-producer 바이너리 신설 + bot ingestion 코드 제거 + 분산 limiter/ingestion lock 적용 |

---

## 설계 결정

| 결정 | 선택 | 근거 |
|------|------|------|
| Alarm 도메인 소유권 | alarm-dispatcher 전체 소유 | 단일 소유자 원칙. DB/Valkey alarm 쓰기 한 서비스에 응집 |
| Bot의 Alarm 접근 | internal HTTP API 클라이언트 | Docker 내부 ~1ms. 분산 모놀리스 회피 |
| 제어면 프로토콜 | 혼합: 설정=Valkey Pub/Sub, 트리거=HTTP | 설정=비동기 브로드캐스트, RunNow=동기 응답 |
| 인프라 통합 | 현행 분리 유지 (Rust=Podman, Go=Docker) | 공유 네트워크 + Valkey/Postgres 연결 |
| AlarmManager 인터페이스 | 소비자별 3개 분리 | fat interface 회피. ISP 준수 |
| MessageSender | iris.Client + delivery.MessageSender 재사용 | 신규 인터페이스 불필요 |
| 큐 봉투 버전 | supported_versions 체크 + 로깅 | 독립 배포 시 호환성 보장 |

---

## 목표 아키텍처

```
┌─ Rust ──────────────────────────────────────────────┐
│  scraper-app: RSS → major_events DB                  │
│  alarm-app:   폴링 → dedup → LPUSH queue             │
│  dispatcher-app: BRPOP → 렌더 → Iris 발송             │
└──────────────────────────────────────────────────────┘
              ↓ alarm:dispatch:queue (Rust 내부 소비)
┌─ Go ────────────────────────────────────────────────┐
│                                                      │
│  hololive-bot                                        │
│    Iris 웹훅 → 커맨드 라우팅 → 응답                    │
│    Alarm CRUD API (/internal/alarm/*)                │
│                                                      │
│  admin-api (P4)                                      │
│    REST API + Auth + WebSocket                       │
│    설정 → Pub/Sub, 트리거 → HTTP                      │
│                                                      │
│  llm-scheduler (P5)                                  │
│    MajorEvent/MemberNews + Delivery                  │
│    /internal/trigger/* (수동 트리거 수신)              │
│                                                      │
│  youtube-producer (P6 ✅)                              │
│    YouTube 통계 + 스크래핑 + PhotoSync                 │
└──────────────────────────────────────────────────────┘
```

---

## P0: 서비스 인터페이스 정의 ✅

### 산출물

| 파일 | 변경 |
|------|------|
| `internal/domain/interfaces.go` | AlarmCRUD, AlarmDispatchState, AlarmChecker, StreamProvider |
| `internal/bot/deps.go` | Holodex→StreamProvider, Alarm→AlarmCRUD |
| `internal/command/command.go` | Holodex→StreamProvider, Alarm→AlarmCRUD |
| `internal/app/bootstrap.go` | coreInfrastructure에 concrete 참조 분리 |
| `internal/app/container.go` | GetAlarmService→domain.AlarmCRUD, GetHolodexService→domain.StreamProvider |
| `internal/app/providers.go` | ~~ProvideAlarmQueueDispatcher~~ (M6에서 제거) |
| `docs/GO_RS_DOMAIN_BOUNDARY.md` | Go/Rust 도메인 경계 문서 |

### 인터페이스 매핑

| 인터페이스 | 소비자 | 구현체 |
|-----------|--------|--------|
| `AlarmCRUD` | Bot 커맨드, Admin API | `notification.AlarmService` |
| `AlarmDispatchState` | `youtube.Scheduler` | `notification.AlarmService` |
| `StreamProvider` | Bot 커맨드, Admin API | `holodex.Service` |

---

## P1: Bootstrap 모듈화 ✅

### 산출물

`bootstrap.go` (581줄) → 6개 파일:

| 파일 | 줄수 | 내용 |
|------|------|------|
| `bootstrap.go` | 198 | core infra structs, initInfraResources, initCoreInfrastructure, Initialize* |
| `bootstrap_runtime.go` | ~200 | buildBotRuntime, buildBotServer, buildBotConfigSubscriber, applyScraperProxyToggle |
| `bootstrap_youtube.go` | 41 | buildYouTubeComponents |
| `bootstrap_llm.go` | 97 | buildMajorEventComponents, buildMemberNewsComponents |
| `bootstrap_alarm.go` | 70 | initAlarmDependencies, initMemberNewsService |
| `bootstrap_tools.go` | 87 | CLI 도구 전용 (WarmMemberCache, DBIntegration, FetchProfiles) |

---

## P2: Config 분할 ✅

### 목적

`*config.Config` 전체를 받던 Provide 함수들의 시그니처를 필요한 서브구조체/스칼라만 받도록 변경.
P3에서 alarm-dispatcher 바이너리 분리 시 각 Provide 함수의 config 의존성이 명시적이어야 함.

### 산출물

| 파일 | 변경 |
|------|------|
| `internal/app/providers.go` | 17개 Provide 함수 시그니처 변경 (`*config.Config` → 서브구조체/스칼라) |
| `internal/app/runtime_providers.go` | `ProvideSystemCollector` (`ServicesConfig`), `ProvideAuthService` (`autoPrepareSchema bool`) |
| `internal/app/bootstrap.go` | 10개 콜사이트 서브구조체 분해 전달 |
| `internal/app/bootstrap_alarm.go` | `initAlarmDependencies` + `initMemberNewsService` 시그니처 분할 |
| `internal/app/bootstrap_runtime.go` | 8개 콜사이트 업데이트 |
| `internal/app/bootstrap_youtube.go` | `buildYouTubeComponents` → `scraperCfg config.ScraperConfig` |
| `internal/app/bootstrap_llm.go` | `buildMajorEventComponents` 시그니처 단순화 (Go scraper 제거 이후 cfg 의존 제거) |
| `internal/bot/deps.go` | `Config *config.Config` 제거 → `BotSelfUser`, `IrisBaseURL`, `Notification` 3개 필드 |
| `internal/bot/bot.go` | `config *config.Config` 제거 → 3개 필드, 참조 6곳 교체 |
| `internal/app/providers_test.go` | 27개 테스트 호출 시그니처 업데이트 |

### 변환 패턴

| 패턴 | Before | After | 대상 |
|------|--------|-------|------|
| 단일 서브구조체 | `cfg *config.Config` | `cfg config.XxxConfig` | IrisClient, HolodexAPIKeys, ChzzkClient, TwitchClient, ExaSearcher, SystemCollector |
| 스칼라 추출 | `cfg *config.Config` | `logDir string` | ActivityLogger, MessageStack |
| 2 서브구조체 | `cfg *config.Config` | `ytCfg, scraperCfg` | YouTubeStack |
| Cliproxy+LLM | `cfg *config.Config` | `cliproxy, llmCfg` | LLM 관련 8개 함수 |
| ConsensusLLM | `cfg *config.Config` | `config.ConsensusLLMConfig` | EventSummarizer, MemberNewsService |
| Bool/Slice 추출 | `cfg *config.Config` | `enabled bool` / `[]int` | AlarmQueueDispatcher, AlarmService, SettingsService, ACLService, AuthService |

### Scope 외 (P4+에서 처리)

- `ProvideAPIRouter` / `newAPIRouter` — 4개 서브구조체 혼합
- `Container.Config` — DI 루트
- `BotRuntime.Config` — buildBotRuntime 내부에서 ProvideAPIRouter 등 호출

---

## P3: Alarm Dispatcher 분리

### 새 바이너리
`cmd/alarm-dispatcher/main.go`

### 소유 범위
- AlarmQueueConsumer (BRPOP) + AlarmQueueDispatcher (render + send)
- AlarmService 전체 (CRUD + state marking + persistence)
- alarm.Repository (DB `alarms` 테이블)
- Valkey 키: `alarm:*`, `notified:*`

### Internal HTTP API

| 메서드 | 경로 | 설명 |
|--------|------|------|
| POST | `/internal/alarm/add` | AddAlarm |
| POST | `/internal/alarm/remove` | RemoveAlarm |
| GET | `/internal/alarm/room/:id` | GetRoomAlarmsWithTypes |
| POST | `/internal/alarm/clear` | ClearRoomAlarms |
| GET | `/internal/alarm/next-stream/:channelId` | GetNextStreamInfo |
| GET | `/internal/alarm/member-name/:channelId` | GetMemberNameWithFallback |
| PUT | `/internal/alarm/settings` | UpdateAlarmAdvanceMinutes |
| PUT | `/internal/alarm/room-name` | SetRoomName |
| PUT | `/internal/alarm/user-name` | SetUserName |
| GET | `/internal/alarm/keys` | GetAllAlarmKeys |
| GET | `/health` | liveness |
| GET | `/ready` | readiness |

### 신규 파일

| 파일 | 용도 |
|------|------|
| `cmd/alarm-dispatcher/main.go` | 바이너리 엔트리포인트 |
| `internal/service/alarm/client.go` | AlarmCRUD HTTP 클라이언트 |
| `internal/service/alarm/api.go` | internal HTTP API 핸들러 |
| `Dockerfile.alarm-dispatcher` | Docker 이미지 |
| `docker-compose.prod.yml` | 서비스 추가 |

### 수정 파일

| 파일 | 변경 |
|------|------|
| `alarm_service.go` | 생성자 optional deps (holodex/chzzk/twitch) |
| `alarm_queue_consumer.go` | `supportedVersions = []uint32{1}` 체크 |
| `bootstrap_alarm.go` | `InitAlarmDispatcherRuntime()` 추가 |
| `bot/bot.go` | AlarmCRUD HTTP 클라이언트 주입 |

### 큐 봉투 버전 체크
- `supportedVersions = []uint32{1}`
- 미지원 버전 → 로그 + 메트릭 + 스킵 (DLQ는 추후)

### 마이그레이션 (Zero Downtime)
1. alarm-dispatcher 배포 (BRPOP + internal API 활성)
2. Bot/Admin → alarm-dispatcher HTTP 클라이언트 전환
3. Bot의 AlarmQueueDispatcher 제거, `GO_ALARM_QUEUE_CONSUMER_ENABLED=false`
4. Bot에서 AlarmService 인스턴스 제거

---

## P4: Admin API 분리 ✅

### 새 바이너리
`cmd/admin-api/main.go` (포트 30002)

### 제어면 설계

**설정 변경 (비동기 — Valkey Pub/Sub)**:
```
admin-api (configpub.Publisher)
  → PUBLISH config:update {"type":"scraper_proxy","payload":{"enabled":true}}
  → Bot (configsub.Subscriber) → applyScraperProxyToggle() + settings.Update()
  → alarm-dispatcher (configsub.Subscriber) → UpdateAlarmAdvanceMinutes()
```

**수동 트리거 (동기 — HTTP 프록시)**:
```
admin-api (trigger.Client)
  → POST llm-scheduler:30003/internal/trigger/majorevent-weekly
  → POST llm-scheduler:30003/internal/trigger/majorevent-monthly
```
- 409 Conflict → `majorevent.ErrNotificationInProgress` 매핑

**Alarm API**: alarm-dispatcher internal API로 프록시 (`alarm.Client`).

**Settings API**: `SettingsApplier` 인터페이스로 추상화
- Bot: `localSettingsApplier` — YouTube/Holodex/ScraperScheduler 직접 in-process 적용
- admin-api: `pubsubSettingsApplier` — Valkey PUBLISH + alarm-dispatcher HTTP

### 신규 파일

| 파일 | 용도 |
|------|------|
| `cmd/admin-api/main.go` | 바이너리 엔트리포인트 |
| `internal/config/admin_api.go` | AdminAPIConfig + LoadAdminAPI() |
| `internal/app/bootstrap_admin.go` | AdminAPIRuntime + BuildAdminAPIRuntime() |
| `internal/server/api_trigger.go` | TriggerHandler (Bot 내부 트리거 수신) |
| `internal/server/settings_applier_local.go` | Bot용 localSettingsApplier |
| `internal/server/settings_applier_pubsub.go` | admin-api용 pubsubSettingsApplier |
| `internal/service/configsub/subscriber.go` | Valkey Pub/Sub 설정 구독자 |
| `internal/service/configpub/publisher.go` | Valkey Pub/Sub 설정 발행자 |
| `internal/service/trigger/client.go` | Bot 트리거 HTTP 프록시 클라이언트 |
| `Dockerfile.admin-api` | Docker 이미지 |

### 수정 파일

| 파일 | 변경 |
|------|------|
| `internal/server/api.go` | `SettingsApplier` 인터페이스 추가, `APIHandler`에 `settingsApplier` 필드 |
| `internal/server/api_settings.go` | private 메서드 → SettingsApplier 위임, 3개 메서드 삭제 |
| `internal/app/runtime.go` | `APIHandler`/`AdminRouter` 제거, `ServerAddr`/`HttpServer` 리네이밍 |
| `internal/app/bootstrap_runtime.go` | `buildAdminServer` → `buildBotServer`, `ProvideBotRouter` 사용 |
| `internal/app/api_router.go` | `ProvideBotRouter` 추가 (webhook + trigger + health만) |
| `internal/app/runtime_providers.go` | `ProvideTriggerHandler`, `ProvideAPIHandler` 시그니처 변경 |
| `internal/app/bootstrap_dispatcher.go` | ConfigSubscriber 통합 |
| `docker-compose.prod.yml` | admin-api 서비스 추가 |
| `Makefile` | `build-admin-api` 타겟 추가 |

### Bot 슬리밍 (Phase 3)

BotRuntime에서 제거된 필드:
- `APIHandler *server.APIHandler`
- `AdminRouter *gin.Engine`

리네이밍:
- `AdminAddr` → `ServerAddr`
- `AdminServer` → `HttpServer`
- `StartAdminServer()` → `StartHTTPServer()`
- `ShutdownAdminServer()` → `ShutdownHTTPServer()`

Bot 라우터 (`ProvideBotRouter`): webhook + `/internal/trigger/*` + `/health` + `/metrics`만 등록.
Admin API 라우트 (members, alarms, rooms, stats, settings, templates, auth 등)는 admin-api 전용.

### 검증 결과
- `go build ./...` — bot, admin-api, alarm-dispatcher 3 바이너리 성공
- `make fmt` — 0 issues
- `make lint` — 0 issues
- `make test` — 1024 tests passed (51 packages)

---

## P5: LLM Scheduler 분리 ✅

### 새 바이너리
`cmd/llm-scheduler/main.go`

### Internal trigger endpoint

| 메서드 | 경로 | 설명 |
|--------|------|------|
| POST | `/internal/trigger/majorevent-weekly` | SendWeeklyNotification |
| POST | `/internal/trigger/majorevent-monthly` | SendMonthlyNotification |
| POST | `/internal/trigger/membernews-weekly` | SendWeeklyDigest |

### 설정 수신
`SUBSCRIBE config:update` → LLM 관련 설정 반영
- `membernews_weekly_run_now` (`{}`) → 멤버뉴스 주간 다이제스트 즉시 실행

> 2026-03-01 이후 MajorEvent 스크래핑 제어(`majorevent_scrape_*`)는 `llm-scheduler` 런타임 소유로 정리되어 bot 운영 API에서 제거됨.

### 신규 파일
- `cmd/llm-scheduler/main.go`
- `Dockerfile.llm-scheduler`
- `internal/app/bootstrap_llm_scheduler.go`
- `internal/config/llm_scheduler.go`
- `internal/service/trigger/client_test.go`
- `docs/LLM_SCHEDULER_RUNBOOK.md`

### 수정 파일
- `internal/app/runtime.go` (BotRuntime에서 Delivery/MajorEvent/MemberNews 스케줄러 제거)
- `internal/app/bootstrap_runtime.go` (Bot 조립 경로에서 LLM/Delivery 구성 제거)
- `internal/config/admin_api.go` (`LLM_SCHEDULER_INTERNAL_URL` 설정 추가)
- `internal/app/bootstrap_admin.go` (Admin trigger proxy 대상 llm-scheduler로 전환)
- `internal/service/trigger/client.go` (Bot 전용 클라이언트 → scheduler 전용 클라이언트 일반화)
- `internal/server/api_settings.go` + `internal/app/api_router.go` (`POST /api/holo/settings/llm` 추가)
- `docker-compose.prod.yml` (llm-scheduler 서비스 추가, admin-api env 전환)
- `Makefile` (`build-llm-scheduler` 추가)

### 검증 결과
- `go test ./...` ✅
- `go build ./...` ✅
- 운영 Runbook(장애/재실행/수동 trigger 절차) 작성 완료 (`docs/LLM_SCHEDULER_RUNBOOK.md`)

---

## P6: YouTube Producer 분리 ✅

- **전제조건**: 분산 rate limiter (Valkey sliding window)
- YouTube 통계 + 스크래핑 + PhotoSync
- 위험: 높음
- 남은 작업 추적: `docs/P6_REMAINING_TASKS.md`

### 사전 작업 (2026-02-27)
- 분산 슬라이딩 윈도우 레이트 리미터 초안 구현
  - `internal/service/ratelimit/sliding_window.go`
  - `internal/service/ratelimit/sliding_window_test.go`
- Holodex API 경로에 분산 레이트 리미터 연결
  - `internal/service/holodex/api_client.go` (`waitForRateLimiter`에 distributed limiter 연동)
  - `internal/service/holodex/service.go` (서비스 초기화 시 limiter 주입)
  - `internal/constants/constants.go` (`HolodexDistributedRateLimitConfig`)
  - `internal/service/holodex/api_client_test.go` (distributed limiter 대기/거부 테스트)
- YouTube producer 경로에 분산 레이트 리미터 연결
  - `internal/service/youtube/scraper/client.go` (`WaitWithBucket`, URL bucket 정규화)
  - `internal/app/bootstrap*.go` (bot/admin-api/alarm-dispatcher sharedRL에 distributed limiter 주입)
  - `internal/constants/constants.go` (`YouTubeProducerDistributedRateLimitConfig`)
  - `internal/service/youtube/scraper/distributed_rate_limit_test.go`
- 운영/설계 문서
  - `docs/DISTRIBUTED_RATE_LIMITING.md`
- youtube-producer 런타임 분리 (1차)
  - `cmd/youtube-producer/main.go`
  - `internal/app/bootstrap_youtube_producer.go`
  - `Dockerfile.youtube-producer`
  - `docker-compose.prod.yml` (`youtube-producer` 서비스 추가)
  - `internal/app/bootstrap_runtime.go` / `internal/app/runtime.go` (bot ingestion 제거 완료)
- ingestion 분산 락 보강
  - `hololive-youtube-producer/internal/runtime/ingestionlease/lease.go` (SetNX + compare-and-expire renew + CompareAndDelete release)
  - `internal/service/cache/service.go` (`CompareAndExpire` CAS helper)
  - `hololive-youtube-producer/internal/runtime/ingestionlease/lease_test.go` / `internal/service/cache/compare_and_expire_test.go`
- 운영 Runbook
  - `docs/STREAM_INGESTER_RUNBOOK.md`
- 검증
  - `go test ./internal/service/ratelimit ./internal/service/holodex ./internal/service/youtube/scraper ./internal/app` ✅
  - `go build ./...` ✅
- 종료 결정
  - 2026-02-27: 운영 검증/롤백 리허설은 운영자 결정으로 필수 범위에서 제외
  - 2026-02-27: P6 상태 `✅ 완료` 승격

---

## BotRuntime 축소 로드맵

| BotRuntime 필드 | 제거 Phase | 이전 대상 |
|-----------------|-----------|----------|
| ~~`AlarmQueueDispatcher`~~ | P3 → M6 제거 | ~~alarm-dispatcher~~ (Rust dispatcher로 대체) |
| `AlarmService` | P3 | alarm-dispatcher (HTTP 클라이언트 교체) |
| `AdminRouter/AdminServer/APIHandler` | P4 ✅ | admin-api |
| `MajorEventScheduler` (×3) | P5 ✅ | llm-scheduler |
| `MemberNewsScheduler` (×2) | P5 ✅ | llm-scheduler |
| `DeliveryDispatcher` | P5 ✅ | llm-scheduler |
| `Scheduler/ScraperScheduler/PhotoSync/OutboxDispatcher` | P6 ✅ | youtube-producer |

**최종 hololive-bot**: Bot + webhookHandler + StreamProvider(읽기) + AlarmCRUD HTTP 클라이언트

---

## 위험 평가

| 위험 | Phase | 확률 | 영향 | 완화 |
|------|-------|------|------|------|
| alarm-dispatcher 장애 시 Bot 커맨드 불가 | P3 | 중간 | 높음 | health check + circuit breaker + 에러 메시지 |
| DB 커넥션 풀 합산 초과 | P3+ | 중간 | 중간 | 서비스별 max_conns 분할 |
| 큐 봉투 버전 불일치 | P3 | 낮음 | 중간 | supported_versions 체크 + 로깅 |
| Admin → 스케줄러 트리거 실패 | P5 | 중간 | 낮음 | HTTP 타임아웃 (30s) + 409 매핑 + 에러 응답 전달 |
| llm-scheduler 장애 시 정기/수동 알림 중단 | P5 | 중간 | 높음 | health check + restart 정책 + 수동 재실행 runbook (`docs/LLM_SCHEDULER_RUNBOOK.md`) |
| Pub/Sub 메시지 유실 (재시작 시) | P4 ✅ | 낮음 | 낮음 | 각 서비스 시작 시 settings 파일에서 초기 상태 로드 |
| 분산 rate limiter (P6) | P6 | 중간 | 중간 | Valkey sliding window(holodex + youtube producer 경로 적용) + youtube-producer 경로 확장 |
| Bot/youtube-producer 동시 ingestion 실행 | P6 | 낮음 | 중간 | bot ingestion 코드 제거 + youtube-producer 단독 ownership + 분산 락(`lock:ingestion:runtime`, compare-and-expire renew) |

---

## 검증 전략

### 각 Phase 공통
```bash
make build && make lint && make test
```

### P3 추가 검증
- alarm-dispatcher 단독 기동 → `/health`, `/ready` 통과
- LPUSH 테스트 메시지 → 처리 확인
- Bot에서 `!알람 추가 가우르구라` → internal API 경유 CRUD 정상 동작
- alarm-dispatcher 중단 시 Bot이 graceful 에러 반환 확인

### P4 추가 검증
- admin-api 단독 기동 → `/health` 통과
- admin-api `POST /api/holo/settings` → Valkey PUBLISH → Bot SUBSCRIBE → `applyScraperProxyToggle()` 확인
- admin-api `POST /api/holo/majorevent/trigger` → llm-scheduler `/internal/trigger/majorevent-weekly` → 200 OK
- admin-api `GET /api/holo/alarms` → alarm-dispatcher HTTP 프록시 → 정상 응답
- Bot 독립 운영 확인 (admin-api 미기동 시 webhook/scheduler 정상 작동)

### P5+ 추가 검증
- llm-scheduler → HTTP 트리거 수신 + delivery 발송 확인
- (선택) 24시간 병렬 운영 → 메트릭 비교
- 장애/재실행/수동 trigger 절차 점검 (`docs/LLM_SCHEDULER_RUNBOOK.md`)
- youtube-producer 분리 운영 점검 (`docs/STREAM_INGESTER_RUNBOOK.md`)
