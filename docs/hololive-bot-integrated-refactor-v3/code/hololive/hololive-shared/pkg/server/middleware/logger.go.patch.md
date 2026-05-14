# Patch: logger.go

HTTP 로그 메시지 `"HTTP"`를 event 기반으로 변경합니다.

```go
import sharedlog "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
```

`logHTTPRequest`를 아래처럼 변경합니다.

```go
func logHTTPRequest(ctx context.Context, logger *slog.Logger, c *gin.Context, path string, latency time.Duration) {
    status := c.Writer.Status()
    level := httpLogLevel(status)
    reqCtx := c.Request.Context()
    if reqCtx == nil {
        reqCtx = ctx
    }
    if !logger.Enabled(reqCtx, level) {
        return
    }

    attrs := httpLogAttrs(c, path, status, latency)
    sharedlog.Log(reqCtx, logger, level, "http.request.completed", "http request completed", attrs...)
}
```

`httpLogAttrs`의 latency는 항상 `duration_ms`로 남깁니다.

```go
attrs = append(attrs, sharedlog.DurationMS(latency))
```
