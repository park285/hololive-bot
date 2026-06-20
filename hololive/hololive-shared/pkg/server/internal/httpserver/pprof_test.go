package httpserver

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

func TestNewPprofServerServesProfile(t *testing.T) {
	ctx := context.Background()
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	server := NewPprofServer(listener.Addr().String(), "")
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			t.Errorf("serve pprof: %v", err)
		}
	}()
	t.Cleanup(func() {
		if err := server.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			t.Errorf("close pprof server: %v", err)
		}
	})

	client := &http.Client{Timeout: 10 * time.Second}
	url := "http://" + listener.Addr().String() + "/debug/pprof/profile?seconds=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatalf("new request %s: %v", url, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	if resp == nil {
		t.Fatalf("GET %s returned nil response", url)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("profile body is empty")
	}
}

func TestNewPprofServerRequiresAPIKey(t *testing.T) {
	server := NewPprofServer("127.0.0.1:0", "test-key")
	ctx := context.Background()

	missing := httptest.NewRequestWithContext(ctx, http.MethodGet, "/debug/pprof/", http.NoBody)
	missingRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(missingRecorder, missing)
	if missingRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing key status = %d, want %d", missingRecorder.Code, http.StatusUnauthorized)
	}

	wrong := httptest.NewRequestWithContext(ctx, http.MethodGet, "/debug/pprof/", http.NoBody)
	wrong.Header.Set(middleware.APIKeyHeader, "wrong-key")
	wrongRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(wrongRecorder, wrong)
	if wrongRecorder.Code != http.StatusForbidden {
		t.Fatalf("wrong key status = %d, want %d", wrongRecorder.Code, http.StatusForbidden)
	}

	ok := httptest.NewRequestWithContext(ctx, http.MethodGet, "/debug/pprof/", http.NoBody)
	ok.Header.Set(middleware.APIKeyHeader, "test-key")
	okRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(okRecorder, ok)
	if okRecorder.Code != http.StatusOK {
		t.Fatalf("valid key status = %d, want %d", okRecorder.Code, http.StatusOK)
	}
}

func TestNewPprofServerKeylessDeniedOnNonLoopback(t *testing.T) {
	ctx := context.Background()

	denied := []string{"0.0.0.0:6060", ":6060", "192.168.1.5:6060"}
	for _, addr := range denied {
		server := NewPprofServer(addr, "")
		rec := httptest.NewRecorder()
		server.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/debug/pprof/", http.NoBody))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("keyless pprof on %q status = %d, want %d", addr, rec.Code, http.StatusForbidden)
		}
	}

	allowed := []string{"127.0.0.1:6060", "[::1]:6060", "localhost:6060"}
	for _, addr := range allowed {
		server := NewPprofServer(addr, "")
		rec := httptest.NewRecorder()
		server.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/debug/pprof/", http.NoBody))
		if rec.Code != http.StatusOK {
			t.Fatalf("keyless pprof on loopback %q status = %d, want %d", addr, rec.Code, http.StatusOK)
		}
	}
}

func TestNewRuntimeHTTPServersPprofGate(t *testing.T) {
	certFile, keyFile := writeH3LocalhostCertificate(t)

	withPprof, err := NewRuntimeHTTPServers(&config.ServerConfig{
		Port:           30001,
		HTTPTransports: []string{"h3"},
		H3Addr:         "127.0.0.1:0",
		H3CertFile:     certFile,
		H3KeyFile:      keyFile,
		PprofAddr:      "127.0.0.1:0",
	}, http.NotFoundHandler(), "test.http")
	if err != nil {
		t.Fatalf("NewRuntimeHTTPServers() error = %v", err)
	}
	if withPprof.Pprof == nil {
		t.Fatal("Pprof = nil, want server")
	}

	noPprof, err := NewRuntimeHTTPServers(&config.ServerConfig{
		Port:           30001,
		HTTPTransports: []string{"h3"},
		H3Addr:         "127.0.0.1:0",
		H3CertFile:     certFile,
		H3KeyFile:      keyFile,
	}, http.NotFoundHandler(), "test.http")
	if err != nil {
		t.Fatalf("NewRuntimeHTTPServers() error = %v", err)
	}
	if noPprof.Pprof != nil {
		t.Fatal("Pprof != nil with empty PprofAddr")
	}
}
