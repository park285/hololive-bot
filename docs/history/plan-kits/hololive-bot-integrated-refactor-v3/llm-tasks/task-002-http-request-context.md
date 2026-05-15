# Task 002. HTTP request context propagation

## 목표

`X-Request-ID`를 `gin.Context`와 `request.Context()` 양쪽에 넣는다.

## 수정 파일

- `hololive/hololive-shared/pkg/server/middleware/security.go`
- `hololive/hololive-shared/pkg/server/middleware/logger.go`

## 변경

`RequestIDMiddleware`에서 다음을 추가한다.

```go
c.Request = c.Request.WithContext(sharedlog.WithRequestID(c.Request.Context(), reqID))
```

HTTP 로그는 `http.request.completed` event를 사용한다.

## 검증

```bash
gofmt -w hololive/hololive-shared/pkg/server/middleware
go test ./hololive/hololive-shared/pkg/server/middleware
```
