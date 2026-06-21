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
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	sharedlog "github.com/park285/shared-go/pkg/logging"
)

// skipPaths는 다음 형식을 지원합니다:
//   - "/exact/path": 정확히 일치하는 경로 스킵
//   - "*/suffix": 해당 suffix로 끝나는 경로 스킵 (예: "*/stream")
//   - "/prefix*": 해당 prefix로 시작하는 경로 스킵 (예: "/api/holo/ws*")
func LoggerMiddleware(ctx context.Context, logger *slog.Logger, skipPaths ...string) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	matcher := newSkipPathMatcher(skipPaths)

	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// 스킵 경로 확인
		if matcher.shouldSkip(path) {
			c.Next()
			return
		}

		start := time.Now()
		c.Next()
		latency := time.Since(start)

		// WebSocket 업그레이드 등으로 연결이 hijacked 상태면 로깅 스킵
		// (hijacked 연결에서 c.Writer 접근 시 경고 발생 방지)
		if c.Writer.Written() && c.Writer.Size() < 0 {
			return
		}

		logHTTPRequest(ctx, logger, c, path, latency)
	}
}

type skipPathMatcher struct {
	exact  map[string]bool
	prefix []string
	suffix []string
}

func newSkipPathMatcher(skipPaths []string) skipPathMatcher {
	matcher := skipPathMatcher{exact: make(map[string]bool)}
	for _, pattern := range skipPaths {
		matcher.add(pattern)
	}
	return matcher
}

func (m *skipPathMatcher) add(pattern string) {
	switch {
	case len(pattern) > 1 && pattern[0] == '*':
		m.suffix = append(m.suffix, pattern[1:])
	case len(pattern) > 1 && pattern[len(pattern)-1] == '*':
		m.prefix = append(m.prefix, pattern[:len(pattern)-1])
	default:
		m.exact[pattern] = true
	}
}

func logHTTPRequest(ctx context.Context, logger *slog.Logger, c *gin.Context, path string, latency time.Duration) {
	status := c.Writer.Status()
	level := httpLogLevel(status)
	reqCtx := requestLogContext(ctx, c)
	if !logger.Enabled(reqCtx, level) {
		return
	}

	attrs := httpLogAttrs(c, path, status, latency)
	sharedlog.Log(reqCtx, logger, level, "http.request.completed", "HTTP", attrs...)
}

func requestLogContext(ctx context.Context, c *gin.Context) context.Context {
	if c != nil && c.Request != nil {
		if reqCtx := c.Request.Context(); reqCtx != nil {
			return reqCtx
		}
	}
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func httpLogLevel(status int) slog.Level {
	if status >= 500 {
		return slog.LevelError
	}
	if status >= 400 {
		return slog.LevelWarn
	}
	return slog.LevelDebug
}

func httpLogAttrs(c *gin.Context, path string, status int, latency time.Duration) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("method", c.Request.Method),
		slog.String("path", path),
		slog.Int("status", status),
		slog.String("ip", c.ClientIP()),
		slog.String("remote_addr", strings.TrimSpace(c.Request.RemoteAddr)),
		slog.String("ua", requestDeviceInfo(c)),
		sharedlog.DurationMS(latency),
	}
	attrs = appendOptionalHeaderAttr(attrs, "x_forwarded_for", c.GetHeader("X-Forwarded-For"))
	attrs = appendOptionalHeaderAttr(attrs, "x_real_ip", c.GetHeader("X-Real-IP"))
	return attrs
}

func requestDeviceInfo(c *gin.Context) string {
	clientHints := ParseClientHints(c)
	if deviceInfo := clientHints.Summary(); deviceInfo != "" {
		return deviceInfo
	}
	return truncateUA(c.Request.UserAgent())
}

func appendOptionalHeaderAttr(attrs []slog.Attr, key, value string) []slog.Attr {
	if value := strings.TrimSpace(value); value != "" {
		return append(attrs, slog.String(key, truncateHeader(value)))
	}
	return attrs
}

func truncateHeader(value string) string {
	const maxLen = 128
	if len(value) > maxLen {
		return value[:maxLen] + "..."
	}
	return value
}

// shouldSkipPath: 경로가 스킵 대상인지 확인합니다.
func shouldSkipPath(path string, exactSkip map[string]bool, prefixSkip, suffixSkip []string) bool {
	return skipPathMatcher{exact: exactSkip, prefix: prefixSkip, suffix: suffixSkip}.shouldSkip(path)
}

func (m skipPathMatcher) shouldSkip(path string) bool {
	if m.exact[path] {
		return true
	}
	return hasAnyPrefix(path, m.prefix) || hasAnySuffix(path, m.suffix)
}

func hasAnyPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func hasAnySuffix(path string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

// truncateUA: User-Agent를 적절한 길이로 자름 (로그 가독성)
func truncateUA(ua string) string {
	const maxLen = 80
	if len(ua) > maxLen {
		return ua[:maxLen] + "..."
	}
	return ua
}

func LogDebugf(ctx context.Context, logger *slog.Logger, msg string, attrs ...any) {
	if logger.Enabled(ctx, slog.LevelDebug) {
		logger.Debug(msg, attrs...)
	}
}
