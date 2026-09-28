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
	"time"

	backoff "github.com/kapu/hololive-shared/internal/service/youtube/scraper/scraping/backoff"
	"github.com/kapu/hololive-shared/internal/service/youtube/scraper/ua"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	ratelimiter "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

type Client struct {
	httpClient            *http.Client    // WithHTTPClient로 주입했거나 initHTTPClients가 만든 직접 연결 client
	transport             *http.Transport // initHTTPClients가 만든 transport. 주입 client면 nil이다.
	uaProvider            ua.Provider
	rateLimiter           *ratelimiter.RateLimiter
	backoffState          *backoff.BackoffState
	stateStore            stateStore
	channelHealthPolicy   ChannelHealthPolicy
	channelHealthDisabled bool
	channelHealth         *ChannelHealthStore
	snapshotSink          SnapshotSink
	snapshotPolicy        SnapshotPolicy

	communityMissing *cacheState

	// config는 runtime이 읽은 YouTube scraper 설정이다. HTTP timeout, 응답 본문 상한, 상태 TTL,
	// 분산 rate limit bucket 접두사의 유일한 출처다.
	config settings.YouTubeConfig
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

func WithRateLimiter(rl *ratelimiter.RateLimiter) ClientOption {
	return func(c *Client) {
		c.rateLimiter = rl
	}
}

func WithStateStore(store stateStore) ClientOption {
	return func(c *Client) {
		c.stateStore = store
	}
}

func WithChannelHealthPolicy(policy *ChannelHealthPolicy) ClientOption {
	return func(c *Client) {
		if policy == nil {
			return
		}

		c.channelHealthPolicy = *policy
	}
}

func WithChannelHealthDisabled() ClientOption {
	return func(c *Client) {
		c.channelHealthDisabled = true
	}
}

func WithSnapshotSink(sink SnapshotSink) ClientOption {
	return func(c *Client) {
		c.snapshotSink = sink
	}
}

func WithSnapshotPolicy(policy SnapshotPolicy) ClientOption {
	return func(c *Client) {
		c.snapshotPolicy = policy
	}
}

// NewClient는 runtime 설정(settings.Config.YouTube)을 필수로 받는다. 패키지 기본값으로 대신하면
// YOUTUBE_SCRAPER_* 같은 운영 설정이 조용히 무시된다.
func NewClient(config settings.YouTubeConfig, opts ...ClientOption) *Client {
	c := &Client{
		config:              config,
		uaProvider:          ua.NewRotatingProvider(ua.StrategySessionTTL, 45*time.Minute),
		rateLimiter:         ratelimiter.New(3 * time.Second),
		backoffState:        backoff.NewBackoffState(),
		channelHealthPolicy: DefaultChannelHealthPolicy(),
		snapshotPolicy:      DefaultSnapshotPolicy(),
	}

	for _, opt := range opts {
		opt(c)
	}

	// stateStore 주입 후 cacheState 초기화
	c.initStateManagers()
	c.initHTTPClients()

	return c
}
