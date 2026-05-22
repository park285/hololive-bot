package runtime

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestStartHTTPServerReportsListenErrorPrefix(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)

	StartHTTPServer(&http.Server{Addr: "invalid::addr"}, nil, errCh)

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected HTTP server error, got nil")
		}
		if !strings.Contains(err.Error(), "HTTP server error") {
			t.Fatalf("error = %q, want HTTP server error prefix", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for HTTP server error")
	}
}

func TestShutdownHTTPServerReportsShutdownErrorPrefix(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(handlerStarted)
			<-releaseHandler
		}),
	}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()
	defer func() {
		close(releaseHandler)
		_ = server.Close()
		<-serveErr
	}()

	clientErr := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			_ = resp.Body.Close()
		}
		clientErr <- err
	}()

	select {
	case <-handlerStarted:
	case err := <-clientErr:
		t.Fatalf("request failed before handler started: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = ShutdownHTTPServer(server, ctx)
	if err == nil {
		t.Fatal("expected shutdown error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if !strings.Contains(err.Error(), "HTTP server shutdown failed") {
		t.Fatalf("error = %q, want HTTP server shutdown failed prefix", err)
	}
}
