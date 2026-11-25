package adminruntime

import (
	"log/slog"
	"testing"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	apphttp "github.com/kapu/hololive-api/internal/planes/admin/internal/httpapi"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

func TestAPIRouter_DomainRoutesRegistered(t *testing.T) {
	ctx := t.Context()
	logger := slog.New(slog.DiscardHandler)
	domainHandlers := testAdminDomainHandlers()

	appConfig := &apiconfig.AdminPlaneConfig{
		Server: settings.ServerConfig{
			APIKey: testAPIKey,
		},
	}

	router, err := apphttp.ProvideAPIRouter(ctx, appConfig, logger, domainHandlers, sharedtestutil.NewTestCacheService(ctx, t), sharedreadiness.NewProbe("admin"))
	if err != nil {
		t.Fatalf("ProvideAPIRouter() error = %v", err)
	}

	routeSet := make(map[string]struct{})

	for _, route := range router.Routes() {
		routeSet[route.Method+" "+route.Path] = struct{}{}
	}

	for domain, routes := range expectedAdminAPIDomainRoutes() {
		for _, route := range routes {
			if _, ok := routeSet[route]; !ok {
				t.Errorf("domain=%s missing route %s", domain, route)
			}
		}
	}
}

func expectedAdminAPIDomainRoutes() map[string][]string {
	return map[string][]string{
		"member": {
			"GET /api/holo/members",
			"POST /api/holo/members",
			"POST /api/holo/members/:id/aliases",
			"DELETE /api/holo/members/:id/aliases",
			"PATCH /api/holo/members/:id/graduation",
			"PATCH /api/holo/members/:id/channel",
			"PATCH /api/holo/members/:id/name",
		},
		"alarm": {
			"GET /api/holo/alarms",
			"DELETE /api/holo/alarms",
		},
		"room": {
			"GET /api/holo/rooms",
			"POST /api/holo/rooms",
			"DELETE /api/holo/rooms",
			"POST /api/holo/rooms/acl",
		},
		"stats_stream": {
			"GET /api/holo/stats",
			"GET /api/holo/streams/live",
			"GET /api/holo/streams/upcoming",
		},
		"settings": {
			"GET /api/holo/settings",
			"POST /api/holo/settings",
			"POST /api/holo/names/room",
		},
	}
}
