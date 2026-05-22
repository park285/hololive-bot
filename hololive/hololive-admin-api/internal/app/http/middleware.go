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

package apphttp

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

func normalizedOrigins(origins []string) []string {
	result := make([]string, 0, len(origins))
	seen := make(map[string]struct{}, len(origins))

	for _, origin := range origins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}

		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}

	return result
}

func containsWildcard(origins []string) bool {
	for _, origin := range origins {
		if strings.TrimSpace(origin) == "*" {
			return true
		}
	}

	return false
}

func newAPICORSConfig(appConfig *config.Config, enforce bool) cors.Config {
	corsConfig := cors.DefaultConfig()

	origins := normalizedOrigins(appConfig.CORS.AllowedOrigins)
	if !enforce {
		// 모니터 모드에서는 guard가 차단하지 않으므로 CORS middleware도
		// 요청 Origin을 반사하도록 둔다. enforce=true에서는 아래에서 명시 origin만 허용한다.
		corsConfig.AllowOriginFunc = func(string) bool { return true }
	} else if len(origins) == 0 || containsWildcard(origins) {
		// API key와 세션 쿠키를 함께 쓰는 admin API에서는 wildcard CORS와
		// credentials 조합을 안전한 기본값으로 취급하지 않는다.
		corsConfig.AllowOriginFunc = func(string) bool { return false }
	} else {
		corsConfig.AllowOrigins = origins
	}

	corsConfig.AllowCredentials = true
	corsConfig.AllowMethods = constants.CORSConfig.AllowMethods
	corsConfig.AllowHeaders = constants.CORSConfig.AllowHeaders

	return corsConfig
}

func corsOriginGuard(allowedOrigins []string, enforce bool, logger *slog.Logger) gin.HandlerFunc {
	origins := normalizedOrigins(allowedOrigins)
	guard := corsOriginGuardState{
		allowed:  corsOriginAllowedSet(origins),
		allowAll: containsWildcard(origins),
		enforce:  enforce,
		logger:   logger,
	}

	return func(c *gin.Context) {
		guard.handle(c, strings.TrimSpace(c.GetHeader("Origin")))
	}
}

type corsOriginGuardState struct {
	allowed  map[string]struct{}
	allowAll bool
	enforce  bool
	logger   *slog.Logger
}

func corsOriginAllowedSet(origins []string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		if origin != "*" {
			allowed[origin] = struct{}{}
		}
	}
	return allowed
}

func (g corsOriginGuardState) handle(c *gin.Context, origin string) {
	if origin == "" {
		c.Next()
		return
	}
	if !g.enforce {
		g.warnMonitorOnly(origin)
		c.Next()
		return
	}
	if !g.allows(origin) {
		sharedserver.RespondError(c, http.StatusForbidden, "forbidden", nil)
		c.Abort()
		return
	}
	c.Next()
}

func (g corsOriginGuardState) warnMonitorOnly(origin string) {
	if g.logger != nil {
		g.logger.Warn("cors_origin_monitor_only", slog.String("origin", origin))
	}
}

func (g corsOriginGuardState) allows(origin string) bool {
	if g.allowAll {
		return false
	}
	_, ok := g.allowed[origin]
	return ok
}
