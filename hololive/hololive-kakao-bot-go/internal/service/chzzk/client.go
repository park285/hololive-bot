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

package chzzk

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/jsonutil"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-kakao-bot-go/internal/errors"
)

const (
	DefaultBaseURL = "https://api.chzzk.naver.com"
	OpenAPIBaseURL = "https://openapi.chzzk.naver.com"

	chzzkGetLiveStatusOp     = "chzzk_get_live_status"
	chzzkGetScheduledLivesOp = "chzzk_get_scheduled_lives"
	chzzkGetLivesOp          = "chzzk_get_lives"
	chzzkGetChannelsOp       = "chzzk_get_channels"
)

type Client struct {
	httpClient       *http.Client
	baseURL          string
	openAPIBaseURL   string
	clientID         string
	clientSecret     string
	logger           *slog.Logger
	circuitOpenUntil *time.Time
	circuitMu        sync.RWMutex
	failureCount     int
}

type ClientConfig struct {
	HTTPClient   *http.Client
	BaseURL      string
	ClientID     string
	ClientSecret string
	Logger       *slog.Logger
}

func NewClient(httpClient *http.Client, baseURL string, logger *slog.Logger) *Client {
	return &Client{
		httpClient:     httpClient,
		baseURL:        baseURL,
		openAPIBaseURL: OpenAPIBaseURL,
		logger:         logger,
	}
}

func NewClientWithConfig(cfg ClientConfig) *Client {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		httpClient:     cfg.HTTPClient,
		baseURL:        baseURL,
		openAPIBaseURL: OpenAPIBaseURL,
		clientID:       cfg.ClientID,
		clientSecret:   cfg.ClientSecret,
		logger:         cfg.Logger,
	}
}

func (c *Client) HasOpenAPICredentials() bool {
	return c.clientID != "" && c.clientSecret != ""
}

