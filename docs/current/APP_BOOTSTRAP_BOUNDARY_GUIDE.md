# App Bootstrap Boundary Guide

이 문서는 `hololive-kakao-bot-go/internal/app` 의 비분리 구조를 줄이기 위한 현재 기준 가이드다.

## 왜 분리가 필요한가

2026-04-14 기준 `internal/app` 은 단일 디렉터리에 Go 파일 58개, 약 7002 LOC 가 모여 있다.
현재 문제는 단순히 파일 수가 많은 것이 아니라, 서로 다른 책임이 같은 패키지 안에 혼재돼 있다는 점이다.

한 디렉터리에 아래 책임이 동시에 들어 있다.

- DI / wiring
- runtime lifecycle
- HTTP router / API registration
- bot bootstrap
- service/provider bootstrap
- integration runtime
- test helper / wrapper

이 구조는 다음 문제를 만든다.

- 변경 영향 범위를 예측하기 어렵다.
- 신규 기능이 기존 bootstrap 파일에 계속 덧붙는다.
- 얇은 helper 복제와 `*_additional_test.go` 누적이 쉬워진다.
- 운영 장애 분석 시 startup/wiring/runtime 경로를 한 번에 모두 읽어야 한다.

## 목표 경계

권장 목표는 `internal/app` 을 아래 경계로 나누는 것이다.

### 1. `internal/app/bootstrap/`

역할:
- 외부 의존성 조립
- provider / service / module bootstrap
- bot/server 초기 wiring

이동 대상 후보:
- `bootstrap_bot*.go`
- `bootstrap_core*.go`
- `bootstrap_services*.go`
- `providers_alarm_consumers.go`
- `providers_single_consumer.go`

### 2. `internal/app/runtime/`

역할:
- process lifecycle
- start / stop / shutdown ordering
- background runtime orchestration

이동 대상 후보:
- `runtime*.go`
- `db_integration_runtime.go`
- `fetch_profiles_runtime.go`

### 3. `internal/app/http/`

역할:
- API router construction
- middleware registration
- route exposure

이동 대상 후보:
- `api_router*.go`
- `api_routes.go`

### 4. `internal/app/wiring/`

역할:
- container assembly
- accessor / dependency view exposure
- 외부에서 import 되는 최소 façade 제공

이동 대상 후보:
- `container.go`
- `container_accessors.go`
- `bootstrap_bot_dependency_views.go`
- `bootstrap_services_types.go`

## 실제 이동 순서

한 번에 대이동하지 말고 다음 순서로 나누는 것이 안전하다.

### 단계 1. helper 중복 제거

이미 이번 패치에서 반영한 내용:

- `internal/app/command_builder_clone.go` 제거
- `internal/bot/command_builder_clone.go` 의 `CloneCommandBuilders()` 재사용

이 단계의 목적은 가장 얇은 중복부터 줄여, 이후 경계 분리의 기준을 명확히 하는 것이다.

### 단계 2. façade 유지 + 내부 패키지 분리

`internal/app` 밖에서 바로 import 되는 공개 함수/타입은 당장 바꾸지 않는다.
먼저 내부 패키지로 구현을 이동시키고, `internal/app` 에서는 얇은 forwarding façade 만 남긴다.

예시:

```go
// internal/app/runtime.go
package app

import appruntime "github.com/kapu/hololive-kakao-bot-go/internal/app/runtime"

func (r *Runtime) Run(ctx context.Context) error {
    return appruntime.Run(ctx, r)
}
```

이 방식의 장점은 cmd / 테스트 / 외부 호출부의 import churn 을 줄일 수 있다는 점이다.

### 단계 3. 테스트 책임 재배치

`*_additional_test.go` 는 임시 방편으로는 유용하지만, 장기적으로는 책임 경계를 흐린다.
테스트 파일은 다음 기준으로 옮긴다.

- router 동작 테스트 → `internal/app/http`
- lifecycle / shutdown 테스트 → `internal/app/runtime`
- wiring / dependency graph 테스트 → `internal/app/bootstrap` 또는 `internal/app/wiring`

파일명도 `*_additional_test.go` 대신 실제 행위 중심 이름으로 바꾼다.

예시:
- `bootstrap_services_providers_additional_test.go`
  → `providers_resolution_test.go`
- `runtime_wrappers_additional_test.go`
  → `runtime_adapter_test.go`

### 단계 4. 대형 struct 정리

`bootstrap_services_types.go`, `container.go` 처럼 많은 의존성을 한 번에 담는 struct 는 다음 원칙으로 나눈다.

- runtime-only dependency
- HTTP-only dependency
- bot command dependency
- alarm/notification stack dependency

즉, “모든 것을 담는 container” 에서 “역할별 dependency bundle” 로 바꾼다.

## 권장 파일 매핑

### bootstrap 쪽
- `bootstrap_bot.go`
- `bootstrap_bot_admin.go`
- `bootstrap_bot_config_subscriber.go`
- `bootstrap_bot_runtime_alarm.go`
- `bootstrap_bot_runtime_orchestration.go`
- `bootstrap_bot_server.go`
- `bootstrap_bot_settings_applier.go`
- `bootstrap_bot_webhook_youtube.go`
- `bootstrap_core.go`
- `bootstrap_core_tools.go`
- `bootstrap_services.go`
- `bootstrap_services_alarm.go`
- `bootstrap_services_alarm_stack.go`
- `bootstrap_services_foundation.go`
- `bootstrap_services_integration.go`
- `bootstrap_services_llm_clients.go`
- `bootstrap_services_modules.go`
- `bootstrap_services_providers.go`
- `providers_alarm_consumers.go`
- `providers_single_consumer.go`

### runtime 쪽
- `runtime.go`
- `runtime_http_server.go`
- `runtime_logging.go`
- `runtime_runner.go`
- `runtime_shutdown.go`
- `runtime_start.go`
- `db_integration_runtime.go`
- `fetch_profiles_runtime.go`

### http 쪽
- `api_router.go`
- `api_router_middleware.go`
- `api_router_registration.go`
- `api_router_runtime_variants.go`
- `api_routes.go`

### wiring 쪽
- `container.go`
- `container_accessors.go`
- `bootstrap_bot_dependency_views.go`
- `bootstrap_services_types.go`

## 금지할 패턴

다음 패턴은 새 구조에서도 금지하는 것이 맞다.

1. 다른 패키지에 이미 있는 얇은 slice clone/helper 를 다시 정의하는 것
2. 새 기능을 `bootstrap_services_modules.go` 같은 기존 대형 파일 끝에 계속 덧붙이는 것
3. 테스트를 일단 `*_additional_test.go` 로만 추가하고 장기 정리를 미루는 것
4. runtime 책임과 DI 책임을 같은 파일에서 같이 수정하는 것

## 리뷰 체크리스트

구조 분리 PR 은 아래를 모두 만족해야 한다.

- public import path churn 이 최소화되었는가
- start/stop ordering 이 바뀌지 않았는가
- router registration 이 runtime wiring 과 분리되었는가
- dependency bundle 이 역할 기준으로 나뉘었는가
- 테스트 파일명이 행위 기준으로 재배치되었는가
