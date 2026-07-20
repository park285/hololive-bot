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

package producerruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
	json "github.com/park285/shared-go/pkg/json"
)

func TestProvideAPIServer(t *testing.T) {
	router := gin.New()
	router.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	server := sharedserver.NewH2CServer(":31004", router, "test-http")
	if server == nil {
		t.Fatal("NewH2CServer() returned nil")
	}
	if server.Addr != ":31004" {
		t.Fatalf("server.Addr = %q, want %q", server.Addr, ":31004")
	}
	if server.ReadHeaderTimeout != constants.ServerTimeout.ReadHeader {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", server.ReadHeaderTimeout, constants.ServerTimeout.ReadHeader)
	}
	if server.ReadTimeout != constants.ServerTimeout.Read {
		t.Fatalf("ReadTimeout = %s, want %s", server.ReadTimeout, constants.ServerTimeout.Read)
	}
	if server.WriteTimeout != constants.ServerTimeout.Write {
		t.Fatalf("WriteTimeout = %s, want %s", server.WriteTimeout, constants.ServerTimeout.Write)
	}
	if server.IdleTimeout != constants.ServerTimeout.Idle {
		t.Fatalf("IdleTimeout = %s, want %s", server.IdleTimeout, constants.ServerTimeout.Idle)
	}
	if server.MaxHeaderBytes != constants.ServerTimeout.MaxHeaderBytes {
		t.Fatalf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, constants.ServerTimeout.MaxHeaderBytes)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ping", http.NoBody)
	rr := httptest.NewRecorder()
	server.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("/ping status = %d, want %d", rr.Code, http.StatusOK)
	}
	if strings.TrimSpace(rr.Body.String()) != "pong" {
		t.Fatalf("/ping body = %q, want %q", rr.Body.String(), "pong")
	}

	t.Run("wraps handler with otelhttp", func(t *testing.T) {
		recorder := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
		prev := otel.GetTracerProvider()
		otel.SetTracerProvider(tp)
		t.Cleanup(func() {
			otel.SetTracerProvider(prev)
			if err := tp.Shutdown(context.Background()); err != nil {
				t.Errorf("shutdown tracer provider: %v", err)
			}
		})

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ping", http.NoBody)
		rr := httptest.NewRecorder()
		server.Handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("/ping status = %d, want %d", rr.Code, http.StatusOK)
		}

		spans := recorder.Ended()
		if len(spans) == 0 {
			t.Fatal("expected otelhttp to emit at least one span")
		}
		if got := spans[0].Name(); got != "test-http" {
			t.Fatalf("span name = %q, want %q", got, "test-http")
		}
	})
}

func TestBuildYouTubeProducerHTTPRouter(t *testing.T) {
	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})

	const apiKey = "test-key"
	readiness := newReadinessState(ingestionRuntimeFeatures{
		youtubeEnabled:   true,
		photoSyncEnabled: false,
	})
	router, err := buildYouTubeProducerHTTPRouter(
		t.Context(),
		&config.Config{Server: config.ServerConfig{APIKey: apiKey}},
		testLogger(),
		readiness,
	)
	if err != nil {
		t.Fatalf("buildYouTubeProducerHTTPRouter() error = %v", err)
	}
	if router == nil {
		t.Fatal("buildYouTubeProducerHTTPRouter() returned nil router")
	}

	t.Run("health endpoint", func(t *testing.T) { assertHealthEndpoint(t, router) })
	t.Run("metrics endpoint requires api key", func(t *testing.T) {
		assertProtectedMetricsEndpoint(t, router, apiKey)
	})
	t.Run("public ready endpoint", func(t *testing.T) {
		assertPublicReadyEndpointNotStarted(t, router)
	})
	t.Run("public ready endpoint after runtime start", func(t *testing.T) {
		readiness.MarkRunning()
		assertPublicReadyEndpointRunning(t, router)
	})
	t.Run("unknown endpoint", func(t *testing.T) { assertUnknownEndpoint(t, router) })
}