func (c *Client) GetLiveStatus(ctx context.Context, channelID string) (*LiveStatusContent, error) {
	if err := c.rejectIfCircuitOpen(); err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/polling/v2/channels/%s/live-status", channelID)
	reqURL := c.baseURL + path

	req, err := c.newRequest(ctx, "GET", reqURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.handleRequestFailure()
		return nil, &errors.APIError{
			Operation:  chzzkGetLiveStatusOp,
			StatusCode: 0,
			Err:        err,
		}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		c.handleStatusCodeError(resp.StatusCode)
		return nil, &errors.APIError{
			Operation:  chzzkGetLiveStatusOp,
			StatusCode: resp.StatusCode,
		}
	}

	body, err := jsonutil.ReadAllLimit(resp.Body, constants.APIConfig.MaxResponseBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var liveStatusResp LiveStatusResponse
	if err := json.Unmarshal(body, &liveStatusResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	c.resetCircuit()
	return liveStatusResp.Content, nil
}

func (c *Client) GetScheduledLives(ctx context.Context, channelID string) ([]ScheduledLive, error) {
	if err := c.rejectIfCircuitOpen(); err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/service/v1/channels/%s/scheduled-lives", channelID)
	reqURL := c.baseURL + path

	req, err := c.newRequest(ctx, "GET", reqURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.handleRequestFailure()
		return nil, &errors.APIError{
			Operation:  chzzkGetScheduledLivesOp,
			StatusCode: 0,
			Err:        err,
		}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		c.handleStatusCodeError(resp.StatusCode)
		return nil, &errors.APIError{
			Operation:  chzzkGetScheduledLivesOp,
			StatusCode: resp.StatusCode,
		}
	}

	body, err := jsonutil.ReadAllLimit(resp.Body, constants.APIConfig.MaxResponseBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var scheduledResp ScheduledLivesResponse
	if err := json.Unmarshal(body, &scheduledResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	c.resetCircuit()

	if scheduledResp.Content == nil {
		return []ScheduledLive{}, nil
	}

	return scheduledResp.Content.ScheduledLives, nil
}

func (c *Client) IsCircuitOpen() bool {
	c.circuitMu.RLock()
	defer c.circuitMu.RUnlock()

	if c.circuitOpenUntil == nil {
		return false
	}

	if time.Now().After(*c.circuitOpenUntil) {
		return false
	}

	return true
}

func (c *Client) newRequest(ctx context.Context, method, targetURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, targetURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "api.capu.blog/hololive-bot (Chzzk API client)")

	return req, nil
}

func (c *Client) rejectIfCircuitOpen() error {
	if !c.IsCircuitOpen() {
		return nil
	}

	c.circuitMu.RLock()
	var remainingMs int64
	if c.circuitOpenUntil != nil {
		remainingMs = time.Until(*c.circuitOpenUntil).Milliseconds()
	}
	c.circuitMu.RUnlock()

	c.logger.Warn("Circuit breaker is open", slog.Int64("retry_after_ms", remainingMs))
	return errors.CircuitOpenError{RetryAfterMs: remainingMs}
}

func (c *Client) handleRequestFailure() {
	count := c.incrementFailureCount()
	if count >= constants.CircuitBreakerConfig.FailureThreshold {
		c.openCircuit()
	}
}

func (c *Client) handleStatusCodeError(statusCode int) {
	if statusCode >= 500 || statusCode == http.StatusTooManyRequests {
		count := c.incrementFailureCount()
		c.logger.Warn("Server error or rate limit",
			slog.Int("status", statusCode),
			slog.Int("failure_count", count),
		)

		if count >= constants.CircuitBreakerConfig.FailureThreshold {
			c.openCircuit()
		}
	}
}

func (c *Client) openCircuit() {
	c.circuitMu.Lock()
	defer c.circuitMu.Unlock()

	resetTime := time.Now().Add(constants.CircuitBreakerConfig.ResetTimeout)
	c.circuitOpenUntil = &resetTime
	c.failureCount = 0

	c.logger.Error("Chzzk circuit breaker opened",
		slog.Duration("reset_timeout", constants.CircuitBreakerConfig.ResetTimeout),
	)
}

func (c *Client) resetCircuit() {
	c.circuitMu.Lock()
	defer c.circuitMu.Unlock()

	c.failureCount = 0
	c.circuitOpenUntil = nil
}

func (c *Client) incrementFailureCount() int {
	c.circuitMu.Lock()
	defer c.circuitMu.Unlock()

	c.failureCount++
	return c.failureCount
}

func (c *Client) GetLives(ctx context.Context, size int, next string) (*LivesResponse, error) {
	if !c.HasOpenAPICredentials() {
		return nil, fmt.Errorf("chzzk open API credentials not configured")
	}

	if err := c.rejectIfCircuitOpen(); err != nil {
		return nil, err
	}

	params := url.Values{}
	if size > 0 && size <= 20 {
		params.Set("size", fmt.Sprintf("%d", size))
	}
	if next != "" {
		params.Set("next", next)
	}

	reqURL := c.openAPIBaseURL + "/open/v1/lives"
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}

	req, err := c.newOpenAPIRequest(ctx, "GET", reqURL)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.handleRequestFailure()
		return nil, &errors.APIError{
			Operation:  chzzkGetLivesOp,
			StatusCode: 0,
			Err:        err,
		}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		c.handleStatusCodeError(resp.StatusCode)
		return nil, &errors.APIError{
			Operation:  chzzkGetLivesOp,
			StatusCode: resp.StatusCode,
		}
	}

	body, err := jsonutil.ReadAllLimit(resp.Body, constants.APIConfig.MaxResponseBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	var apiResp OpenAPIResponse[LivesResponse]
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if apiResp.Code != http.StatusOK {
		return nil, fmt.Errorf("API error: code=%d, message=%s", apiResp.Code, apiResp.Message)
	}

	c.resetCircuit()
	return &apiResp.Content, nil
}

func (c *Client) GetChannels(ctx context.Context, channelIDs []string) (*ChannelsResponse, error) {
	if !c.HasOpenAPICredentials() {
		return nil, fmt.Errorf("chzzk open API credentials not configured")
	}

	if len(channelIDs) == 0 {
		return &ChannelsResponse{Data: []ChannelData{}}, nil
	}

	if len(channelIDs) > 20 {
		return nil, fmt.Errorf("maximum 20 channel IDs allowed, got %d", len(channelIDs))
	}

	if err := c.rejectIfCircuitOpen(); err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("channelIds", strings.Join(channelIDs, ","))

	reqURL := c.openAPIBaseURL + "/open/v1/channels?" + params.Encode()

	req, err := c.newOpenAPIRequest(ctx, "GET", reqURL)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.handleRequestFailure()
		return nil, &errors.APIError{
			Operation:  chzzkGetChannelsOp,
			StatusCode: 0,
			Err:        err,
		}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		c.handleStatusCodeError(resp.StatusCode)
		return nil, &errors.APIError{
			Operation:  chzzkGetChannelsOp,
			StatusCode: resp.StatusCode,
		}
	}

	body, err := jsonutil.ReadAllLimit(resp.Body, constants.APIConfig.MaxResponseBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	var apiResp OpenAPIResponse[ChannelsResponse]
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if apiResp.Code != http.StatusOK {
		return nil, fmt.Errorf("API error: code=%d, message=%s", apiResp.Code, apiResp.Message)
	}

	c.resetCircuit()
	return &apiResp.Content, nil
}

func (c *Client) GetLivesByChannelIDs(ctx context.Context, channelIDs []string) ([]LiveData, error) {
	if !c.HasOpenAPICredentials() {
		return nil, fmt.Errorf("chzzk open API credentials not configured")
	}

	targets := normalizeChannelTargets(channelIDs)
	if len(targets) == 0 {
		return []LiveData{}, nil
	}

	if len(targets) <= constants.ChzzkConfig.BatchLookupThreshold {
		return c.getLivesByStatusChecks(ctx, targets)
	}

	return c.getLivesByPageScan(ctx, targets)
}

func normalizeChannelTargets(channelIDs []string) []string {
	targetSet := make(map[string]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		channelID = strings.TrimSpace(channelID)
		if channelID == "" {
			continue
		}
		targetSet[channelID] = struct{}{}
	}

	targets := make([]string, 0, len(targetSet))
	for channelID := range targetSet {
		targets = append(targets, channelID)
	}

	return targets
}

func (c *Client) getLivesByStatusChecks(ctx context.Context, channelIDs []string) ([]LiveData, error) {
	var (
		mu      sync.Mutex
		g       errgroup.Group
		liveMap = make(map[string]LiveData, len(channelIDs))
	)
	g.SetLimit(constants.ChzzkConfig.MaxConcurrentStatusChecks)

	for _, channelID := range channelIDs {
		channelID := channelID
		g.Go(func() error {
			status, err := c.GetLiveStatus(ctx, channelID)
			if err != nil {
				return fmt.Errorf("get live status for %s: %w", channelID, err)
			}
			if status == nil || !strings.EqualFold(status.Status, "OPEN") {
				return nil
			}

			mu.Lock()
			liveMap[channelID] = LiveData{
				ChannelID:           channelID,
				LiveTitle:           status.LiveTitle,
				ConcurrentUserCount: status.ConcurrentUserCount,
				LiveCategoryValue:   status.LiveCategoryValue,
			}
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("get lives by status checks: %w", err)
	}

	result := make([]LiveData, 0, len(liveMap))
	for _, channelID := range channelIDs {
		live, ok := liveMap[channelID]
		if !ok {
			continue
		}
		result = append(result, live)
	}

	return result, nil
}

func (c *Client) getLivesByPageScan(ctx context.Context, channelIDs []string) ([]LiveData, error) {
	targets := make(map[string]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		targets[channelID] = struct{}{}
	}

	result := make([]LiveData, 0, len(targets))
	found := make(map[string]struct{}, len(targets))
	next := ""

	for {
		resp, err := c.GetLives(ctx, constants.ChzzkConfig.MaxLivesPageSize, next)
		if err != nil {
			return nil, fmt.Errorf("get lives page: %w", err)
		}

		for i := range resp.Data {
			if _, ok := targets[resp.Data[i].ChannelID]; !ok {
				continue
			}
			if _, exists := found[resp.Data[i].ChannelID]; exists {
				continue
			}
			found[resp.Data[i].ChannelID] = struct{}{}
			result = append(result, resp.Data[i])
		}

		if len(found) == len(targets) || resp.Page.Next == "" {
			break
		}
		next = resp.Page.Next
	}

	return result, nil
}

func (c *Client) newOpenAPIRequest(ctx context.Context, method, reqURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Client-Id", c.clientID)
	req.Header.Set("Client-Secret", c.clientSecret)

	return req, nil
}
