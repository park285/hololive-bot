package server

import (
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

// NewIPAllowList: 허용된 IP 대역(CIDR) 목록을 파싱하여 IPNet 슬라이스를 생성합니다.
func NewIPAllowList(allowed []string) ([]*net.IPNet, error) {
	nets := make([]*net.IPNet, 0, len(allowed))
	for _, raw := range allowed {
		trimmed := stringutil.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if !strings.Contains(trimmed, "/") {
			if strings.Contains(trimmed, ":") {
				trimmed += "/128"
			} else {
				trimmed += "/32"
			}
		}
		_, cidr, err := net.ParseCIDR(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %s: %w", trimmed, err)
		}
		nets = append(nets, cidr)
	}
	return nets, nil
}

// AdminIPAllowMiddleware: 관리자 페이지 접근 시 클라이언트 IP가 허용 목록에 있는지 검사하는 미들웨어
// allowlist가 비어있으면 모든 IP를 허용합니다 (개발/테스트용).
func AdminIPAllowMiddleware(allowed []*net.IPNet, logger *slog.Logger) gin.HandlerFunc {
	if len(allowed) == 0 {
		if logger != nil {
			logger.Warn("Admin IP allowlist is empty; allowing all admin requests (configure ADMIN_ALLOWED_IPS for production)")
		}
		return func(c *gin.Context) {
			c.Next()
		}
	}
	return func(c *gin.Context) {
		clientIP := net.ParseIP(c.ClientIP())
		if clientIP == nil {
			logger.Warn("Invalid client IP")
			c.JSON(403, gin.H{"error": "forbidden"})
			c.Abort()
			return
		}
		for _, cidr := range allowed {
			if cidr.Contains(clientIP) {
				c.Next()
				return
			}
		}
		logger.Warn("Admin IP blocked", slog.String("ip", clientIP.String()))
		c.JSON(403, gin.H{"error": "forbidden"})
		c.Abort()
	}
}
