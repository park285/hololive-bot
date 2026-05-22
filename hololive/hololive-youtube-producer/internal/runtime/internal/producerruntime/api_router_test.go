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
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
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

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
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
			_ = tp.Shutdown(context.Background())
		})

		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
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

func TestProvideHealthOnlyRouter(t *testing.T) {
	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})

	ctx := t.Context()

	readiness := newReadinessState(youtubeProducerRuntimeName, ingestionRuntimeFeatures{
		youtubeEnabled:   true,
		photoSyncEnabled: false,
	})
	router, err := sharedserver.NewHealthOnlyRuntimeRouter(ctx, testLogger(), "", func(opts *sharedserver.RuntimeRouterOptions) {
		opts.EnableGzip = true
		opts.ReadyResponder = func(c *gin.Context) {
			statusCode, payload := readiness.Response()
			c.JSON(statusCode, payload)
		}
	})
	if err != nil {
		t.Fatalf("NewHealthOnlyRuntimeRouter() error = %v", err)
	}
	if router == nil {
		t.Fatal("NewHealthOnlyRuntimeRouter() returned nil router")
	}
	if gin.Mode() != gin.ReleaseMode {
		t.Fatalf("gin mode = %q, want %q", gin.Mode(), gin.ReleaseMode)
	}
	if router.TrustedPlatform != gin.PlatformCloudflare {
		t.Fatalf("TrustedPlatform = %q, want %q", router.TrustedPlatform, gin.PlatformCloudflare)
	}

	t.Run("health endpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("/health status = %d, want %d", rr.Code, http.StatusOK)
		}

		var payload map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatalf("unmarshal /health response: %v", err)
		}
		status, _ := payload["status"].(string)
		if status != "ok" {
			t.Fatalf("health status = %q, want %q", status, "ok")
		}
	})

	t.Run("metrics endpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("/metrics status = %d, want %d", rr.Code, http.StatusOK)
		}
		if rr.Body.Len() == 0 {
			t.Fatal("/metrics body is empty")
		}
		if !strings.Contains(rr.Header().Get("Content-Type"), "text/plain") {
			t.Fatalf("Content-Type = %q, want contains text/plain", rr.Header().Get("Content-Type"))
		}
	})

	t.Run("metrics endpoint requires api key when configured", func(t *testing.T) {
		protectedRouter, err := sharedserver.NewHealthOnlyRuntimeRouter(ctx, testLogger(), "test-key", func(opts *sharedserver.RuntimeRouterOptions) {
			opts.EnableGzip = true
			opts.ReadyResponder = func(c *gin.Context) {
				statusCode, payload := readiness.Response()
				c.JSON(statusCode, payload)
			}
		})
		if err != nil {
			t.Fatalf("NewHealthOnlyRuntimeRouter() protected error = %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rr := httptest.NewRecorder()
		protectedRouter.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("/metrics status without api key = %d, want %d", rr.Code, http.StatusUnauthorized)
		}

		reqWithKey := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		reqWithKey.Header.Set(middleware.APIKeyHeader, "test-key")
		rrWithKey := httptest.NewRecorder()
		protectedRouter.ServeHTTP(rrWithKey, reqWithKey)
		if rrWithKey.Code != http.StatusOK {
			t.Fatalf("/metrics status with api key = %d, want %d", rrWithKey.Code, http.StatusOK)
		}
		if rrWithKey.Body.Len() == 0 {
			t.Fatal("/metrics body with api key is empty")
		}
	})

	t.Run("ready endpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("/ready status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
		}

		var payload map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatalf("unmarshal /ready response: %v", err)
		}
		status, _ := payload["status"].(string)
		if status != "not_ready" {
			t.Fatalf("ready status = %q, want %q", status, "not_ready")
		}
		if got, _ := payload["runtime"].(string); got != youtubeProducerRuntimeName {
			t.Fatalf("runtime = %q, want %q", got, youtubeProducerRuntimeName)
		}
	})

	t.Run("ready endpoint after runtime start", func(t *testing.T) {
		readiness.MarkRunning()

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("/ready status = %d, want %d", rr.Code, http.StatusOK)
		}

		var payload map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatalf("unmarshal /ready response: %v", err)
		}
		if got, _ := payload["status"].(string); got != "ready" {
			t.Fatalf("status = %q, want %q", got, "ready")
		}
		if got, _ := payload["youtube_enabled"].(bool); !got {
			t.Fatal("youtube_enabled = false, want true")
		}
	})

	t.Run("unknown endpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("/unknown status = %d, want %d", rr.Code, http.StatusNotFound)
		}
	})
}

func TestBuildYouTubeProducerHTTPServer(t *testing.T) {
	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})

	ctx := t.Context()

	appConfig := &config.Config{
		Server: config.ServerConfig{
			Port: 31004,
		},
	}

	readiness := newReadinessState(youtubeProducerRuntimeName, ingestionRuntimeFeatures{
		youtubeEnabled:   false,
		photoSyncEnabled: true,
	})
	server, err := buildYouTubeProducerHTTPServer(ctx, appConfig, testLogger(), youtubeProducerRuntimeName, readiness)
	if err != nil {
		t.Fatalf("buildYouTubeProducerHTTPServer() error = %v", err)
	}
	if server == nil {
		t.Fatal("buildYouTubeProducerHTTPServer() returned nil server")
	}
	if server.Addr != ":31004" {
		t.Fatalf("server.Addr = %q, want %q", server.Addr, ":31004")
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	server.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("/health via built server status = %d, want %d", rr.Code, http.StatusOK)
	}

	readyReq := httptest.NewRequest(http.MethodGet, "/ready", nil)
	readyRR := httptest.NewRecorder()
	server.Handler.ServeHTTP(readyRR, readyReq)
	if readyRR.Code != http.StatusServiceUnavailable {
		t.Fatalf("/ready via built server status = %d, want %d", readyRR.Code, http.StatusServiceUnavailable)
	}
}
