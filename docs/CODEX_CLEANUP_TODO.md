# Codex 코드 정리 리팩토링

> 생성: 2026-03-05
> 기반: golangci-lint v2.10.1 + 수동 코드 리뷰 (6개 Go 모듈 전수 검사)
> 범위: #81~#100 split-file 시리즈에서 도입된 품질 이슈
> 결과: 431줄 추가, 861줄 삭제 (순 -430줄)

---

## 검증 결과 (전체 통과)

```
6개 모듈 golangci-lint: 0 issues
6개 모듈 go build:      전체 성공
6개 모듈 go test:       전체 통과
```

---

## P0: Lint 위반 — 5/5 완료

| ID | 모듈 | 수정 내용 |
|----|------|----------|
| L1 | bot | unused `applyAPIRouterMiddleware` 함수 제거 + import 정리 (`api_router_middleware.go`) |
| L2 | bot | gci 3건: `api_router.go` trailing newline, `bootstrap_bot.go` trailing newline, `providers_alarm_consumers.go` import 그룹 분리 |
| L3 | shared | `config.go` trailing newline 제거 |
| L4 | dispatcher | `grouping.go:26` rangeValCopy 128bytes → `for i := range` 인덱싱 |
| L5 | llm-sched | `repository.go:9,12` llm-sched↔hololive-shared import 사이 빈 줄 추가 |

---

## P1: Codex 안티패턴 제거 — 6/9 완료, 2 SKIP(정당), 1 미완료

### 완료 (6건)

| ID | 모듈 | 수정 내용 | 삭제 파일 |
|----|------|----------|----------|
| A1 | bot | 9개 `api_routes_*.go` → `api_routes.go` 1개 병합 | 9개 삭제 |
| A2 | bot | `container_accessors.go` 11개 getter nil guard 제거, `TestContainerGetters_ReturnNilWhenBotDepsMissing` 테스트 삭제 | — |
| A3 | bot | `bootstrap_bot_settings_applier.go` base nil guard 3건 제거 (ApplyScraperProxy, ApplyAlarmAdvanceMinutes, ScraperProxyRuntimeState), `TestBotSettingsApplier_NilBaseBranches` 테스트 삭제 | — |
| A4 | bot | `runtimeAlarmSchedulerBuilder` 타입, `defaultRuntimeAlarmSchedulerBuilder` 함수, `coreInfrastructure.runtimeAlarmSchedulerBuilder` 필드, `buildRuntimeAlarmScheduler` 함수, 관련 테스트 전체 제거. `BotRuntime.AlarmScheduler`는 nil로 유지 (runtime_start.go에서 nil skip) | `bootstrap_bot_runtime_alarm.go`, `bootstrap_bot_runtime_alarm_test.go` |
| A5 | bot | bootstrap 파일 3그룹 통합: admin(2→1), dependency_views(4→1), services_types(3→1) | `bootstrap_bot_admin_helpers.go`, `_views_types.go`, `_views_core.go`, `_views_runtime.go`, `_alarm_types.go`, `_modules_types.go` |
| A7 | llm-sched | `repository.go` 불가능한 `r == nil` receiver guard 2건 제거 (L198, L248). `r.pool == nil` 체크는 유지 (postgres nil 전달 가능) |  — |
| A8 | llm-sched | `repository_pgx.go` `q == nil || q.pool == nil` guard 3건 제거 (Exec, Query, QueryRow). `TestPGXMemberNewsQuerier_NilPoolErrors` 테스트 삭제 | — |

### SKIP (2건 — 분석 후 정당한 설계로 판단)

| ID | 모듈 | 사유 |
|----|------|------|
| A6 | bot | `init*`은 외부 자원 초기화(DB/cache 연결, side effect), `build*`는 이미 초기화된 의존성 조립(순수 조립). `Components/Dependencies/Stack` 접미사도 각각 다른 역할을 표현. 강제 통일 시 의미 손실 |
| A9 | llm-sched | `facade_filter.go`, `facade_summarizer.go`는 typed nil pointer→interface 변환을 수행하는 정당한 adapter. Go에서 `(*SourceValidator)(nil)`을 interface에 넣으면 non-nil이 되는 문제를 해결. 테스트(`TestFilterCandidates_TypedNilValidatorIsTreatedAsNil`)도 이를 검증 |

