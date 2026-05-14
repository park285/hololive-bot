# Task 001. shared-go logging foundation

## 목표

`shared-go/pkg/logging`에 공통 event/context/operation 기반을 추가한다.

## 수정 파일

- `shared-go/pkg/logging/attrs.go`
- `shared-go/pkg/logging/context.go`
- `shared-go/pkg/logging/id.go`
- `shared-go/pkg/logging/log.go`
- `shared-go/pkg/logging/operation.go`
- `shared-go/pkg/logging/sanitize.go`

## 금지

- `hololive-admin-api` 수정 금지
- 새 logging framework 도입 금지
- 기존 `NewLogger`, `EnableFileLogging` API 삭제 금지

## 적용

`code/shared-go/pkg/logging/*`를 복사한다.

## 검증

```bash
gofmt -w shared-go/pkg/logging
go test ./shared-go/pkg/logging
```
