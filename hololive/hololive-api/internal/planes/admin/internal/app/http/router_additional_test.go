package apphttp

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
	json "github.com/park285/shared-go/pkg/json"

	"github.com/kapu/hololive-api/internal/planes/admin/internal/server"
)

func TestProvideAPIRouterRegistersDomainRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router, err := ProvideAPIRouter(
		t.Context(),
		testRouterConfig(),
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

	routeSet := make(map[string]struct{})
	for _, route := range router.Routes() {
		routeSet[route.Method+" "+route.Path] = struct{}{}
	}

	expectedRoutes := []string{
		"GET /oauth/callback",
		"POST /api/auth/register",
		"GET /api/holo/members",
		"DELETE /api/holo/alarms",
		"POST /api/holo/rooms/acl",
		"GET /api/holo/stats/youtube/community-shorts",
		"GET /api/holo/streams/live",
		"POST /api/holo/settings/llm",
		"POST /api/holo/templates/:key/preview",
		"GET /api/holo/profiles/name",
		"POST /api/holo/majorevent/monthly-trigger",
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
	validDomains := (&server.Handler{}).DomainHandlers()
	validAuth := &server.AuthHandler{}

	tests := []struct {
		name    string
		cfg     *config.Config
		domains *server.DomainHandlers
		auth    *server.AuthHandler
		wantErr string
	}{
		{name: "nil config", cfg: nil, domains: validDomains, auth: validAuth, wantErr: "config must not be nil"},
		{name: "blank api key", cfg: &config.Config{}, domains: validDomains, auth: validAuth, wantErr: "API_SECRET_KEY required"},
		{name: "nil domains", cfg: validConfig, domains: nil, auth: validAuth, wantErr: "domain handlers must not be nil"},
		{name: "nil auth", cfg: validConfig, domains: validDomains, auth: nil, wantErr: "auth handler must not be nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAPIRouterInputs(tt.cfg, tt.domains, tt.auth)
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
		{name: "template", mutate: func(h *server.DomainHandlers) { h.Template = nil }, wantErr: "template handler must not be nil"},
		{name: "profile", mutate: func(h *server.DomainHandlers) { h.Profile = nil }, wantErr: "profile handler must not be nil"},
		{name: "major event", mutate: func(h *server.DomainHandlers) { h.MajorEvent = nil }, wantErr: "major event handler must not be nil"},
		{name: "oauth", mutate: func(h *server.DomainHandlers) { h.OAuth = nil }, wantErr: "oauth handler must not be nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domains := *(&server.Handler{}).DomainHandlers()
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

	if err := validateDomainHandlers((&server.Handler{}).DomainHandlers()); err != nil {
		t.Fatalf("validateDomainHandlers() valid error = %v", err)
	}
}

func TestNewAPICORSConfigModes(t *testing.T) {
	t.Parallel()

	monitor := newAPICORSConfig(&config.Config{
		CORS: config.CORSConfig{AllowedOrigins: []string{"https://allowed.example"}},
	}, false)
	if monitor.AllowOriginFunc == nil || !monitor.AllowOriginFunc("https://any.example") {
		t.Fatal("monitor CORS config should allow any origin")
	}

	enforced := newAPICORSConfig(&config.Config{
		CORS: config.CORSConfig{AllowedOrigins: []string{" https://a.example ", "", "https://a.example", "https://b.example"}},
	}, true)
	if !reflect.DeepEqual(enforced.AllowOrigins, []string{"https://a.example", "https://b.example"}) {
		t.Fatalf("enforced AllowOrigins = %#v", enforced.AllowOrigins)
	}
	if enforced.AllowOriginFunc != nil {
		t.Fatal("enforced explicit origin config should not set AllowOriginFunc")
	}

	wildcard := newAPICORSConfig(&config.Config{
		CORS: config.CORSConfig{AllowedOrigins: []string{"*"}},
	}, true)
	if wildcard.AllowOriginFunc == nil || wildcard.AllowOriginFunc("https://allowed.example") {
		t.Fatal("enforced wildcard CORS config should reject origins")
	}
}

func TestCorsOriginGuardAllowsNoOriginAndMonitorMode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(corsOriginGuard([]string{"https://allowed.example"}, true, nil))
	router.GET("/no-origin", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	noOriginReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/no-origin", http.NoBody)
	noOriginRec := httptest.NewRecorder()
	router.ServeHTTP(noOriginRec, noOriginReq)
	if noOriginRec.Code != http.StatusOK {
		t.Fatalf("no origin status = %d, want %d", noOriginRec.Code, http.StatusOK)
	}

	monitorRouter := gin.New()
	monitorRouter.Use(corsOriginGuard([]string{"https://allowed.example"}, false, slog.New(slog.DiscardHandler)))
	monitorRouter.GET("/monitor", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	monitorReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/monitor", http.NoBody)
	monitorReq.Header.Set("Origin", "https://blocked.example")
	monitorRec := httptest.NewRecorder()
	monitorRouter.ServeHTTP(monitorRec, monitorReq)
	if monitorRec.Code != http.StatusOK {
		t.Fatalf("monitor status = %d, want %d", monitorRec.Code, http.StatusOK)
	}
}

func TestNewAPIRouterCORSValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	if router, err := newAPIRouter(t.Context(), nil, nil); err == nil || router != nil {
		t.Fatalf("newAPIRouter(nil) router=%v error=%v", router, err)
	}

	productionWildcard := &config.Config{
		Environment: "production",
		Server:      config.ServerConfig{APIKey: "test-key"},
		CORS: config.CORSConfig{
			AllowedOrigins: []string{"*"},
			Enforce:        true,
		},
	}
	if err := validateAPICORSConfig(productionWildcard, true); err == nil {
		t.Fatal("validateAPICORSConfig() expected production wildcard error")
	}
	if err := validateAPICORSConfig(productionWildcard, false); err != nil {
		t.Fatalf("validateAPICORSConfig() non-production error = %v", err)
	}

	router, err := newAPIRouter(t.Context(), testRouterConfig(), nil)
	if err != nil {
		t.Fatalf("newAPIRouter() error = %v", err)
	}
	if router == nil {
		t.Fatal("newAPIRouter() returned nil router")
	}
}

func TestProvideAPIRouterRejectsEmptyAdminAllowedIPsOnlyInProduction(t *testing.T) {
	gin.SetMode(gin.TestMode)

	productionEmpty := testRouterConfig()
	productionEmpty.Environment = "production"
	productionEmpty.Server.AdminAllowedIPs = nil
	if router, err := provideTestAPIRouter(t, productionEmpty); err == nil || router != nil || err.Error() != "ADMIN_ALLOWED_IPS must be configured in production" {
		t.Fatalf("production empty allowlist router=%v error=%v, want production allowlist error", router, err)
	}

	productionAllowed := testRouterConfig()
	productionAllowed.Environment = "production"
	productionAllowed.Server.AdminAllowedIPs = []string{"100.100.1.0/24"}
	if router, err := provideTestAPIRouter(t, productionAllowed); err != nil || router == nil {
		t.Fatalf("production configured allowlist router=%v error=%v, want no error", router, err)
	}

	nonProductionEmpty := testRouterConfig()
	nonProductionEmpty.Environment = "development"
	nonProductionEmpty.Server.AdminAllowedIPs = nil
	if router, err := provideTestAPIRouter(t, nonProductionEmpty); err != nil || router == nil {
		t.Fatalf("non-production empty allowlist router=%v error=%v, want no error", router, err)
	}
}

func TestAPIRateLimitNilCacheAndAbortResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(apiRateLimitMiddleware(nil, nil))
	router.GET("/limited", func(c *gin.Context) {
		c.Status(http.StatusTeapot)
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/limited", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("nil cache rate limit status = %d, want %d", rec.Code, http.StatusTeapot)
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
	if err := json.Unmarshal(abortRec.Body.Bytes(), &payload); err != nil {
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
		(&server.Handler{}).DomainHandlers(),
		&server.AuthHandler{},
		nil,
		nil,
		nil,
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
	req.Header.Set(middleware.APIKeyHeader, "wrong-key")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong api key status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func provideTestAPIRouter(t *testing.T, cfg *config.Config) (*gin.Engine, error) {
	t.Helper()

	return ProvideAPIRouter(
		t.Context(),
		cfg,
		slog.New(slog.DiscardHandler),
		(&server.Handler{}).DomainHandlers(),
		&server.AuthHandler{},
		nil,
		nil,
		nil,
	)
}

func testRouterConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{APIKey: "test-key"},
		CORS: config.CORSConfig{
			AllowedOrigins: []string{"http://localhost:3000"},
		},
	}
}