---

## P2: 중복 코드 제거 — 3/6 완료, 3 SKIP(정당)

### 완료 (3건)

| ID | 모듈 | 수정 내용 |
|----|------|----------|
| D2 | shared | `admin_api.go` envconfig default 태그 3건 제거 (LogLevel, AppVersion, CORSEnforce), `llm_scheduler.go` default 태그 5건 제거 (IrisBaseURL, LogLevel, BotPrefix, BotSelfUser, AppVersion). code-level fallback이 envconfig default보다 견고하므로 태그 쪽을 제거 |
| D3 | shared | `config.go` TrimSpace 이중호출 3건 제거: `strings.TrimSpace(envutil.String(...))` → `envutil.String(...)`, `stringutil.TrimSpace(envutil.String(...))` → `envutil.String(...)`. envutil.String()이 내부에서 이미 TrimSpace 수행 |

### SKIP (3건)

| ID | 모듈 | 사유 |
|----|------|------|
| D1 | shared | 3개 config builder(`buildConfig`, `buildAdminAPIConfig`, `buildLLMSchedulerConfig`)의 실질 공통 부분은 `loadValkeyConfig()`/`loadPostgresConfig()`/`loadTelemetryConfig()` 호출 3줄뿐이며, 이미 함수로 추출됨. 각 config가 다른 envconfig struct와 필드를 사용하므로 추가 공통 추출 시 가독성 저하 |
| D4 | shared | `config_parsers.go`의 `parseStringWithDefault(value, fallback)` 등은 이미 로드된 string 값을 파싱. `envutil.String(key, fallback)`은 환경변수 키를 조회. 시그니처와 용도가 상이 |
| D5 | bot | bootstrap 파일의 에러 래핑(`fmt.Errorf("provide database resources: %w", err)` 등)은 모두 호출 위치 context를 포함. Go에서 콜스택 없이 에러 체인으로 실패 지점을 특정하는 표준 패턴 |
| D6 | llm-sched | `writeThroughSubscribe`/`writeThroughUnsubscribe`(각 ~18줄)는 DB 작업 후 cache write-through를 담당. Subscribe/Unsubscribe에 인라인하면 DB↔Cache 관심사 분리가 깨짐 |

---

## P3: 구조 개선 — 0/3 완료, 2 SKIP(정당), 1 미완료

### SKIP (2건)

| ID | 모듈 | 사유 |
|----|------|------|
| S1 | bot | `memberNewsWeeklyRunNowTrigger` 인터페이스는 테스트 mock(`trackingMemberNewsRunNowTrigger`)이 구현. 인터페이스 유지 필수 |
| S2 | llm-sched | `memberNewsQuerier` 인터페이스는 `Repository.pool` 필드 타입으로 사용되어 pgx 직접 의존 격리 역할. 구현체 1개지만 의존성 격리 목적으로 유지 |

### 미완료 (1건)

| ID | 모듈 | 상태 | 설명 |
|----|------|------|------|
| S3 | shared | **TODO** | `admin_api.go`/`llm_scheduler.go`는 `envconfig.Process()`, `config.go`는 `envutil` 사용. envutil 기반으로 통일하면 config 아키텍처 전면 변경 필요 (envconfig struct 제거, builder 함수 재작성, 테스트 전면 수정). 별도 PR 권장 |

---

## 변경된 파일 목록 (37개)

