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
	"github.com/park285/hololive-bot/shared-go/pkg/stringutil"
)

func NewIPAllowList(allowed []string) ([]*net.IPNet, error) {
	nets := make([]*net.IPNet, 0, len(allowed))
	for _, raw := range allowed {
		trimmed := stringutil.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		trimmed = normalizeIPAllowListCIDR(trimmed)
		_, cidr, err := net.ParseCIDR(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %s: %w", trimmed, err)
		}
		nets = append(nets, cidr)
	}
	return nets, nil
}

func normalizeIPAllowListCIDR(raw string) string {
	if strings.Contains(raw, "/") {
		return raw
	}
	if strings.Contains(raw, ":") {
		return raw + "/128"
	}
	return raw + "/32"
}

// allowlist가 비어있으면 모든 IP를 허용합니다 (개발/테스트용).
func AdminIPAllowMiddleware(allowed []*net.IPNet, logger *slog.Logger) gin.HandlerFunc {
	log := logger
	if log == nil {
		log = slog.Default()
	}

	if len(allowed) == 0 {
		log.Warn("Admin IP allowlist is empty; allowing all admin requests (configure ADMIN_ALLOWED_IPS for production)")
		return func(c *gin.Context) {
			c.Next()
		}
	}
	return func(c *gin.Context) {
		clientIP := net.ParseIP(c.ClientIP())
		if clientIP == nil {
			log.Warn("Invalid client IP", slog.String("ip", c.ClientIP()))
			abortWithError(c, 403, "forbidden", "")
			return
		}
		if !adminIPAllowed(clientIP, allowed) {
			log.Warn("Admin IP blocked", slog.String("ip", clientIP.String()))
			abortWithError(c, 403, "forbidden", "")
			return
		}
		c.Next()
	}
}

func adminIPAllowed(clientIP net.IP, allowed []*net.IPNet) bool {
	if clientIP == nil {
		return false
	}
	for _, cidr := range allowed {
		if cidr != nil && cidr.Contains(clientIP) {
			return true
		}
	}
	return false
}
