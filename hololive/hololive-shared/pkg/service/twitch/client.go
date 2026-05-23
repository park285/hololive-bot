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

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/park285/hololive-bot/shared-go/pkg/httputil"
	"github.com/park285/hololive-bot/shared-go/pkg/json"
	"github.com/park285/hololive-bot/shared-go/pkg/jsonutil"
)

const maxUserLoginsPerRequest = 100

type ClientConfig struct {
	HTTPClient   *http.Client
	ClientID     string
	ClientSecret string
}

type Client struct {
	httpClient   *http.Client
	clientID     string
	clientSecret string
	logger       *slog.Logger

	token       atomic.Value // string
	tokenExpiry atomic.Value // time.Time
	tokenMu     sync.Mutex

	circuitOpen     atomic.Bool
	circuitOpenedAt atomic.Value // time.Time
	failureCount    atomic.Int32
}

func NewClient(config ClientConfig, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = httputil.NewExternalAPIClient(constants.TwitchConfig.Timeout)
	}

	c := &Client{
		httpClient:   httpClient,
		clientID:     config.ClientID,
		clientSecret: config.ClientSecret,
		logger:       logger,
	}
	c.tokenExpiry.Store(time.Time{})
	c.circuitOpenedAt.Store(time.Time{})

	return c
}

func (c *Client) IsConfigured() bool {
	return c != nil && c.clientID != "" && c.clientSecret != ""
}

func (c *Client) ensureValidToken(ctx context.Context) error {
	expiry, _ := c.tokenExpiry.Load().(time.Time)
	if time.Now().Before(expiry.Add(-constants.TwitchConfig.TokenRefreshSkew)) {
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
	if time.Now().Before(expiry.Add(-constants.TwitchConfig.TokenRefreshSkew)) {
		return nil
	}

	params := url.Values{}
	params.Set("client_id", c.clientID)
	params.Set("client_secret", c.clientSecret)
	params.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, constants.TwitchConfig.AuthURL, strings.NewReader(params.Encode()))
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

	body, err := jsonutil.ReadAllLimit(resp.Body, constants.APIConfig.MaxResponseBodyBytes)
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
	if !c.circuitOpen.Load() {
		return false
	}

	openedAt, _ := c.circuitOpenedAt.Load().(time.Time)
	if time.Since(openedAt) > constants.CircuitBreakerConfig.ResetTimeout {
		c.circuitOpen.Store(false)
		c.failureCount.Store(0)
		c.logger.Info("Twitch circuit breaker reset")

		return false
	}

	return true
}

func (c *Client) recordFailure() {
	count := c.failureCount.Add(1)
	if count >= int32(constants.CircuitBreakerConfig.FailureThreshold) {
		c.circuitOpen.Store(true)
		c.circuitOpenedAt.Store(time.Now())
		c.logger.Warn("Twitch circuit breaker opened",
			slog.Int("failure_count", int(count)))
	}
}

func (c *Client) recordSuccess() {
	c.failureCount.Store(0)
}

func (c *Client) IsCircuitOpen() bool {
	return c.isCircuitOpen()
}
