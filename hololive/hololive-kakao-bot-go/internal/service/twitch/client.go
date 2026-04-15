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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/jsonutil"

	apperrors "github.com/kapu/hololive-kakao-bot-go/internal/errors"
)

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

func NewClient(cfg ClientConfig, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = httputil.NewExternalAPIClient(constants.TwitchConfig.Timeout)
	}

	c := &Client{
		httpClient:   httpClient,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		logger:       logger,
	}
	c.tokenExpiry.Store(time.Time{})
	c.circuitOpenedAt.Store(time.Time{})

	return c
}

func (c *Client) IsConfigured() bool {
	return c != nil && c.clientID != "" && c.clientSecret != ""
}

func (c *Client) GetStreams(ctx context.Context, userLogins []string) (*StreamsResponse, error) {
	return c.getStreams(ctx, userLogins, true)
}

func (c *Client) getStreams(ctx context.Context, userLogins []string, allowRefreshRetry bool) (*StreamsResponse, error) {
	if !c.IsConfigured() {
		return nil, errors.New("twitch client not configured")
	}

	if len(userLogins) == 0 {
		return &StreamsResponse{Data: []StreamData{}}, nil
	}

	if c.isCircuitOpen() {
		return nil, &apperrors.APIError{
			Operation:  "twitch_get_streams",
			StatusCode: 503,
			Err:        errors.New("circuit breaker open"),
		}
	}

	if err := c.ensureValidToken(ctx); err != nil {
		return nil, fmt.Errorf("ensure token: %w", err)
	}

	params := url.Values{}
	for _, login := range userLogins {
		params.Add("user_login", login)
	}

	reqURL := constants.TwitchConfig.BaseURL + "/streams?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	token, _ := c.token.Load().(string)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Client-Id", c.clientID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.recordFailure()
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		c.recordFailure()
		c.invalidateToken()
		if !allowRefreshRetry {
			return nil, &apperrors.APIError{
				Operation:  "twitch_get_streams",
				StatusCode: http.StatusUnauthorized,
				Err:        errors.New("unauthorized after token refresh"),
			}
		}
		if refreshErr := c.refreshToken(ctx); refreshErr != nil {
			return nil, fmt.Errorf("refresh token after 401: %w", refreshErr)
		}
		return c.getStreams(ctx, userLogins, false)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		c.recordFailure()
		return nil, &apperrors.APIError{
			Operation:  "twitch_get_streams",
			StatusCode: 429,
			Err:        errors.New("rate limited"),
		}
	}

	if resp.StatusCode >= 500 {
		c.recordFailure()
		return nil, &apperrors.APIError{
			Operation:  "twitch_get_streams",
			StatusCode: resp.StatusCode,
			Err:        errors.New("server error"),
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &apperrors.APIError{
			Operation:  "twitch_get_streams",
			StatusCode: resp.StatusCode,
			Err:        errors.New("unexpected status"),
		}
	}

	body, err := jsonutil.ReadAllLimit(resp.Body, constants.APIConfig.MaxResponseBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var result StreamsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	c.recordSuccess()
	return &result, nil
}

func (c *Client) ensureValidToken(ctx context.Context) error {
	expiry, _ := c.tokenExpiry.Load().(time.Time)
	if time.Now().Before(expiry.Add(-constants.TwitchConfig.TokenRefreshSkew)) {
		return nil
	}

	return c.refreshToken(ctx)
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

	c.token.Store(tokenResp.AccessToken)
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
