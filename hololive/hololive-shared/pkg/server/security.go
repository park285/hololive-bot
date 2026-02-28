package server

import (
	"github.com/gin-gonic/gin"
)

// SecurityHeadersMiddleware 보안 헤더 추가 미들웨어
func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		// CSP는 SPA 환경에서 제한적으로 적용
		c.Header("Content-Security-Policy", "frame-ancestors 'none'")
		c.Next()
	}
}
