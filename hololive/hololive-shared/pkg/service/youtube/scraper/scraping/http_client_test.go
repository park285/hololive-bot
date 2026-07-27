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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
