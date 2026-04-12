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
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/contracts/common"
)

const (
	// APIKeyHeader: 하위 호환성을 위한 재수출. 실제 정의는 contracts/common 패키지에 있습니다.
	APIKeyHeader = common.APIKeyHeader //nolint:gosec // G101: 헤더 이름일 뿐 실제 credentials가 아님
)

// apiKey가 빈 문자열이면 인증을 건너뜁니다 (개발 환경용).
func APIKeyAuthMiddleware(apiKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if apiKey == "" {
			c.Next()
			return
		}

		providedKey := c.GetHeader(APIKeyHeader)
		if providedKey == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "API key required",
			})
			return
		}

		if subtle.ConstantTimeCompare([]byte(providedKey), []byte(apiKey)) != 1 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "forbidden",
				"message": "invalid API key",
			})
			return
		}

		c.Next()
	}
}

// API Key가 없으면 401, 잘못된 키면 403, 인증 성공해도 경로가 없으므로 404 반환.
// 크롤러/스캐너가 루트 경로 등에 접근할 때 서버 구조 노출을 방지합니다.
func NoRouteAuthHandler(apiKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// API Key가 설정되지 않은 경우 기본 404 반환 (개발 모드)
		if apiKey == "" {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": "endpoint not found",
			})
			return
		}

		providedKey := c.GetHeader(APIKeyHeader)
		if providedKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "API key required",
			})
			return
		}

		// 타이밍 공격 방지를 위해 constant-time 비교 사용
		if subtle.ConstantTimeCompare([]byte(providedKey), []byte(apiKey)) != 1 {
			c.JSON(http.StatusForbidden, gin.H{
				"error":   "forbidden",
				"message": "invalid API key",
			})
			return
		}

		// 인증 성공해도 경로가 없으므로 404 반환
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "endpoint not found",
		})
	}
}
