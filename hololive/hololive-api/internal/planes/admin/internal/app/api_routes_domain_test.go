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

	"github.com/kapu/hololive-shared/pkg/config"

	"github.com/kapu/hololive-api/internal/planes/admin/internal/server"
)

type routeSpec struct {
	method string
	path   string
}

func TestAPIRouter_DomainRoutesRegistered(t *testing.T) {
	ctx := t.Context()
	logger := slog.New(slog.DiscardHandler)
	apiHandler := &server.Handler{}
	domainHandlers := apiHandler.DomainHandlers()
	authHandler := &server.AuthHandler{}

	appConfig := &config.Config{
		Server: config.ServerConfig{
			APIKey: "test-key",
		},
		CORS: config.CORSConfig{
			AllowedOrigins: []string{"http://localhost:3000"},
		},
	}

	router, err := ProvideAPIRouter(ctx, appConfig, logger, domainHandlers, authHandler, nil, nil, nil)
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
			{method: "GET", path: "/api/holo/stats/system"},
			{method: "GET", path: "/api/holo/stats/channels"},
			{method: "GET", path: "/api/holo/stats/youtube/community-shorts"},
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
