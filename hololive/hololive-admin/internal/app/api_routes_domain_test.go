package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-admin/internal/server"
	"github.com/kapu/hololive-shared/pkg/config"
)

type routeSpec struct {
	method string
	path   string
}

func TestAPIRouter_DomainRoutesRegistered(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	apiHandler := &server.APIHandler{}
	domainHandlers := apiHandler.DomainHandlers()
	authHandler := &server.AuthHandler{}

	cfg := &config.Config{
		Server: config.ServerConfig{
			APIKey: "test-key",
		},
		CORS: config.CORSConfig{
			AllowedOrigins: []string{"http://localhost:3000"},
		},
	}

	router, err := ProvideAPIRouter(ctx, cfg, logger, domainHandlers, authHandler, nil, nil)
	if err != nil {
		t.Fatalf("ProvideAPIRouter() error = %v", err)
	}

	routeSet := make(map[string]struct{})
	for _, route := range router.Routes() {
		routeSet[route.Method+" "+route.Path] = struct{}{}
	}

	expectedByDomain := map[string][]routeSpec{
		"oauth": {
			{method: "GET", path: "/oauth/callback"},
		},
		"auth": {
			{method: "POST", path: "/api/auth/register"},
			{method: "POST", path: "/api/auth/login"},
			{method: "POST", path: "/api/auth/logout"},
			{method: "POST", path: "/api/auth/refresh"},
			{method: "GET", path: "/api/auth/me"},
			{method: "POST", path: "/api/auth/password/reset-request"},
			{method: "POST", path: "/api/auth/password/reset"},
		},
		"member": {
			{method: "GET", path: "/api/holo/members"},
			{method: "POST", path: "/api/holo/members"},
			{method: "POST", path: "/api/holo/members/:id/aliases"},
			{method: "DELETE", path: "/api/holo/members/:id/aliases"},
			{method: "PATCH", path: "/api/holo/members/:id/graduation"},
			{method: "PATCH", path: "/api/holo/members/:id/channel"},
			{method: "PATCH", path: "/api/holo/members/:id/name"},
		},
		"alarm": {
			{method: "GET", path: "/api/holo/alarms"},
			{method: "DELETE", path: "/api/holo/alarms"},
		},
		"room": {
			{method: "GET", path: "/api/holo/rooms"},
			{method: "POST", path: "/api/holo/rooms"},
			{method: "DELETE", path: "/api/holo/rooms"},
			{method: "POST", path: "/api/holo/rooms/acl"},
		},
		"stats_stream": {
			{method: "GET", path: "/api/holo/stats"},
			{method: "GET", path: "/api/holo/stats/channels"},
			{method: "GET", path: "/api/holo/streams/live"},
			{method: "GET", path: "/api/holo/streams/upcoming"},
			{method: "GET", path: "/api/holo/channels"},
			{method: "GET", path: "/api/holo/channels/search"},
		},
		"settings": {
			{method: "GET", path: "/api/holo/logs"},
			{method: "GET", path: "/api/holo/settings"},
			{method: "POST", path: "/api/holo/settings"},
			{method: "POST", path: "/api/holo/settings/llm"},
			{method: "POST", path: "/api/holo/names/room"},
			{method: "POST", path: "/api/holo/names/user"},
		},
		"template": {
			{method: "GET", path: "/api/holo/templates"},
			{method: "GET", path: "/api/holo/templates/:key"},
			{method: "PUT", path: "/api/holo/templates/:key"},
			{method: "DELETE", path: "/api/holo/templates/:key"},
			{method: "POST", path: "/api/holo/templates/:key/preview"},
			{method: "GET", path: "/api/holo/templates/:key/revisions"},
			{method: "GET", path: "/api/holo/templates/:key/revisions/:id"},
		},
		"milestone": {
			{method: "GET", path: "/api/holo/milestones"},
			{method: "GET", path: "/api/holo/milestones/near"},
			{method: "GET", path: "/api/holo/milestones/stats"},
		},
		"profile": {
			{method: "GET", path: "/api/holo/profiles"},
			{method: "GET", path: "/api/holo/profiles/name"},
		},
		"major_event": {
			{method: "POST", path: "/api/holo/majorevent/trigger"},
			{method: "POST", path: "/api/holo/majorevent/monthly-trigger"},
		},
	}

	for domain, routes := range expectedByDomain {
		for _, route := range routes {
			key := route.method + " " + route.path
			if _, ok := routeSet[key]; !ok {
				t.Errorf("domain=%s missing route %s", domain, key)
			}
		}
	}
}
