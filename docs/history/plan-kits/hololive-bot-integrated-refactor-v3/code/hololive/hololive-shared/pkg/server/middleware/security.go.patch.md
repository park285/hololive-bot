# Patch: security.go

`RequestIDMiddleware` import에 shared logging을 추가합니다.

```go
import (
    "net/http"

    "github.com/gin-gonic/gin"
    "github.com/google/uuid"
    sharedlog "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"

    "github.com/kapu/hololive-shared/pkg/constants"
)
```

함수는 아래처럼 교체합니다.

```go
func RequestIDMiddleware() gin.HandlerFunc {
    const headerKey = "X-Request-ID"
    return func(c *gin.Context) {
        reqID := c.GetHeader(headerKey)
        if reqID == "" {
            reqID = uuid.NewString()
        }

        c.Set("request_id", reqID)
        c.Request = c.Request.WithContext(sharedlog.WithRequestID(c.Request.Context(), reqID))
        c.Header(headerKey, reqID)

        c.Next()
    }
}
```
