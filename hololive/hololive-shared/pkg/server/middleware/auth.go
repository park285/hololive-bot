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

// APIKeyAuthMiddleware: X-API-Key 헤더를 검증하는 인증 미들웨어를 반환합니다.
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

// NoRouteAuthHandler: 미등록 경로 접근 시 API Key를 검증하는 핸들러.
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
