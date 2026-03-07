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

package membernewsclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	commoncontracts "github.com/kapu/hololive-shared/pkg/contracts/common"
	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/contracts/subscription"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func New(baseURL, apiKey string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return &Client{
		baseURL:    baseURL,
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: httputil.NewClient(60 * time.Second),
	}
}

type digestRequest struct {
	RoomID string `json:"room_id"`
	Period string `json:"period"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (c *Client) GenerateRoomDigest(ctx context.Context, roomID string, period membernewscontracts.Period) (*membernewscontracts.Digest, error) {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return nil, fmt.Errorf("room id is required")
	}

	body, err := json.Marshal(digestRequest{RoomID: roomID, Period: string(membernewscontracts.NormalizePeriod(period))})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+membernewscontracts.DigestPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set(commoncontracts.APIKeyHeader, c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		var parsed errorResponse
		_ = json.NewDecoder(resp.Body).Decode(&parsed)
		if strings.EqualFold(strings.TrimSpace(parsed.Error), "no_subscribed_members") {
			return nil, membernewscontracts.ErrNoSubscribedMembers
		}
	}

	if err := httputil.CheckStatus(resp); err != nil {
		return nil, fmt.Errorf("check status: %w", err)
	}

	var digest membernewscontracts.Digest
	if err := httputil.DecodeJSON(resp, &digest); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &digest, nil
}

func (c *Client) SubscribeRoom(ctx context.Context, roomID, roomName string) error {
	return c.postSubscription(ctx, subscription.SubscribeRequest{RoomID: roomID, RoomName: roomName})
}

func (c *Client) UnsubscribeRoom(ctx context.Context, roomID string) error {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return fmt.Errorf("room id is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+membernewscontracts.SubscriptionsPath+"/"+roomID, http.NoBody)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set(commoncontracts.APIKeyHeader, c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := httputil.CheckStatus(resp); err != nil {
		return fmt.Errorf("check status: %w", err)
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *Client) IsRoomSubscribed(ctx context.Context, roomID string) (bool, error) {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return false, fmt.Errorf("room id is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+membernewscontracts.SubscriptionsPath+"/"+roomID, http.NoBody)
	if err != nil {
		return false, fmt.Errorf("new request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set(commoncontracts.APIKeyHeader, c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := httputil.CheckStatus(resp); err != nil {
		return false, fmt.Errorf("check status: %w", err)
	}

	var parsed subscription.SubscriptionStatusResponse
	if err := httputil.DecodeJSON(resp, &parsed); err != nil {
		return false, fmt.Errorf("decode response: %w", err)
	}
	return parsed.Subscribed, nil
}

func (c *Client) postSubscription(ctx context.Context, payload subscription.SubscribeRequest) error {
	payload.RoomID = strings.TrimSpace(payload.RoomID)
	payload.RoomName = strings.TrimSpace(payload.RoomName)
	if payload.RoomID == "" {
		return fmt.Errorf("room id is required")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+membernewscontracts.SubscriptionsPath, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set(commoncontracts.APIKeyHeader, c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := httputil.CheckStatus(resp); err != nil {
		return fmt.Errorf("check status: %w", err)
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// Ensure interface usage doesn't rely on direct error comparisons.
func IsNoSubscribedMembers(err error) bool {
	return errors.Is(err, membernewscontracts.ErrNoSubscribedMembers)
}
