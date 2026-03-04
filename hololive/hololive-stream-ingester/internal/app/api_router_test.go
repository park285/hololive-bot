package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
)

func TestProvideAPIServer(t *testing.T) {
	router := gin.New()
	router.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	server := ProvideAPIServer(":31004", router)
	if server == nil {
		t.Fatal("ProvideAPIServer() returned nil")
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
}

func TestProvideHealthOnlyRouter(t *testing.T) {
	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})

	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	router, err := ProvideHealthOnlyRouter(ctx, testLogger())
	if err != nil {
		t.Fatalf("ProvideHealthOnlyRouter() error = %v", err)
	}
	if router == nil {
		t.Fatal("ProvideHealthOnlyRouter() returned nil router")
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

	t.Run("unknown endpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("/unknown status = %d, want %d", rr.Code, http.StatusNotFound)
		}
	})
}

func TestBuildStreamIngesterHTTPServer(t *testing.T) {
	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})

	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Port: 31004,
		},
	}

	server, err := buildStreamIngesterHTTPServer(ctx, cfg, testLogger())
	if err != nil {
		t.Fatalf("buildStreamIngesterHTTPServer() error = %v", err)
	}
	if server == nil {
		t.Fatal("buildStreamIngesterHTTPServer() returned nil server")
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
}
