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
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/kapu/hololive-shared/pkg/constants"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"golang.org/x/sync/errgroup"
)

func (c *Client) GetLives(ctx context.Context, size int, next string) (*LivesResponse, error) {
	if !c.HasOpenAPICredentials() {
		return nil, errors.New("chzzk open API credentials not configured")
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

	var apiResp OpenAPIResponse[LivesResponse]
	if err := c.executeRequest(chzzkGetLivesOp, req, "read response body", func(body []byte) error {
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}

		if apiResp.Code != http.StatusOK {
			return fmt.Errorf("API error: code=%d, message=%s", apiResp.Code, apiResp.Message)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return &apiResp.Content, nil
}

func (c *Client) GetChannels(ctx context.Context, channelIDs []string) (*ChannelsResponse, error) {
	if !c.HasOpenAPICredentials() {
		return nil, errors.New("chzzk open API credentials not configured")
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

	var apiResp OpenAPIResponse[ChannelsResponse]
	if err := c.executeRequest(chzzkGetChannelsOp, req, "read response body", func(body []byte) error {
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}

		if apiResp.Code != http.StatusOK {
			return fmt.Errorf("API error: code=%d, message=%s", apiResp.Code, apiResp.Message)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return &apiResp.Content, nil
}

func (c *Client) GetLivesByChannelIDs(ctx context.Context, channelIDs []string) ([]LiveData, error) {
	if !c.HasOpenAPICredentials() {
		return nil, errors.New("chzzk open API credentials not configured")
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

	slices.Sort(targets)

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

	found := make(map[string]LiveData, len(targets))
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

			found[resp.Data[i].ChannelID] = resp.Data[i]
		}

		if len(found) == len(targets) || resp.Page.Next == "" {
			break
		}

		next = resp.Page.Next
	}

	result := make([]LiveData, 0, len(found))
	for _, channelID := range channelIDs {
		live, ok := found[channelID]
		if !ok {
			continue
		}

		result = append(result, live)
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
