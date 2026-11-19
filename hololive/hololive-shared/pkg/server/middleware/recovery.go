package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	sharedlog "github.com/park285/shared-go/v2/pkg/logging"
)

func RecoveryMiddleware(ctx context.Context, logger *slog.Logger) gin.HandlerFunc {
	log := logger
	if log == nil {
		log = slog.Default()
	}

	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logRecoveredPanic(ctx, log, c, recovered)

				if !c.Writer.Written() {
					abortWithError(c, http.StatusInternalServerError, "internal_error", "internal server error")

					return
				}

				c.Abort()
			}
		}()

		c.Next()
	}
}

func logRecoveredPanic(ctx context.Context, logger *slog.Logger, c *gin.Context, recovered any) {
	attrs := []slog.Attr{
		slog.Group("panic", slog.String("type", fmt.Sprintf("%T", recovered))),
		slog.String("stack", string(debug.Stack())),
	}

	if c != nil && c.Request != nil {
		attrs = append(attrs,
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.String("client_ip", c.ClientIP()),
		)
	}

	sharedlog.Error(panicLogContext(ctx, c), logger, "http.request.panic_recovered", "", attrs...)
}

func panicLogContext(ctx context.Context, c *gin.Context) context.Context {
	if c != nil && c.Request != nil {
		return c.Request.Context()
	}

	return ctx
}
