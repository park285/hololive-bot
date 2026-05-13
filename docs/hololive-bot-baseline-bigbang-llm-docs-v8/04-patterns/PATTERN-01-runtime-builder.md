# PATTERN-01 — Runtime builder 분리

## 적용 대상

`BuildRuntime`, `BuildAdminAPIRuntime`, bootstrap component builder처럼 client 생성, service 생성, handler 조립, cleanup-on-error가 한 함수에 섞인 경우.

## 분리 원칙

큰 함수 하나를 아래 helper로 나눕니다.

```go
func validateRuntimeInputs(...) error
func buildRuntimeClients(...) (*runtimeClients, error)
func buildRuntimeServices(...) (*runtimeServices, error)
func buildRuntimeHandlers(...) (*runtimeHandlers, error)
func assembleRuntime(...) *Runtime
func (r *runtimeResources) close(logger *slog.Logger)
```

## 주의

- cleanup order를 바꾸지 않습니다.
- nil guard를 유지합니다.
- error wrapping message를 유지합니다.
- mode별 client 생성 조건을 유지합니다.