func TestBuildYouTubeProducerHTTPServersReturnsRouterError(t *testing.T) {
	originalTrustedProxies := constants.ServerConfig.TrustedProxies
	constants.ServerConfig.TrustedProxies = []string{"not-a-valid-proxy-entry"}
	t.Cleanup(func() {
		constants.ServerConfig.TrustedProxies = originalTrustedProxies
	})

	servers, err := buildYouTubeProducerHTTPServers(
		t.Context(),
		&config.Config{Server: config.ServerConfig{APIKey: "test-key"}},
		testLogger(),
		youtubeProducerRuntimeName,
		newReadinessState(ingestionRuntimeFeatures{}),
	)
	if err == nil {
		t.Fatal("buildYouTubeProducerHTTPServers() error = nil, want trusted proxy error")
	}
	if servers != nil {
		t.Fatal("buildYouTubeProducerHTTPServers() returned non-nil servers on error")
	}
	if !strings.Contains(err.Error(), "build youtube producer router: set trusted proxies") {
		t.Fatalf("buildYouTubeProducerHTTPServers() error = %q", err.Error())
	}
}

func assertHealthEndpoint(t *testing.T, router http.Handler) {
	t.Helper()
	rr := serveRuntimeTestRequest(t, t.Context(), router, "/health", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("/health status = %d, want %d", rr.Code, http.StatusOK)
	}
	payload := decodeRuntimeTestPayload(t, rr)
	requirePayloadStatus(t, payload, "ok")
}

func assertProtectedMetricsEndpoint(t *testing.T, router http.Handler, apiKey string) {
	t.Helper()

	rr := serveRuntimeTestRequest(t, t.Context(), router, "/metrics", "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("/metrics status without api key = %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	rrWithKey := serveRuntimeTestRequest(t, t.Context(), router, "/metrics", apiKey)
	if rrWithKey.Code != http.StatusOK {
		t.Fatalf("/metrics status with api key = %d, want %d", rrWithKey.Code, http.StatusOK)
	}
	if rrWithKey.Body.Len() == 0 {
		t.Fatal("/metrics body with api key is empty")
	}
	if !strings.Contains(rrWithKey.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("Content-Type = %q, want contains text/plain", rrWithKey.Header().Get("Content-Type"))
	}
}

func assertPublicReadyEndpointNotStarted(t *testing.T, router http.Handler) {
	t.Helper()
	rr := serveRuntimeTestRequest(t, t.Context(), router, "/ready", "")
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("/ready status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
	payload := decodeRuntimeTestPayload(t, rr)
	requirePayloadStatus(t, payload, "not_ready")
	requirePublicReadyPayload(t, payload)
}

func assertPublicReadyEndpointRunning(t *testing.T, router http.Handler) {
	t.Helper()
	rr := serveRuntimeTestRequest(t, t.Context(), router, "/ready", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("/ready status = %d, want %d", rr.Code, http.StatusOK)
	}
	payload := decodeRuntimeTestPayload(t, rr)
	requirePayloadStatus(t, payload, "ready")
	requirePublicReadyPayload(t, payload)
}

func requirePublicReadyPayload(t *testing.T, payload map[string]any) {
	t.Helper()
	for _, key := range []string{"runtime", "http_server_started", "youtube_enabled", "photo_sync_enabled", "valkey_available", "budget_backend_available"} {
		if _, exists := payload[key]; exists {
			t.Fatalf("public /ready payload must omit %q: %v", key, payload)
		}
	}
}

func assertUnknownEndpoint(t *testing.T, router http.Handler) {
	t.Helper()
	rr := serveRuntimeTestRequest(t, t.Context(), router, "/unknown", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("/unknown status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func serveRuntimeTestRequest(t *testing.T, ctx context.Context, router http.Handler, target, apiKey string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, target, http.NoBody)
	if apiKey != "" {
		req.Header.Set(middleware.APIKeyHeader, apiKey)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func decodeRuntimeTestPayload(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return payload
}

func requirePayloadStatus(t *testing.T, payload map[string]any, want string) {
	t.Helper()
	got, ok := payload["status"].(string)
	if !ok {
		t.Fatalf("status = %#v, want string %q", payload["status"], want)
	}
	if got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}
