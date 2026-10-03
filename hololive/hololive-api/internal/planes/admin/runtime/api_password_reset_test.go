package adminruntime

import (
	jsonv2 "encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apphttp "github.com/kapu/hololive-api/internal/planes/admin/internal/httpapi"
	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	authsvc "github.com/kapu/hololive-api/internal/planes/admin/internal/service/auth"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

func TestAPIRouterPasswordResetUnsupportedPreservesIPAllowlist(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	config := &settings.Config{
		Server: settings.ServerConfig{
			APIKey: testAPIKey, AdminAllowedIPs: []string{"100.100.1.0/24"},
		},
		CORS: settings.CORSConfig{AllowedOrigins: []string{testAllowedOrigin}},
	}
	// DB와 캐시가 없는 non-nil 서비스다. 핸들러가 재설정 작업을 호출하면 503 응답에 도달하지 못한다.
	authHandler := server.NewAuthHandler(new(authsvc.Service), logger)
	router, err := apphttp.ProvideAPIRouter(
		t.Context(), config, logger, (&server.Handler{}).DomainHandlers(), authHandler,
		sharedtestutil.NewTestCacheService(t.Context(), t),
	)
	assertAPIRouterSuccess(t, router, err)

	for _, endpoint := range []struct{ path, body string }{
		{path: "/api/auth/password/reset-request", body: `{"email":"synthetic@example.invalid"}`},
		{path: "/api/auth/password/reset", body: `{"token":"synthetic-reset-token","newPassword":"SyntheticPass123"}`},
	} {
		for _, peer := range []struct {
			name, remoteAddr string
			status           int
		}{
			{name: "allowed", remoteAddr: "100.100.1.5:40000", status: http.StatusServiceUnavailable},
			{name: "outside allowlist", remoteAddr: "203.0.113.9:40000", status: http.StatusForbidden},
		} {
			t.Run(endpoint.path+"/"+peer.name, func(t *testing.T) {
				request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, endpoint.path, strings.NewReader(endpoint.body))

				request.RemoteAddr = peer.remoteAddr
				request.Header.Set("Content-Type", "application/json")

				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)

				if recorder.Code != peer.status {
					t.Fatalf("status = %d, want %d (body %s)", recorder.Code, peer.status, recorder.Body.String())
				}

				if peer.status != http.StatusServiceUnavailable {
					return
				}

				var response struct {
					Success bool              `json:"success"`
					Error   authsvc.ErrorCode `json:"error"`
				}

				if decodeErr := jsonv2.Unmarshal(recorder.Body.Bytes(), &response); decodeErr != nil {
					t.Fatal(decodeErr)
				}

				if response.Success || response.Error != authsvc.CodeInternal {
					t.Fatalf("unsupported response = %+v", response)
				}
			})
		}
	}
}
