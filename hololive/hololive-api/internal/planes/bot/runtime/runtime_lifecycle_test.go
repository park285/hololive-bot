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

package botruntime

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"
	"github.com/quic-go/quic-go/http3"
)

func TestBotRuntimeClose_CallsCleanup(t *testing.T) {
	t.Parallel()

	calls := 0
	runtime := &BotRuntime{
		Managed: lifecycle.NewManaged(func() { calls++ }),
	}

	runtime.Close()

	if calls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", calls)
	}
}

func TestBotRuntimeStartHTTPServer_Branches(t *testing.T) {
	t.Parallel()

	t.Run("nil runtime or nil server", func(t *testing.T) {
		t.Parallel()

		var nilRuntime *BotRuntime

		nilRuntime.StartHTTPServer(make(chan error, 1))

		runtime := &BotRuntime{}
		runtime.StartHTTPServer(make(chan error, 1))
	})

	t.Run("listen error pushes err channel", func(t *testing.T) {
		t.Parallel()

		runtime := &BotRuntime{
			H3Server: &http3.Server{Addr: "invalid::addr"},
		}
		errCh := make(chan error, 1)

		runtime.StartHTTPServer(errCh)

		select {
		case err := <-errCh:
			if err == nil || !strings.Contains(err.Error(), "HTTP/3 server error") {
				t.Fatalf("unexpected error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for HTTP server error")
		}
	})

	t.Run("short-link listen error pushes err channel", func(t *testing.T) {
		t.Parallel()

		runtime := &BotRuntime{
			ShortLinkServer: &http.Server{Addr: "invalid::addr", ReadHeaderTimeout: time.Second},
		}
		errCh := make(chan error, 1)

		runtime.StartHTTPServer(errCh)

		select {
		case err := <-errCh:
			if err == nil || !strings.Contains(err.Error(), "short-link server error") {
				t.Fatalf("unexpected error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for short-link server error")
		}
	})
}

func TestBotRuntimeStartAndHelpers_NoPanicOnNilComponents(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	runtime := &BotRuntime{
		Logger: logger,
	}

	runtime.Start(t.Context(), nil)
	runtime.logError("expected test error", errors.New("boom"))

	if logBuf.Len() == 0 {
		t.Fatal("expected runtime helpers to write logs")
	}
}

func newShortLinkTestServer(t *testing.T) (*http.Server, string, <-chan error) {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	if listener == nil {
		t.Fatal("net.ListenConfig.Listen returned a nil listener")
	}

	server := &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusNoContent)
		}),
		ReadHeaderTimeout: time.Second,
	}
	serveErr := make(chan error, 1)

	go func() {
		serveErr <- server.Serve(listener)
	}()

	return server, listener.Addr().String(), serveErr
}

func assertShortLinkResponds(t *testing.T, addr string) {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	if request == nil {
		t.Fatal("http.NewRequestWithContext returned a nil request")
	}

	client := &http.Client{Timeout: 2 * time.Second}

	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	if response == nil {
		t.Fatal("http.Client.Do returned a nil response")
	}

	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatalf("close response body: %v", closeErr)
	}

	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("short-link status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
}

func assertShortLinkListenerClosed(t *testing.T, addr string) {
	t.Helper()

	dialer := &net.Dialer{Timeout: time.Second}

	connection, err := dialer.DialContext(t.Context(), "tcp", addr)
	if err == nil {
		if closeErr := connection.Close(); closeErr != nil {
			t.Fatalf("close unexpected post-shutdown connection: %v", closeErr)
		}

		t.Fatal("short-link listener still accepts connections after shutdown")
	}
}

func TestBotRuntimeShutdownHTTPServer_DrainsShortLinkListener(t *testing.T) {
	t.Parallel()

	server, addr, serveErr := newShortLinkTestServer(t)

	assertShortLinkResponds(t, addr)

	runtime := &BotRuntime{ShortLinkServer: server}
	shutdownContext, cancel := context.WithTimeout(t.Context(), 2*time.Second)

	defer cancel()

	if err := runtime.ShutdownHTTPServer(shutdownContext); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Serve() error = %v, want http.ErrServerClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("short-link listener did not stop after shutdown")
	}

	assertShortLinkListenerClosed(t, addr)
}

func TestBotRuntimeRun_ExitsOnServerError(t *testing.T) {
	t.Parallel()

	runtime := &BotRuntime{
		Logger:     slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		ServerAddr: "invalid::addr",
		H3Server:   &http3.Server{Addr: "invalid::addr"},
	}

	errCh := make(chan error, 1)

	go func() {
		errCh <- runtime.Run()
	}()

	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "HTTP/3 server error") {
			t.Fatalf("unexpected Run() error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not exit on server error")
	}
}
