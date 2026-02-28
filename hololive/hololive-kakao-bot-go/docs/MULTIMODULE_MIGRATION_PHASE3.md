# Phase 3: 서비스별 독립 모듈 생성 가이드

> Phase 0~2 완료 상태에서 이어지는 작업. 이 문서를 읽고 바로 구현을 시작할 수 있다.
> **상태 (2026-02-28)**: Phase 3 구현은 완료되었으며, 본 문서는 결정사항 참조용으로 유지한다. 실행 절차/체크리스트 최신본은 `MULTIMODULE_MIGRATION_P3_PLAN.md`를 기준으로 한다.

## 경로 정리 요약 (완료)

- `cmd/alarm-dispatcher` → `hololive-alarm/cmd/alarm-dispatcher`
- `cmd/admin-api` → `hololive-admin/cmd/admin-api`
- `cmd/llm-scheduler` → `hololive-llm-sched/cmd/llm-scheduler`
- `cmd/stream-ingester` → `hololive-stream-ingester/cmd/stream-ingester`
- 서비스별 Dockerfile은 각 모듈 루트로 이동 완료

## 현재 상태 (Phase 2 완료 기준)

### 구조

```
llm/
├── go.work                       # 멀티모듈 workspace
├── shared-go/                    # 기존 유지
├── hololive-shared/              # NEW (Phase 1~2에서 생성)
│   ├── go.mod                    # module github.com/kapu/hololive-shared
│   └── pkg/                      # 959 tests PASS, 32 packages
│       ├── domain/
│       ├── constants/
│       ├── errors/
│       ├── util/
│       ├── iris/
│       ├── health/
│       ├── config/
│       ├── platform/bootstrap/
│       ├── adapter/
│       ├── llm/
│       ├── repository/
│       └── service/{cache,database,member,holodex,template,configsub,
│                    ratelimit,notification,chzzk,twitch,matcher,alarm,
│                    youtube,membernews,delivery,settings,majorevent}/
└── hololive-kakao-bot-go/        # 381 tests PASS, 27 packages
    ├── cmd/{bot,alarm-dispatcher,admin-api,llm-scheduler,stream-ingester,tools}/
    └── internal/
        ├── app/                  # 24 files — bootstrap + providers + runtime
        ├── bot/                  # bot 전용 (command router, deps)
        ├── command/              # bot 전용 (chat command handlers)
        ├── server/               # bot + admin-api 공유 (API handlers)
        ├── assets/               # 미사용 (fonts embed)
        └── service/
            ├── acl/              # bot 전용
            ├── activity/         # bot 전용
            ├── auth/             # bot 전용 (admin-api도 사용)
            ├── configpub/        # admin-api 전용
            ├── system/           # bot 전용
            └── trigger/          # admin-api 전용
```

### 테스트 수치
- hololive-shared: 959 tests, 32 packages
- hololive-kakao-bot-go: 381 tests, 27 packages
- 합계: 1,340 tests (원본 1,055 대비 중복 원본 미삭제분 포함)

---

## 결정사항

### D1: providers.go 분할

**결정**: bot 전용 3개 함수만 bot-go 잔류, 나머지 ~40개 shared 이동

| 분류 | 함수 | 대상 |
|------|------|------|
| bot 전용 | `ProvideBotDependencies` | hololive-bot `internal/app/` |
| bot 전용 | `ProvideACLService` | hololive-bot `internal/app/` |
| bot 전용 | `ProvideActivityLogger` | hololive-bot `internal/app/` |
| 공유 (~40개) | `ProvideCacheResources`, `ProvideHolodexService`, ... | `hololive-shared/pkg/providers/` |

**구현 방법**:
1. `hololive-shared/pkg/providers/providers.go` 생성
2. providers.go에서 bot/acl/activity import가 없는 함수 전부 이동
3. bot-go에 `providers_bot.go` 남기고 나머지 삭제
4. 모든 bootstrap 파일에서 `app.Provide*` → `providers.Provide*` import 변경

### D2: app/ 파일 분배

**결정**: 공유 파일 shared 이동, 서비스 전용 bootstrap은 각 모듈로

| 파일 | 귀속 | 이유 |
|------|------|------|
| `api_router.go` | shared | ProvideHealthOnlyRouter/TriggerRouter는 다수 서비스 사용 |
| `ingestion_lock.go` | shared | stream-ingester + bot 사용 |
| `iris_sender_adapter.go` | shared | iris 관련 |
| `bootstrap_dispatcher.go` | hololive-alarm | alarm-dispatcher 전용 |
| `bootstrap_admin.go` | hololive-admin | admin-api 전용 |
| `bootstrap_llm_scheduler.go` | hololive-llm-sched | llm-scheduler 전용 |
| `bootstrap_llm.go` | hololive-llm-sched | llm 컴포넌트 조립 |
| `bootstrap_stream_ingester.go` | hololive-stream-ingester | stream-ingester 전용 |
| `bootstrap.go` | hololive-bot | bot 코어 인프라 init |
| `bootstrap_runtime.go` | hololive-bot | bot 런타임 조립 |
| `bootstrap_youtube.go` | hololive-bot | bot YouTube 컴포넌트 |
| `bootstrap_alarm.go` | hololive-bot | bot alarm 큐 조립 |
| `container.go` | hololive-bot | bot DI 컨테이너 |
| `runtime.go` | hololive-bot | BotRuntime |
| `runtime_providers.go` | hololive-bot | bot 전용 providers |
| `bootstrap_tools.go` | hololive-bot (또는 삭제) | tools 전용 |