### 수정 (13개)
| 파일 | 변경 |
|------|------|
| `hololive-dispatcher-go/internal/dispatch/grouping.go` | rangeValCopy 수정 |
| `hololive-kakao-bot-go/internal/app/api_router.go` | trailing newline 제거 |
| `hololive-kakao-bot-go/internal/app/api_router_middleware.go` | unused 함수 제거 + import 정리 |
| `hololive-kakao-bot-go/internal/app/bootstrap_bot.go` | trailing newline 제거 |
| `hololive-kakao-bot-go/internal/app/bootstrap_bot_admin.go` | helpers.go 병합 |
| `hololive-kakao-bot-go/internal/app/bootstrap_bot_dependency_views.go` | types+core+runtime 병합 |
| `hololive-kakao-bot-go/internal/app/bootstrap_bot_runtime_orchestration.go` | alarmScheduler 변수 제거 |
| `hololive-kakao-bot-go/internal/app/bootstrap_bot_settings_applier.go` | base nil guard 3건 제거 |
| `hololive-kakao-bot-go/internal/app/bootstrap_bot_settings_applier_additional_test.go` | nil base 테스트, alarm builder 테스트 삭제 |
| `hololive-kakao-bot-go/internal/app/bootstrap_services.go` | runtimeAlarmSchedulerBuilder 필드 제거 |
| `hololive-kakao-bot-go/internal/app/bootstrap_services_types.go` | alarm_types+modules_types 병합 |
| `hololive-kakao-bot-go/internal/app/container_accessors.go` | 11개 nil guard 제거 |
| `hololive-kakao-bot-go/internal/app/container_additional_test.go` | nil botDeps 테스트 삭제 |
| `hololive-kakao-bot-go/internal/app/providers_alarm_consumers.go` | import 그룹 분리 |
| `hololive-llm-sched/internal/service/membernews/repository.go` | import 정렬 + receiver nil guard 2건 제거 |
| `hololive-llm-sched/internal/service/membernews/repository_pgx.go` | q == nil guard 3건 제거 |
| `hololive-llm-sched/internal/service/membernews/repository_pgx_test.go` | nil pool 테스트 삭제 |
| `hololive-shared/pkg/config/admin_api.go` | envconfig default 태그 3건 제거 |
| `hololive-shared/pkg/config/config.go` | TrimSpace 이중호출 3건 + trailing newline 제거 |
| `hololive-shared/pkg/config/llm_scheduler.go` | envconfig default 태그 5건 제거 |

### 신규 (1개)
| 파일 | 내용 |
|------|------|
| `hololive-kakao-bot-go/internal/app/api_routes.go` | 9개 api_routes 파일 통합 |

### 삭제 (17개)
| 파일 | 사유 |
|------|------|
| `api_routes_alarm.go` | A1: api_routes.go로 병합 |
| `api_routes_member.go` | A1: api_routes.go로 병합 |
| `api_routes_room.go` | A1: api_routes.go로 병합 |
| `api_routes_majorevent.go` | A1: api_routes.go로 병합 |
| `api_routes_milestone.go` | A1: api_routes.go로 병합 |
| `api_routes_profile.go` | A1: api_routes.go로 병합 |
| `api_routes_settings.go` | A1: api_routes.go로 병합 |
| `api_routes_stats_stream.go` | A1: api_routes.go로 병합 |
| `api_routes_template.go` | A1: api_routes.go로 병합 |
| `bootstrap_bot_admin_helpers.go` | A5: bootstrap_bot_admin.go로 병합 |
| `bootstrap_bot_dependency_views_types.go` | A5: bootstrap_bot_dependency_views.go로 병합 |
| `bootstrap_bot_dependency_views_core.go` | A5: bootstrap_bot_dependency_views.go로 병합 |
| `bootstrap_bot_dependency_views_runtime.go` | A5: bootstrap_bot_dependency_views.go로 병합 |
| `bootstrap_bot_runtime_alarm.go` | A4: dead code 제거 |
| `bootstrap_bot_runtime_alarm_test.go` | A4: dead code 제거 |
| `bootstrap_services_alarm_types.go` | A5: bootstrap_services_types.go로 병합 |
| `bootstrap_services_modules_types.go` | A5: bootstrap_services_types.go로 병합 |

---

## 남은 작업

| ID | 모듈 | 작업 | 권장 |
|----|------|------|------|
| S3 | shared | envconfig vs envutil 통일 | 별도 PR. admin_api.go/llm_scheduler.go의 envconfig.Process() → envutil 개별 호출로 전환, envconfig struct 제거, builder 함수 재작성 |
