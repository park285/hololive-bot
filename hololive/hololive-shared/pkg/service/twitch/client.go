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

package twitch

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/shared-go/pkg/httputil"
	"github.com/park285/shared-go/pkg/json"
	"github.com/park285/shared-go/pkg/jsonutil"
)

const maxUserLoginsPerRequest = 100

type ClientConfig struct {
	HTTPClient           *http.Client
	ClientID             string
	ClientSecret         string
	BaseURL              string
	AuthURL              string
	Timeout              time.Duration
	TokenRefreshSkew     time.Duration
	MaxResponseBodyBytes int64
}

type Client struct {
	httpClient           *http.Client
	clientID             string
	clientSecret         string
	baseURL              string
	authURL              string
	tokenRefreshSkew     time.Duration
	maxResponseBodyBytes int64
	logger               *slog.Logger

	token       atomic.Value // string
	tokenExpiry atomic.Value // time.Time
	tokenMu     sync.Mutex

	breaker *util.Breaker
}

func NewClient(cfg ClientConfig, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}

	d := config.DefaultTwitchOperationalConfig()

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = d.BaseURL
	}
	authURL := cfg.AuthURL
	if authURL == "" {
		authURL = d.AuthURL
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = d.Timeout
	}
	tokenRefreshSkew := cfg.TokenRefreshSkew
	if tokenRefreshSkew == 0 {
		tokenRefreshSkew = d.TokenRefreshSkew
	}
	maxBody := cfg.MaxResponseBodyBytes
	if maxBody == 0 {
		maxBody = config.DefaultMaxResponseBodyBytes
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = httputil.NewExternalAPIClient(timeout)
	}

	c := &Client{
		httpClient:           httpClient,
		clientID:             cfg.ClientID,
		clientSecret:         cfg.ClientSecret,
		baseURL:              baseURL,
		authURL:              authURL,
		tokenRefreshSkew:     tokenRefreshSkew,
		maxResponseBodyBytes: maxBody,
		logger:               logger,
		breaker: util.NewBreaker(
			constants.CircuitBreakerConfig.FailureThreshold,
			constants.CircuitBreakerConfig.ResetTimeout,
		),
	}
	c.tokenExpiry.Store(time.Time{})

	return c
}

func (c *Client) IsConfigured() bool {
	return c != nil && c.clientID != "" && c.clientSecret != ""
}

func (c *Client) ensureValidToken(ctx context.Context) error {
	expiry, _ := c.tokenExpiry.Load().(time.Time)
	if time.Now().Before(expiry.Add(-c.tokenRefreshSkew)) {
		return nil
	}

	if err := c.refreshToken(ctx); err != nil {
		return fmt.Errorf("refresh token: %w", err)
	}

	return nil
}

func (c *Client) refreshToken(ctx context.Context) error {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	// Double-check: 다른 고루틴이 이미 갱신했을 수 있음
	expiry, _ := c.tokenExpiry.Load().(time.Time)
	if time.Now().Before(expiry.Add(-c.tokenRefreshSkew)) {
		return nil
	}

	params := url.Values{}
	params.Set("client_id", c.clientID)
	params.Set("client_secret", c.clientSecret)
	params.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.authURL, strings.NewReader(params.Encode()))
	if err != nil {
		return fmt.Errorf("create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do token request: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token request failed: status %d", resp.StatusCode)
	}

	body, err := jsonutil.ReadAllLimit(resp.Body, c.maxResponseBodyBytes)
	if err != nil {
		return fmt.Errorf("read token response: %w", err)
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("unmarshal token response: %w", err)
	}

	accessToken := strings.TrimSpace(tokenResp.AccessToken)
	if accessToken == "" {
		return fmt.Errorf("twitch token response missing access_token")
	}

	if tokenResp.ExpiresIn <= 0 {
		return fmt.Errorf("twitch token response expires_in must be positive: %d", tokenResp.ExpiresIn)
	}

	c.token.Store(accessToken)
	c.tokenExpiry.Store(time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second))

	c.logger.Info("Twitch token refreshed",
		slog.Int("expires_in_seconds", tokenResp.ExpiresIn))

	return nil
}

func (c *Client) invalidateToken() {
	c.tokenExpiry.Store(time.Time{})
}

func (c *Client) isCircuitOpen() bool {
	return !c.breaker.Allow()
}

func (c *Client) recordFailure() {
	if opened := c.breaker.RecordFailure(); opened {
		c.logger.Warn("Twitch circuit breaker opened",
			slog.Int("failure_count", int(c.breaker.Failures())),
		)
	}
}

func (c *Client) recordSuccess() {
	c.breaker.RecordSuccess()
}

// IsCircuitOpen은 circuit open 여부를 반환합니다. Allow() 기반이므로
// resetTimeout 경과 시 reset side-effect가 발생합니다(원본 동작 보존).
func (c *Client) IsCircuitOpen() bool {
	return !c.breaker.Allow()
}
