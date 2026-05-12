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

package scraper

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/ua"
)

type Client struct {
	httpClient       *http.Client // 테스트/특수 경로용 고정 클라이언트
	directHTTPClient *http.Client
	proxyHTTPClient  *http.Client
	directTransport  *http.Transport
	proxyTransport   *http.Transport
	activeHTTPClient atomic.Pointer[http.Client]
	proxyEnabled     atomic.Bool
	uaProvider       ua.Provider
	rateLimiter      *RateLimiter
	backoffState     *BackoffState
	proxyConfig      ProxyConfig
	stateStore       stateStore
	fetcherEngine    FetcherEngine

	communityMissing *cacheState
	videoRSSBackoff  *cacheState
}

type ClientOption func(*Client)

func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

func WithUAProvider(provider ua.Provider) ClientOption {
	return func(c *Client) {
		c.uaProvider = provider
	}
}

func WithRateLimiter(rl *RateLimiter) ClientOption {
	return func(c *Client) {
		c.rateLimiter = rl
	}
}

func WithStateStore(store stateStore) ClientOption {
	return func(c *Client) {
		c.stateStore = store
	}
}

func WithFetcherEngine(engine FetcherEngine) ClientOption {
	return func(c *Client) {
		c.fetcherEngine = normalizeFetcherEngine(engine)
	}
}

func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		uaProvider:    ua.NewRotatingProvider(ua.StrategySessionTTL, 45*time.Minute),
		rateLimiter:   NewRateLimiter(3 * time.Second),
		backoffState:  NewBackoffState(),
		fetcherEngine: FetcherEngineNetHTTP,
	}

	// 옵션 적용 (프록시 설정 포함)
	for _, opt := range opts {
		opt(c)
	}

	// stateStore 주입 후 cacheState 초기화
	c.initStateManagers()
	c.initHTTPClients()

	return c
}
