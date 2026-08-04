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

package scraping

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/embedded"
	"go.opentelemetry.io/otel/trace/noop"
)

type startCountingTracerProvider struct {
	embedded.TracerProvider
	starts atomic.Int64
}

func (p *startCountingTracerProvider) Tracer(string, ...trace.TracerOption) trace.Tracer {
	return &startCountingTracer{starts: &p.starts}
}

type startCountingTracer struct {
	embedded.Tracer
	starts *atomic.Int64
}

func (t *startCountingTracer) Start(ctx context.Context, _ string, _ ...trace.SpanStartOption) (context.Context, trace.Span) {
	t.starts.Add(1)
	return noop.NewTracerProvider().Tracer("test-noop").Start(ctx, "test-noop")
}

func TestCreateHTTPClient_DirectHTTP2(t *testing.T) {
	client, transport, err := createHTTPClient(ProxyConfig{})
	require.NoError(t, err)
	require.NotNil(t, client)

	require.NotNil(t, transport, "base transport should be returned")
	assert.True(t, transport.ForceAttemptHTTP2, "direct path should have ForceAttemptHTTP2=true")
}

func TestCreateHTTPClient_ProxyHTTP2(t *testing.T) {
	client, transport, err := createHTTPClient(ProxyConfig{
		Enabled: true,
		URL:     "socks5://proxy.internal:1080",
	})
	require.NoError(t, err)
	require.NotNil(t, client)

	require.NotNil(t, transport, "base transport should be returned")
	assert.False(t, transport.ForceAttemptHTTP2, "proxy path should disable HTTP/2 (single tunnel multiplex is fragile)")
}

func TestCreateHTTPClient_OutboundTracingDisabledUntilAttributeAllowlist(t *testing.T) {
	provider := &startCountingTracerProvider{}
	previousProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
	})

	_, controlSpan := otel.Tracer("scraper-tracing-contract-test").Start(t.Context(), "control")
	controlSpan.End()
	require.Equal(t, int64(1), provider.starts.Load(), "test tracer provider must be active")

	requestURIs := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURIs <- r.URL.RequestURI()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, transport, err := createHTTPClient(ProxyConfig{})
	require.NoError(t, err)
	require.Same(t, transport, client.Transport, "scraper client must retain the configured base transport")

	const sentinelURI = "/youtube/videos/sensitive-video-id?channel=sensitive-channel-id&q=sensitive-query"
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+sentinelURI, http.NoBody)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	if resp == nil {
		t.Fatal("scraper request returned a nil response without an error")
	}
	defer func() {
		mustClose(t, resp.Body)
	}()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, sentinelURI, <-requestURIs)
	assert.Equal(t, int64(1), provider.starts.Load(), "scraper request must not create an outbound client span")
}

func TestCreateHTTPClient_RejectsInvalidProxyURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "unsupported scheme", url: "http://proxy.internal:1080"},
		{name: "missing host", url: "socks5://:1080"},
		{name: "missing port", url: "socks5://proxy.internal"},
		{name: "unbracketed ipv6 host", url: "socks5://2001:db8::1:1080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, transport, err := createHTTPClient(ProxyConfig{
				Enabled: true,
				URL:     tt.url,
			})

			require.Error(t, err)
			assert.Nil(t, client)
			assert.Nil(t, transport)
		})
	}
}

type nilResponseTransport struct{}

func (nilResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, nil
}

func TestNetHTTPPageFetcherNilResponse(t *testing.T) {
	client := NewClient(WithHTTPClient(&http.Client{Transport: nilResponseTransport{}}))
	fetcher := netHTTPPageFetcher{client: client}

	_, err := fetcher.FetchPage(t.Context(), pageFetchRequest{URL: "https://youtube.example/@test/videos"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil response")
}

func TestNetHTTPPageFetcherReturnsFinalURLAfterRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		mustWriteResponse(t, w, "final body")
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(WithHTTPClient(server.Client()))
	resp, err := netHTTPPageFetcher{client: client}.FetchPage(t.Context(), pageFetchRequest{URL: server.URL + "/start"})

	require.NoError(t, err)
	assert.Equal(t, server.URL+"/final", resp.FinalURL)
}
