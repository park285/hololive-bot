package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// LoggerMiddleware: slog 기반 HTTP 접속 로깅 미들웨어 (고성능 최적화)
// skipPaths는 다음 형식을 지원합니다:
//   - "/exact/path": 정확히 일치하는 경로 스킵
//   - "*/suffix": 해당 suffix로 끝나는 경로 스킵 (예: "*/stream")
//   - "/prefix*": 해당 prefix로 시작하는 경로 스킵 (예: "/api/holo/ws*")
func LoggerMiddleware(ctx context.Context, logger *slog.Logger, skipPaths ...string) gin.HandlerFunc {
	// 스킵 설정 분류
	exactSkip := make(map[string]bool)
	var prefixSkip, suffixSkip []string

	for _, pattern := range skipPaths {
		switch {
		case len(pattern) > 1 && pattern[0] == '*':
			// *suffix 패턴
			suffixSkip = append(suffixSkip, pattern[1:])
		case len(pattern) > 1 && pattern[len(pattern)-1] == '*':
			// prefix* 패턴
			prefixSkip = append(prefixSkip, pattern[:len(pattern)-1])
		default:
			// 정확한 경로 매칭
			exactSkip[pattern] = true
		}
	}

	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// 스킵 경로 확인
		if shouldSkipPath(path, exactSkip, prefixSkip, suffixSkip) {
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

		status := c.Writer.Status()

		// 레벨 결정: 정상 요청은 DEBUG, 4xx는 WARN, 5xx는 ERROR
		level := slog.LevelDebug
		if status >= 500 {
			level = slog.LevelError
		} else if status >= 400 {
			level = slog.LevelWarn
		}

		// 효율화: 해당 레벨이 활성화 상태인지 먼저 확인
		if !logger.Enabled(ctx, level) {
			return
		}

		clientIP := c.ClientIP()
		method := c.Request.Method

		// Client Hints 우선 사용 (실제 기기 정보)
		clientHints := ParseClientHints(c)
		deviceInfo := clientHints.Summary()
		if deviceInfo == "" {
			// Client Hints 미지원 시 기존 User-Agent 사용
			deviceInfo = truncateUA(c.Request.UserAgent())
		}

		// 기본 필드
		attrs := []slog.Attr{
			slog.String("method", method),
			slog.String("path", path),
			slog.Int("status", status),
			slog.String("ip", clientIP),
			slog.String("ua", deviceInfo),
		}

		// 느린 요청(100ms+)만 레이턴시 포함
		if latency >= 100*time.Millisecond {
			attrs = append(attrs, slog.Duration("latency", latency))
		}

		logger.LogAttrs(ctx, level, "HTTP", attrs...)
	}
}

// shouldSkipPath: 경로가 스킵 대상인지 확인합니다.
func shouldSkipPath(path string, exactSkip map[string]bool, prefixSkip, suffixSkip []string) bool {
	// 정확한 매칭
	if exactSkip[path] {
		return true
	}

	// prefix 매칭
	for _, prefix := range prefixSkip {
		if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
			return true
		}
	}

	// suffix 매칭
	for _, suffix := range suffixSkip {
		if len(path) >= len(suffix) && path[len(path)-len(suffix):] == suffix {
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

// LogDebugf: Debug 레벨 로그를 조건부로 출력 (지연 평가)
func LogDebugf(ctx context.Context, logger *slog.Logger, msg string, attrs ...any) {
	if logger.Enabled(ctx, slog.LevelDebug) {
		logger.Debug(msg, attrs...)
	}
}
