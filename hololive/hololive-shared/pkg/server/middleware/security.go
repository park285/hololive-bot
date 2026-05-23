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
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	sharedlog "github.com/park285/hololive-bot/shared-go/pkg/logging"

	"github.com/kapu/hololive-shared/pkg/constants"
)

// SecurityHeadersMiddleware 보안 헤더 추가 미들웨어
func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		// CSP는 SPA 환경에서 제한적으로 적용
		c.Header("Content-Security-Policy", "frame-ancestors 'none'")
		c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		c.Next()
	}
}

// RequestIDMiddleware 요청마다 고유 X-Request-ID를 생성/전파하는 미들웨어.
// 클라이언트가 이미 X-Request-ID를 보냈으면 그대로 사용한다.
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

// MaxBodySizeMiddleware 요청 본문 크기를 제한하는 미들웨어.
// 설정된 maxBytes를 초과하면 413 Payload Too Large를 반환한다.
func MaxBodySizeMiddleware(maxBytes int64) gin.HandlerFunc {
	if maxBytes <= 0 {
		maxBytes = constants.ServerConfig.MaxBodyBytes
	}
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}
