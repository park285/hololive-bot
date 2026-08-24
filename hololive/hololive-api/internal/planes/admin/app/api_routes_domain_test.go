// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package app

import (
	"log/slog"
	"testing"

	apphttp "github.com/kapu/hololive-api/internal/planes/admin/app/http"
	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestAPIRouter_DomainRoutesRegistered(t *testing.T) {
	ctx := t.Context()
	logger := slog.New(slog.DiscardHandler)
	apiHandler := &server.Handler{}
	domainHandlers := apiHandler.DomainHandlers()
	authHandler := &server.AuthHandler{}

	appConfig := &settings.Config{
		Server: settings.ServerConfig{
			APIKey: testAPIKey,
		},
		CORS: settings.CORSConfig{
			AllowedOrigins: []string{testAllowedOrigin},
		},
	}

	router, err := apphttp.ProvideAPIRouter(ctx, appConfig, logger, domainHandlers, authHandler, nil)
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
		"oauth": {
			"GET /oauth/callback",
		},
		"auth": {
			"POST /api/auth/register",
			"POST /api/auth/login",
			"POST /api/auth/logout",
			"POST /api/auth/refresh",
			"GET /api/auth/me",
			"POST /api/auth/password/reset-request",
			"POST /api/auth/password/reset",
		},
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
			"GET /api/holo/stats/system",
			"GET /api/holo/stats/youtube/community-shorts",
			"GET /api/holo/streams/live",
			"GET /api/holo/streams/upcoming",
			"GET /api/holo/channels",
			"GET /api/holo/channels/search",
		},
		"settings": {
			"GET /api/holo/logs",
			"GET /api/holo/settings",
			"POST /api/holo/settings",
			"POST /api/holo/settings/llm",
			"POST /api/holo/names/room",
			"POST /api/holo/names/user",
		},
		"template": {
			"GET /api/holo/templates",
			"GET /api/holo/templates/:key",
			"PUT /api/holo/templates/:key",
			"DELETE /api/holo/templates/:key",
			"POST /api/holo/templates/:key/preview",
			"GET /api/holo/templates/:key/revisions",
			"GET /api/holo/templates/:key/revisions/:id",
		},
		"profile": {
			"GET /api/holo/profiles",
			"GET /api/holo/profiles/name",
		},
		"major_event": {
			"POST /api/holo/majorevent/trigger",
			"POST /api/holo/majorevent/monthly-trigger",
		},
	}
}
