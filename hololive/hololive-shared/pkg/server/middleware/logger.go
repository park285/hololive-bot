package middleware

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/core/stringutil"
	sharedlog "github.com/park285/shared-go/v2/pkg/logging"
)

const (
	maxLoggedUserAgentRunes = 80
	maxLoggedHeaderRunes    = 128
)

func LoggerMiddleware(ctx context.Context, logger *slog.Logger, skipPaths ...string) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}

	matcher := newSkipPathMatcher(skipPaths)

	return func(c *gin.Context) {
		path := c.Request.URL.Path

		if matcher.shouldSkip(path) {
			c.Next()

			return
		}

		start := time.Now()

		c.Next()

		latency := time.Since(start)

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
		return c.Request.Context()
	}

	return ctx
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
	clientHints := parseClientHints(c)
	if deviceInfo := clientHints.Summary(); deviceInfo != "" {
		return deviceInfo
	}

	return stringutil.TruncateString(c.Request.UserAgent(), maxLoggedUserAgentRunes)
}

func appendOptionalHeaderAttr(attrs []slog.Attr, key, value string) []slog.Attr {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return append(attrs, slog.String(key, stringutil.TruncateString(trimmed, maxLoggedHeaderRunes)))
	}

	return attrs
}

func (m *skipPathMatcher) shouldSkip(path string) bool {
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
