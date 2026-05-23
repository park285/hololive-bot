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

// Holodex API Integration
//
// 본 코드는 Holodex API (https://holodex.net)를 사용하며, Holodex API Terms of Service를 준수합니다.
//
// Attribution (Holodex API Terms Section 6):
//   - API Provider: Holodex (https://holodex.net)
//   - License: https://holodex.net/api/terms
//   - Disclaimer: THE HOLODEX API IS PROVIDED "AS IS" WITHOUT WARRANTY OF ANY KIND.
//
// See: https://holodex.net/api/terms for full terms.

package apiclient

import (
	"context"
	stdErrors "errors"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/park285/hololive-bot/shared-go/pkg/httputil"
	"golang.org/x/time/rate"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
)

type Requester interface {
	DoRequest(ctx context.Context, method, path string, params url.Values) ([]byte, error)
	IsCircuitOpen() bool
}

type APIClient struct {
	httpClient       *http.Client
	baseURL          string
	apiKey           string
	logger           *slog.Logger
	failureCount     int
	circuitOpenUntil *time.Time
	circuitMu        sync.RWMutex
	rateLimiter      *rate.Limiter // Rate limiter: 초당 10 요청
	semaphore        chan struct{} // Semaphore: 동시 API 요청 수 제한
	distributed      distributedRateLimiter
}

type holodexRequestRetryState struct {
	maxAttempts       int
	maxTimeoutRetries int
	timeoutCount      int
	lastErr           error
}

type distributedRateLimiter interface {
	Allow(ctx context.Context, bucket string, limit int, window time.Duration) (ratelimit.Decision, error)
}

var errNoAPIKeys = stdErrors.New("no Holodex API keys configured")

func NewHolodexAPIClient(
	httpClient *http.Client,
	baseURL string,
	apiKey string,
	logger *slog.Logger,
	distributed distributedRateLimiter,
) *APIClient {
	if httpClient == nil {
		httpClient = httputil.NewExternalAPIClient(constants.APIConfig.HolodexTimeout)
	}
	return &APIClient{
		httpClient:  httpClient,
		baseURL:     baseURL,
		apiKey:      apiKey,
		logger:      logger,
		rateLimiter: rate.NewLimiter(rate.Every(100*time.Millisecond), 1), // 초당 10 요청
		semaphore:   make(chan struct{}, constants.HolodexConcurrencyConfig.MaxConcurrentRequests),
		distributed: distributed,
	}
}

func (c *APIClient) getNextAPIKey() string {
	return c.apiKey
}
