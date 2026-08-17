package providerhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func TestWrapProviderHTTPDoerRejectsNil(t *testing.T) {
	t.Parallel()
	if _, err := WrapProviderHTTPDoer(nil); err == nil || collecterr.ClassOf(err) != collecterr.ClassConfiguration {
		t.Fatalf("nil doer = %v", err)
	}
}

func TestNewProviderHTTPClientRejectsNonPositiveTimeouts(t *testing.T) {
	t.Parallel()
	cfg := testTransportConfig()
	cfg.RequestTimeout = 0
	_, err := NewProviderHTTPClient(cfg)
	if err == nil || collecterr.ClassOf(err) != collecterr.ClassConfiguration {
		t.Fatalf("zero RequestTimeout = %v", err)
	}
}

func TestNewProviderHTTPClientAppliesCapsAndRedirectPolicy(t *testing.T) {
	t.Parallel()
	cfg := testTransportConfig()
	cfg.MaxConnsPerHost = 4
	cfg.MaxIdleConnsPerHost = 2
	client, err := NewProviderHTTPClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("close client: %v", closeErr)
		}
	})
	if client.transport == nil || client.transport.MaxConnsPerHost != 4 || client.transport.MaxIdleConnsPerHost != 2 {
		t.Fatalf("transport caps = %#v", client.transport)
	}
	if client.transport.DisableCompression {
		t.Fatal("DisableCompression must stay false")
	}
	if err := client.client.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect = %v", err)
	}
}

func TestHTTP013OwnedClientClosesIdleConnectionsOnce(t *testing.T) {
	t.Parallel()
	client, err := NewProviderHTTPClient(testTransportConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !client.owned || client.transport == nil {
		t.Fatal("owned client must keep its transport")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHTTP012InjectedDoerIsNotClosedOrMutated(t *testing.T) {
	t.Parallel()
	var closes atomic.Int32
	original := &http.Client{
		Timeout: 3 * time.Second,
		Transport: closeCountingTransport{
			base:   http.DefaultTransport,
			closes: &closes,
		},
	}
	wrapped, err := WrapProviderHTTPDoer(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := wrapped.Close(); err != nil {
		t.Fatal(err)
	}
	if original.Timeout != 3*time.Second {
		t.Fatalf("timeout mutated to %s", original.Timeout)
	}
	if original.CheckRedirect != nil {
		t.Fatal("injected CheckRedirect was mutated")
	}
	if closes.Load() != 0 {
		t.Fatalf("CloseIdleConnections called %d times", closes.Load())
	}
}

func TestHTTP006RedirectNotFollowed(t *testing.T) {
	t.Parallel()
	var secondHits atomic.Int32
	var secondKey string
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits.Add(1)
		secondKey = r.Header.Get("X-APIKEY")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(second.Close)
	var firstHits atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHits.Add(1)
		http.Redirect(w, r, second.URL+"/other", http.StatusFound)
	}))
	t.Cleanup(first.Close)
	client, err := NewProviderHTTPClient(testTransportConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("close client: %v", closeErr)
		}
	})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, first.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-APIKEY", "secret-key")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || resp.Body == nil {
		t.Fatal("response or response body is nil")
	}
	t.Cleanup(func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close response: %v", closeErr)
		}
	})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if firstHits.Load() != 1 || secondHits.Load() != 0 || secondKey != "" {
		t.Fatalf("hits first=%d second=%d key=%q", firstHits.Load(), secondHits.Load(), secondKey)
	}
}

type closeCountingTransport struct {
	base   http.RoundTripper
	closes *atomic.Int32
}

func (t closeCountingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(req)
}

func (t closeCountingTransport) CloseIdleConnections() {
	t.closes.Add(1)
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func testTransportConfig() ProviderTransportConfig {
	return ProviderTransportConfig{
		RequestTimeout:        time.Second,
		DialTimeout:           time.Second,
		TLSHandshakeTimeout:   time.Second,
		ResponseHeaderTimeout: time.Second,
		IdleConnTimeout:       time.Second,
		MaxConnsPerHost:       2,
		MaxIdleConnsPerHost:   1,
	}
}
