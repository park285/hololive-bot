package httpapi

import (
	jsonv2 "encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/contracts/common"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

func TestProvideAPIRouterRegistersDomainRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router, err := ProvideAPIRouter(
		t.Context(),
		testRouterConfig(),
		slog.New(slog.DiscardHandler),
		testDomainHandlers(),
		sharedtestutil.NewTestCacheService(t.Context(), t),
		sharedreadiness.NewProbe("admin"),
	)
	if err != nil {
		t.Fatalf("ProvideAPIRouter() error = %v", err)
	}

	routeSet := make(map[string]struct{})

	for _, route := range router.Routes() {
		routeSet[route.Method+" "+route.Path] = struct{}{}
	}

	expectedRoutes := []string{
		"GET /api/holo/members",
		"DELETE /api/holo/alarms",
		"POST /api/holo/rooms/acl",
		"GET /api/holo/streams/live",
	}
	for _, route := range expectedRoutes {
		if _, ok := routeSet[route]; !ok {
			t.Fatalf("missing route %s", route)
		}
	}
}

func TestValidateAPIRouterInputsRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	validConfig := testRouterConfig()
	validDomains := testDomainHandlers()

	tests := []struct {
		name    string
		cfg     *apiconfig.AdminPlaneConfig
		domains *server.DomainHandlers
		wantErr string
	}{
		{name: "nil config", cfg: nil, domains: validDomains, wantErr: "config must not be nil"},
		{name: "blank api key", cfg: &apiconfig.AdminPlaneConfig{}, domains: validDomains, wantErr: "API_SECRET_KEY required"},
		{name: "nil domains", cfg: validConfig, domains: nil, wantErr: "domain handlers must not be nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateAPIRouterInputs(tt.cfg, tt.domains)
			if err == nil {
				t.Fatal("validateAPIRouterInputs() expected error")
			}

			if err.Error() != tt.wantErr {
				t.Fatalf("validateAPIRouterInputs() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateDomainHandlersRejectsEachMissingHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*server.DomainHandlers)
		wantErr string
	}{
		{name: "member", mutate: func(h *server.DomainHandlers) { h.Member = nil }, wantErr: "member handler must not be nil"},
		{name: "alarm", mutate: func(h *server.DomainHandlers) { h.Alarm = nil }, wantErr: "alarm handler must not be nil"},
		{name: "room", mutate: func(h *server.DomainHandlers) { h.Room = nil }, wantErr: "room handler must not be nil"},
		{name: "stream", mutate: func(h *server.DomainHandlers) { h.Stream = nil }, wantErr: "stream handler must not be nil"},
		{name: "stats", mutate: func(h *server.DomainHandlers) { h.Stats = nil }, wantErr: "stats handler must not be nil"},
		{name: "settings", mutate: func(h *server.DomainHandlers) { h.Settings = nil }, wantErr: "settings handler must not be nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			domains := *testDomainHandlers()
			tt.mutate(&domains)

			err := validateDomainHandlers(&domains)
			if err == nil {
				t.Fatal("validateDomainHandlers() expected error")
			}

			if err.Error() != tt.wantErr {
				t.Fatalf("validateDomainHandlers() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}

	if err := validateDomainHandlers(testDomainHandlers()); err != nil {
		t.Fatalf("validateDomainHandlers() valid error = %v", err)
	}
}

func TestNewAPIRouterValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	probe := sharedreadiness.NewProbe("admin")

	if router, err := newAPIRouter(t.Context(), nil, nil, probe); err == nil || router != nil {
		t.Fatalf("newAPIRouter(nil) router=%v error=%v", router, err)
	}

	if router, err := newAPIRouter(t.Context(), testRouterConfig(), nil, nil); err == nil || router != nil {
		t.Fatalf("newAPIRouter(nil probe) router=%v error=%v", router, err)
	}

	router, err := newAPIRouter(t.Context(), testRouterConfig(), nil, probe)
	if err != nil {
		t.Fatalf("newAPIRouter() error = %v", err)
	}

	if router == nil {
		t.Fatal("newAPIRouter() returned nil router")
	}
}

func TestAPIRateLimitNilCacheAndAbortResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// cache가 없으면 통과시키는 middleware 대신 기동 오류다(fail-closed).
	if handler, err := apiRateLimitMiddleware(nil, nil); err == nil || handler != nil {
		t.Fatalf("apiRateLimitMiddleware(nil) = (%v, %v), want startup error", handler != nil, err)
	}

	abortRouter := gin.New()
	abortRouter.GET("/limited", func(c *gin.Context) {
		abortWithRateLimitError(c)
	})

	abortReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/limited", http.NoBody)
	abortRec := httptest.NewRecorder()
	abortRouter.ServeHTTP(abortRec, abortReq)

	if abortRec.Code != http.StatusTooManyRequests {
		t.Fatalf("abort status = %d, want %d", abortRec.Code, http.StatusTooManyRequests)
	}

	var payload map[string]any

	if err := jsonv2.Unmarshal(abortRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["error"] != "too many requests" {
		t.Fatalf("error=%v want=%q", payload["error"], "too many requests")
	}
}

func TestRegisteredRoutesRequireAPIKeyInAppHTTPPackage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router, err := ProvideAPIRouter(
		t.Context(),
		testRouterConfig(),
		nil,
		testDomainHandlers(),
		sharedtestutil.NewTestCacheService(t.Context(), t),
		sharedreadiness.NewProbe("admin"),
	)
	if err != nil {
		t.Fatalf("ProvideAPIRouter() error = %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/holo/stats", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing api key status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/holo/stats", http.NoBody)
	req.Header.Set(common.APIKeyHeader, "wrong-key")

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong api key status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

// testDomainHandlers는 route 등록과 인증만 검사하는 시험용이다. 각 handler의 의존성이 비어 있어 본문은 호출할 수 없다.
func testDomainHandlers() *server.DomainHandlers {
	return &server.DomainHandlers{
		Member:   &server.MemberHandler{},
		Alarm:    &server.AlarmHandler{},
		Room:     &server.RoomHandler{},
		Stream:   &server.StreamHandler{},
		Stats:    &server.StatsHandler{},
		Settings: &server.SettingsHandler{},
	}
}

func testRouterConfig() *apiconfig.AdminPlaneConfig {
	return &apiconfig.AdminPlaneConfig{
		Server: settings.ServerConfig{APIKey: "test-key"},
	}
}