### D3: server/ 분할

**결정**: 3분할 (shared / bot / admin)

| 파일 | 귀속 | 이유 |
|------|------|------|
| `h2c.go`, `logger_middleware.go`, `security_middleware.go`, `client_hints.go` | shared | 모든 서비스 공통 미들웨어 |
| `api_trigger.go`, `trigger_handler.go` | shared | llm-scheduler + admin-api |
| `settings_applier_local.go` | shared | bot + stream-ingester |
| `settings_applier_pubsub.go` | hololive-admin | admin-api 전용 |
| `api.go` (APIHandler) | hololive-bot | bot API 핸들러 |
| `api_member.go`, `api_stream.go`, `api_alarm.go`, `api_stats.go`, `api_milestone.go`, `api_template.go`, `api_settings.go`, `api_log.go` | hololive-bot | bot REST API |
| `api_auth.go`, `auth_handler.go` | hololive-admin 또는 shared | admin + bot 공유 가능 |

---

## Phase 3 실행 순서

### P3-0: providers.go 분할 + app/ 공유 파일 shared 이동
1. `hololive-shared/pkg/providers/` 생성
2. providers.go → 공유 함수 이동 (bot 전용 3개만 잔류)
3. `api_router.go`, `ingestion_lock.go`, `iris_sender_adapter.go` → shared 이동
4. `hololive-shared/pkg/server/` 생성 — 공통 미들웨어 + trigger 이동
5. 빌드 검증

### P3-1: hololive-alarm 모듈 (가장 단순)
```
hololive-alarm/
├── go.mod     # require hololive-shared, shared-go
├── cmd/alarm-dispatcher/main.go
└── internal/
    └── app/
        └── bootstrap_dispatcher.go
```
- `hololive-kakao-bot-go/cmd/alarm-dispatcher/` → 이동
- `bootstrap_dispatcher.go` → 이동, `app.Provide*` → `providers.Provide*` 변경
- go.mod: `replace ../hololive-shared`, `replace ../shared-go`

### P3-2: hololive-admin 모듈
```
hololive-admin/
├── go.mod
├── cmd/admin-api/main.go
└── internal/
    ├── app/bootstrap_admin.go
    └── service/
        ├── configpub/
        └── trigger/
```
- `settings_applier_pubsub.go` → hololive-admin 내부
- `auth/` → shared 또는 admin (bot에서도 사용 여부 재확인)

### P3-3: hololive-llm-sched 모듈
```
hololive-llm-sched/
├── go.mod
├── cmd/llm-scheduler/main.go
└── internal/
    └── app/
        ├── bootstrap_llm_scheduler.go
        └── bootstrap_llm.go
```

### P3-4: hololive-stream-ingester 모듈
```
hololive-stream-ingester/
├── go.mod
├── cmd/stream-ingester/main.go
└── internal/
    └── app/
        └── bootstrap_stream_ingester.go
```

### P3-5: hololive-bot 모듈 (가장 복잡, 마지막)
```
hololive-bot/
├── go.mod
├── cmd/bot/main.go
└── internal/
    ├── app/    (bootstrap.go, runtime.go, container.go, ...)
    ├── bot/
    ├── command/
    ├── server/ (bot 전용 API handlers)
    └── service/{acl,activity,auth,system}/
```

### P3-6: go.work 최종 업데이트 + hololive-kakao-bot-go 제거

```go
use (
    ./shared-go
    ./hololive-shared
    ./hololive-bot
    ./hololive-alarm
    ./hololive-admin
    ./hololive-llm-sched
    ./hololive-stream-ingester
    ./game-bot-go
    ./mcp-llm-server-go
    ./admin-dashboard/backend
)
```

---

## 원본 삭제 대기 목록 (Phase 2 잔존)

Phase 2에서 shared로 이동 완료됐으나 원본이 아직 남아있는 디렉토리.
Phase 3 시작 전 삭제 권장:

```bash
rm -rf hololive-kakao-bot-go/internal/service/{cache,database,member,holodex,template,configsub,ratelimit,notification,chzzk,twitch,matcher,alarm,youtube,membernews,delivery,settings,majorevent}
rm -rf hololive-kakao-bot-go/internal/{health,config,platform}
```

**주의**: 원본 삭제 후 빌드 실패 시 남은 import 경로를 `hololive-shared` 경로로 교체.

---

## 리스크 및 주의사항

1. **ProvideMessageStack**: `MessageStack` struct가 `providers.go`에 정의됨 — 함께 shared로 이동 필요
2. **DefaultPollerIntervals**: 동일 파일 내 도우미 함수 — 같이 이동
3. **resolveAlarmAdvanceMinutes**: alarm bootstrap에서 사용 — 같이 이동
4. **auth 패키지**: bot의 APIHandler와 admin-api 양쪽에서 사용 가능성 → 실제 import 확인 후 결정
5. **assets/**: 현재 어디서도 import되지 않음 → Phase 3에서 삭제 검토
6. **providers_test.go**: 테스트도 분할 필요 (bot 전용 테스트 vs 공유 테스트)
7. **circular dep 방지**: 서비스 모듈 → hololive-shared만 의존, 역방향 금지
