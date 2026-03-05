package majoreventclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	commoncontracts "github.com/kapu/hololive-shared/pkg/contracts/common"
	majoreventcontracts "github.com/kapu/hololive-shared/pkg/contracts/majorevent"
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
		httpClient: httputil.DefaultClient(),
	}
}

func (c *Client) IsSubscribed(ctx context.Context, roomID string) (bool, error) {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return false, fmt.Errorf("room id is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+majoreventcontracts.SubscriptionsPath+"/"+roomID, http.NoBody)
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

func (c *Client) Subscribe(ctx context.Context, roomID, roomName string) error {
	roomID = strings.TrimSpace(roomID)
	roomName = strings.TrimSpace(roomName)
	if roomID == "" {
		return fmt.Errorf("room id is required")
	}

	body, err := json.Marshal(subscription.SubscribeRequest{RoomID: roomID, RoomName: roomName})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+majoreventcontracts.SubscriptionsPath, bytes.NewReader(body))
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

func (c *Client) Unsubscribe(ctx context.Context, roomID string) error {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return fmt.Errorf("room id is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+majoreventcontracts.SubscriptionsPath+"/"+roomID, http.NoBody)
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
