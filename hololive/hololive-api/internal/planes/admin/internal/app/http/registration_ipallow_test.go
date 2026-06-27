package apphttp

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/server/middleware"

	"github.com/kapu/hololive-api/internal/planes/admin/internal/server"
)

func adminAllowlistRouter(t *testing.T, allowedIPs []string) *gin.Engine {
	t.Helper()

	cfg := testRouterConfig()
	cfg.Server.AdminAllowedIPs = allowedIPs

	router, err := ProvideAPIRouter(
		t.Context(),
		cfg,
		slog.New(slog.DiscardHandler),
		(&server.Handler{}).DomainHandlers(),
		&server.AuthHandler{},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("ProvideAPIRouter() error = %v", err)
	}
	return router
}

func loginStatus(t *testing.T, router *gin.Engine, remoteAddr string, headers map[string]string) int {
	t.Helper()

	// 빈 본문이므로 allowlist를 통과하면 핸들러의 ShouldBindJSON이 400을 반환한다.
	// allowlist에서 차단되면 핸들러에 도달하지 못하고 403이 된다.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/auth/login", http.NoBody)
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code
}

func registerStatus(t *testing.T, router *gin.Engine, remoteAddr string, headers map[string]string) int {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/auth/register", http.NoBody)
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code
}

// 허용 CIDR이 설정되면 /api/auth/* 는 RemoteAddr 기준으로 통과/차단되어야 한다.
func TestAdminAuthAllowlist_AllowsInsideAndBlocksOutside(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := adminAllowlistRouter(t, []string{"100.100.1.0/24"})

	if got := loginStatus(t, router, "100.100.1.5:40000", nil); got != http.StatusBadRequest {
		t.Fatalf("inside-allowlist status = %d, want %d (passes allowlist, empty body)", got, http.StatusBadRequest)
	}

	if got := loginStatus(t, router, "203.0.113.9:40000", nil); got != http.StatusForbidden {
		t.Fatalf("outside-allowlist status = %d, want %d (blocked by allowlist)", got, http.StatusForbidden)
	}
}

func TestAdminRegisterAllowlistBlocksOutsideRemoteAddrBeforeAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := adminAllowlistRouter(t, []string{"100.100.1.0/24"})
	apiKeyHeaders := map[string]string{middleware.APIKeyHeader: "test-key"}

	if got := registerStatus(t, router, "203.0.113.9:40000", apiKeyHeaders); got != http.StatusForbidden {
		t.Fatalf("outside-allowlist register status = %d, want %d", got, http.StatusForbidden)
	}

	if got := registerStatus(t, router, "100.100.1.5:40000", nil); got != http.StatusUnauthorized {
		t.Fatalf("inside-allowlist register without api key status = %d, want %d", got, http.StatusUnauthorized)
	}

	if got := registerStatus(t, router, "100.100.1.5:40000", apiKeyHeaders); got != http.StatusBadRequest {
		t.Fatalf("inside-allowlist register with api key status = %d, want %d", got, http.StatusBadRequest)
	}
}

// CF-Connecting-IP / X-Forwarded-For 위조로 allowlist를 우회할 수 없어야 한다.
// RemoteAddr가 허용 대역 밖이면 위조 헤더가 허용 IP를 가리켜도 403이어야 한다.
func TestAdminAuthAllowlist_IgnoresSpoofedClientIPHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := adminAllowlistRouter(t, []string{"100.100.1.0/24"})

	spoofHeaders := []map[string]string{
		{"CF-Connecting-IP": "100.100.1.5"},
		{"X-Forwarded-For": "100.100.1.5"},
		{"CF-Connecting-IP": "100.100.1.5", "X-Forwarded-For": "100.100.1.6"},
		{"X-Real-IP": "100.100.1.5"},
	}

	for _, headers := range spoofHeaders {
		if got := loginStatus(t, router, "203.0.113.9:40000", headers); got != http.StatusForbidden {
			t.Fatalf("spoofed headers %v from outside RemoteAddr status = %d, want %d", headers, got, http.StatusForbidden)
		}
	}
}

// 허용 목록이 비어 있으면(기본값) 전체 허용으로 동작해야 한다 (개발 편의).
func TestAdminAuthAllowlist_EmptyAllowsAll(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := adminAllowlistRouter(t, nil)

	if got := loginStatus(t, router, "203.0.113.9:40000", nil); got != http.StatusBadRequest {
		t.Fatalf("empty allowlist status = %d, want %d (allow-all, empty body)", got, http.StatusBadRequest)
	}
}
