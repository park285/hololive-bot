// Package middleware: HTTP 미들웨어
package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/static"
)

var (
	hintsOnce   sync.Once
	cachedHints []string
)

// buildHints: index.html에서 추출한 critical assets로 Link 헤더 생성
func buildHints() []string {
	if !static.HasEmbedded() {
		return nil
	}

	css, js := static.CriticalAssets()
	hints := make([]string, 0, len(css)+len(js))

	for _, path := range css {
		hints = append(hints, fmt.Sprintf("<%s>; rel=preload; as=style", path))
	}
	for _, path := range js {
		hints = append(hints, fmt.Sprintf("<%s>; rel=modulepreload", path))
	}

	return hints
}

// EarlyHints: HTTP 103 Early Hints를 전송하여 브라우저가 리소스를 미리 로드하도록 유도
// Go 1.19+ 필요 (http.ResponseController.Flush)
// 참고: Cloudflare Tunnel은 103을 클라이언트로 전달하지 않을 수 있음
func EarlyHints(customHints []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// HTML 페이지 요청만 처리 (SPA fallback)
		path := c.Request.URL.Path
		if !shouldSendEarlyHints(c, path) {
			c.Next()
			return
		}

		// 힌트 초기화 (한 번만)
		hints := customHints
		if len(hints) == 0 {
			hintsOnce.Do(func() {
				cachedHints = buildHints()
			})
			hints = cachedHints
		}

		if len(hints) == 0 {
			c.Next()
			return
		}

		// Link 헤더 설정
		for _, hint := range hints {
			c.Writer.Header().Add("Link", hint)
		}

		// 103 Early Hints 전송 (Go 1.19+)
		if flusher, ok := c.Writer.(http.Flusher); ok {
			c.Writer.WriteHeader(http.StatusEarlyHints)
			flusher.Flush()
		}

		c.Next()
	}
}

// shouldSendEarlyHints: Early Hints를 보낼지 판단
func shouldSendEarlyHints(c *gin.Context, path string) bool {
	// GET 요청만
	if c.Request.Method != http.MethodGet {
		return false
	}

	// API 요청 제외
	if strings.HasPrefix(path, "/admin/api/") {
		return false
	}

	// 정적 자산 제외
	if strings.HasPrefix(path, "/assets/") {
		return false
	}

	// Accept 헤더로 HTML 요청 확인
	accept := c.GetHeader("Accept")
	if !strings.Contains(accept, "text/html") {
		return false
	}

	// 루트 또는 SPA 경로
	return path == "/" || !strings.Contains(path, ".")
}
