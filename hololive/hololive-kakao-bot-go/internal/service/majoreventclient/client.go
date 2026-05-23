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

package majoreventclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	majoreventcontracts "github.com/kapu/hololive-shared/pkg/contracts/majorevent"
	"github.com/kapu/hololive-shared/pkg/contracts/subscription"
	"github.com/park285/hololive-bot/shared-go/pkg/httputil"
)

type Client struct {
	httpClient *httputil.JSONClient
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		httpClient: httputil.NewJSONClient(baseURL, apiKey, 30*time.Second),
	}
}

func (c *Client) IsSubscribed(ctx context.Context, roomID string) (bool, error) {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return false, errors.New("room id is required")
	}

	req, err := c.httpClient.NewRequest(ctx, http.MethodGet, majoreventcontracts.SubscriptionsPath+"/"+roomID)
	if err != nil {
		return false, fmt.Errorf("new request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("request: %w", err)
	}

	if err := c.httpClient.CheckStatus(resp); err != nil {
		defer func() { _ = resp.Body.Close() }()
		return false, fmt.Errorf("check status: %w", err)
	}

	var parsed subscription.SubscriptionStatusResponse
	if err := c.httpClient.DecodeJSON(resp, &parsed); err != nil {
		return false, fmt.Errorf("decode response: %w", err)
	}

	return parsed.Subscribed, nil
}

func (c *Client) Subscribe(ctx context.Context, roomID, roomName string) error {
	roomID = strings.TrimSpace(roomID)
	roomName = strings.TrimSpace(roomName)

	if roomID == "" {
		return errors.New("room id is required")
	}

	req, err := c.httpClient.NewJSONRequest(ctx, http.MethodPost, majoreventcontracts.SubscriptionsPath, subscription.SubscribeRequest{
		RoomID:   roomID,
		RoomName: roomName,
	})
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}

	if err := c.httpClient.CheckStatus(resp); err != nil {
		defer func() { _ = resp.Body.Close() }()
		return fmt.Errorf("check status: %w", err)
	}

	if err := c.httpClient.DiscardBody(resp); err != nil {
		return fmt.Errorf("discard body: %w", err)
	}

	return nil
}

func (c *Client) Unsubscribe(ctx context.Context, roomID string) error {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return errors.New("room id is required")
	}

	req, err := c.httpClient.NewRequest(ctx, http.MethodDelete, majoreventcontracts.SubscriptionsPath+"/"+roomID)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}

	if err := c.httpClient.CheckStatus(resp); err != nil {
		defer func() { _ = resp.Body.Close() }()
		return fmt.Errorf("check status: %w", err)
	}

	if err := c.httpClient.DiscardBody(resp); err != nil {
		return fmt.Errorf("discard body: %w", err)
	}

	return nil
}
