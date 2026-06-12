package httpserver

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
)

func TestNewPprofServerServesProfile(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	server := NewPprofServer(listener.Addr().String())
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	client := &http.Client{Timeout: 10 * time.Second}
	url := "http://" + listener.Addr().String() + "/debug/pprof/profile?seconds=1"
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

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

func TestNewRuntimeHTTPServersPprofGate(t *testing.T) {
	certFile, keyFile := writeH3LocalhostCertificate(t)

	withPprof, err := NewRuntimeHTTPServers(config.ServerConfig{
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

	noPprof, err := NewRuntimeHTTPServers(config.ServerConfig{
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
