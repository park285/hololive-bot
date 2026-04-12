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
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

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
