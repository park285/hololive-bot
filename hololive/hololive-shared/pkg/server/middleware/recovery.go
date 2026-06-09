// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

// RecoveryMiddleware는 HTTP boundary에서 발생한 panic이 프로세스 밖으로 전파되지 않도록
// 공통 복구 정책을 적용합니다. ApplyBaseMiddleware가 이 미들웨어를 항상 설치하므로 신규
// gin.Engine 작성자가 gin.Recovery를 별도로 기억하지 않아도 됩니다.
func RecoveryMiddleware(logger *slog.Logger) gin.HandlerFunc {
	log := logger
	if log == nil {
		log = slog.Default()
	}

	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logRecoveredPanic(log, c, recovered)
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

func logRecoveredPanic(logger *slog.Logger, c *gin.Context, recovered any) {
	ctx := context.Background()
	attrs := []slog.Attr{
		slog.Any("panic", recovered),
		slog.String("stack", string(debug.Stack())),
	}

	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
		attrs = append(attrs,
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.String("client_ip", c.ClientIP()),
		)
	}

	logger.LogAttrs(ctx, slog.LevelError, "http.request.panic_recovered", attrs...)
}
