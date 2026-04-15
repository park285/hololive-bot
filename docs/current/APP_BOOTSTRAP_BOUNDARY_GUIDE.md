# App Bootstrap Boundary Guide

이 문서는 `hololive-kakao-bot-go/internal/app` 경계 분리의 **현재 상태**를 기록한다.

## 2026-04-15 상태

이번 정리로 아래 항목은 완료됐다.

- `internal/app/http/`
  - API router / middleware / route registration 구현이 전용 디렉터리로 이동했다.
  - `internal/app/api_router.go` 는 forwarding façade 역할만 남긴다.
- `internal/app/runtime/`
  - lifecycle / HTTP server / run loop 구현이 전용 helper seam 으로 분리됐다.
  - `internal/app/runtime*.go` 루트 파일은 façade / thin wrapper 역할만 남긴다.
- `internal/app/wiring/`
  - container assembly / accessor / dependency-view build 구현이 전용 helper seam 으로 분리됐다.
  - `container.go`, `container_accessors.go`, `bootstrap_bot_dependency_views.go` 는 façade / local shape adapter 역할만 남긴다.
- `internal/app/bootstrap/`
  - provider / core / service / bot helper 구현이 전용 디렉터리로 이동했다.
  - 루트 `bootstrap_*.go`, `providers_*.go` 파일은 thin wrapper / orchestration / local shape adapter 역할만 남긴다.
- `*_additional_test.go`
  - `internal/app` 하위의 임시 파일명은 모두 제거됐다.
  - 테스트 파일명은 행위/책임 중심 이름으로 재배치됐다.

즉, `internal/app` 의 즉시성 높은 경계 리스크는 더 이상 “HTTP만 분리된 상태”가 아니다.
현재는 **http / runtime / wiring / bootstrap 구현이 전용 seam 뒤로 숨겨지고, 루트 패키지는 façade 중심으로 얇아진 상태**다.

## 현재 경계

### `internal/app/http/`
- router construction
- middleware registration
- route exposure

### `internal/app/runtime/`
- start / stop / shutdown ordering
- HTTP server lifecycle helper
- run loop helper

### `internal/app/wiring/`
- container assembly helper
- accessor/helper exposure
- runtime dependency-view construction

### `internal/app/bootstrap/`
- provider assembly
- core/service bootstrap implementation
- bot runtime helper implementation

### `internal/app` 루트
- public façade
- local type shape
- bootstrap orchestration

## 이번 라운드에서 닫힌 위험

### 1. startup/shutdown 와 router/wiring 변경의 직접 결합
`runtime/` 와 `wiring/` seam 이 생기면서, 구현 변경은 전용 helper 파일에서 다루고 루트 파일은 forwarding façade 를 유지한다.

### 2. `*_additional_test.go` 누적
`internal/app` 하위에서 `*_additional_test.go` 가 0개가 되었다.
테스트 파일명은 `runtime_lifecycle_test.go`, `container_lifecycle_test.go` 같은 책임 중심 이름으로 바뀌었다.

### 3. HTTP 구현이 루트에 남아 있던 문제
HTTP router 관련 구현은 `internal/app/http/` 아래로 이동했고 루트에는 thin entrypoint 만 남았다.

### 4. bootstrap 구현이 루트에 남아 있던 문제
bootstrap helper 구현은 `internal/app/bootstrap/` 아래로 이동했고 루트에는 orchestration / wrapper / local shape helper 만 남았다.

## 남아 있는 장기 과제

아래는 현재 기준으로 “즉시 blocker” 가 아니라 다음 구조 패치에서 다뤄도 되는 장기 과제다.

- `bootstrap_services_types.go` 의 역할별 bundle 추가 세분화
- bootstrap orchestration 파일의 추가 축소

이 장기 과제는 신규 churn 이 다시 루트에 쌓일 때만 진행하면 된다.

## 검증 기준

현재 경계 상태는 아래 명령으로 검증한다.

```bash
cd hololive/hololive-kakao-bot-go
find internal/app -name '*_additional_test.go'
go test ./internal/app/... -count=1
```

종료 조건은 다음과 같다.

- `internal/app/http/`, `internal/app/runtime/`, `internal/app/wiring/`, `internal/app/bootstrap/` 이 실제 구현 seam 으로 존재
- 루트 `internal/app` 파일은 façade / orchestration 위주로 유지
- `*_additional_test.go` 가 0개
- `go test ./internal/app/... -count=1` 통과
