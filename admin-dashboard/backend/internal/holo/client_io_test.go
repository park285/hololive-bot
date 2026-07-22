package holo

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kapu/admin-dashboard/internal/httpx"
	"github.com/kapu/hololive-shared/pkg/httpbody"
)

type holoRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f holoRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type holoCloseErrorBody struct {
	io.Reader
	err error
}

func (b holoCloseErrorBody) Close() error { return b.err }

func TestProxyPreservesTransportCancellationCause(t *testing.T) {
	client := &Client{
		baseURL: "http://holo.test",
		http: &http.Client{Transport: holoRoundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, context.Canceled
		})},
	}

	_, err := client.Proxy(context.Background(), http.MethodGet, "/status", nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Proxy() error = %v, want context.Canceled cause", err)
	}
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Status != http.StatusBadGateway {
		t.Fatalf("Proxy() error = %v (%T), want 502 AppError", err, err)
	}
	if appErr.Body.Error != "Service unavailable" {
		t.Fatalf("Proxy() response error = %q, want existing bad-gateway contract", appErr.Body.Error)
	}
}

func TestProxyRejectsOversizedResponseAndPreservesCause(t *testing.T) {
	client := &Client{
		baseURL: "http://holo.test",
		http: &http.Client{Transport: holoRoundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(io.LimitReader(infiniteByteReader{}, maxProxyBodyBytes+1)),
			}, nil
		})},
	}

	_, err := client.Proxy(context.Background(), http.MethodGet, "/status", nil, nil)
	if !errors.Is(err, httpbody.ErrTooLarge) {
		t.Fatalf("Proxy() error = %v, want httpbody.ErrTooLarge cause", err)
	}
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Status != http.StatusBadGateway {
		t.Fatalf("Proxy() error = %v (%T), want 502 AppError", err, err)
	}
}

func TestProxyPreservesResponseCloseFailure(t *testing.T) {
	wantErr := errors.New("close failed")
	client := &Client{
		baseURL: "http://holo.test",
		http: &http.Client{Transport: holoRoundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       holoCloseErrorBody{Reader: strings.NewReader(`{"ok":true}`), err: wantErr},
			}, nil
		})},
	}

	_, err := client.Proxy(context.Background(), http.MethodGet, "/status", nil, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Proxy() error = %v, want close failure cause", err)
	}
}

func TestProxyDrains5xxBodyForKeepAliveReuse(t *testing.T) {
	var newConnections atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		if _, err := io.WriteString(w, "temporary upstream failure"); err != nil {
			t.Errorf("write upstream failure response: %v", err)
		}
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConnections.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)

	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	for range 2 {
		_, proxyErr := client.Proxy(context.Background(), http.MethodGet, "/status", nil, nil)
		var appErr *httpx.AppError
		if !errors.As(proxyErr, &appErr) || appErr.Status != http.StatusBadGateway {
			t.Fatalf("Proxy() error = %v, want 502", proxyErr)
		}
	}
	client.http.CloseIdleConnections()
	if got := newConnections.Load(); got != 1 {
		t.Fatalf("new connections = %d, want 1 after draining sequential 5xx responses", got)
	}
}

type infiniteByteReader struct{}

func (infiniteByteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}
