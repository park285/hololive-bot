package httpserver

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// echoClientIPRoute는 c.ClientIP() 결과를 본문에 그대로 반환하는 테스트 라우트를 등록합니다.
func echoClientIPRoute(router *gin.Engine) error {
	router.GET("/whoami", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})
	return nil
}

func newClientIPProbeRouter(t *testing.T, trustRemoteAddrOnly bool) *gin.Engine {
	t.Helper()

	logger := slog.New(slog.DiscardHandler)
	router, err := NewRuntimeRouter(t.Context(), logger, RuntimeRouterOptions{
		APIKey:              "probe-key",
		TrustRemoteAddrOnly: trustRemoteAddrOnly,
		RegisterRoutes:      echoClientIPRoute,
	})
	if err != nil {
		t.Fatalf("NewRuntimeRouter() error = %v", err)
	}
	return router
}

func probeClientIP(t *testing.T, router *gin.Engine, remoteAddr string, headers map[string]string) string {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/whoami", http.NoBody)
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("/whoami status = %d, want %d", res.Code, http.StatusOK)
	}
	return res.Body.String()
}

// TrustRemoteAddrOnly=true이면 CF-Connecting-IP / X-Forwarded-For 위조를 무시하고
// TCP RemoteAddr만 ClientIP로 반영해야 한다 (Tailscale 직결 형상).
func TestNewRuntimeRouter_TrustRemoteAddrOnly_IgnoresSpoofedHeaders(t *testing.T) {
	t.Parallel()

	router := newClientIPProbeRouter(t, true)

	cases := []struct {
		name    string
		headers map[string]string
	}{
		{
			name:    "CF-Connecting-IP 위조 무시",
			headers: map[string]string{"CF-Connecting-IP": "100.100.1.5"},
		},
		{
			name:    "X-Forwarded-For 위조 무시",
			headers: map[string]string{"X-Forwarded-For": "100.100.1.5"},
		},
		{
			name: "두 헤더 동시 위조 무시",
			headers: map[string]string{
				"CF-Connecting-IP": "100.100.1.5",
				"X-Forwarded-For":  "100.100.1.6, 100.100.1.7",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := probeClientIP(t, router, "203.0.113.9:54321", tc.headers)
			if got != "203.0.113.9" {
				t.Fatalf("ClientIP = %q, want %q (spoofed header must be ignored)", got, "203.0.113.9")
			}
		})
	}
}

// 회귀 가드: 옵션 미설정(zero value=false)이면 기존 Cloudflare 동작을 유지해야 한다.
// gin.PlatformCloudflare에서는 CF-Connecting-IP가 ClientIP로 신뢰된다.
func TestNewRuntimeRouter_DefaultTrustsCloudflareHeader(t *testing.T) {
	t.Parallel()

	router := newClientIPProbeRouter(t, false)

	got := probeClientIP(t, router, "203.0.113.9:54321", map[string]string{
		"CF-Connecting-IP": "198.51.100.23",
	})
	if got != "198.51.100.23" {
		t.Fatalf("ClientIP = %q, want %q (default Cloudflare platform trusts CF-Connecting-IP)", got, "198.51.100.23")
	}
}
