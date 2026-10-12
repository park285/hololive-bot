package adminruntime

import (
	jsonv2 "encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	apphttp "github.com/kapu/hololive-api/internal/planes/admin/internal/httpapi"
	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	authsvc "github.com/kapu/hololive-api/internal/planes/admin/internal/service/auth"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

// /api/auth 그룹은 RemoteAddr 기준 관리자 IP 허용 목록 뒤에 있다. 토큰 없는 /me는 서비스를 부르기 전에
// 401로 끝나므로, 허용 peer는 핸들러에 닿고 목록 밖 peer는 403으로 막히는지 DB 없이 확인할 수 있다.
func TestAPIRouterAuthGroupEnforcesAdminIPAllowlist(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	config := &apiconfig.AdminPlaneConfig{
		Server: settings.ServerConfig{
			APIKey: testAPIKey, AdminAllowedIPs: []string{"100.100.1.0/24"},
		},
		CORS: settings.CORSConfig{AllowedOrigins: []string{testAllowedOrigin}},
	}
	authHandler := server.NewAuthHandler(new(authsvc.Service), logger)
	router, err := apphttp.ProvideAPIRouter(
		t.Context(), config, logger, (&server.Handler{}).DomainHandlers(), authHandler,
		sharedtestutil.NewTestCacheService(t.Context(), t),
	)
	assertAPIRouterSuccess(t, router, err)

	for _, peer := range []struct {
		name, remoteAddr string
		status           int
	}{
		{name: "allowed", remoteAddr: "100.100.1.5:40000", status: http.StatusUnauthorized},
		{name: "outside allowlist", remoteAddr: "203.0.113.9:40000", status: http.StatusForbidden},
	} {
		t.Run(peer.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/auth/me", http.NoBody)

			request.RemoteAddr = peer.remoteAddr

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			if recorder.Code != peer.status {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, peer.status, recorder.Body.String())
			}

			if peer.status != http.StatusUnauthorized {
				return
			}

			var response struct {
				Success bool              `json:"success"`
				Error   authsvc.ErrorCode `json:"error"`
			}

			if decodeErr := jsonv2.Unmarshal(recorder.Body.Bytes(), &response); decodeErr != nil {
				t.Fatal(decodeErr)
			}

			if response.Success || response.Error != authsvc.CodeUnauthorized {
				t.Fatalf("unauthorized response = %+v", response)
			}
		})
	}
}
