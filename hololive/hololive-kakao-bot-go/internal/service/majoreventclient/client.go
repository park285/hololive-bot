package majoreventclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	majoreventcontracts "github.com/kapu/hololive-shared/pkg/contracts/majorevent"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func New(baseURL, apiKey string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return &Client{
		baseURL: baseURL,
		apiKey:  strings.TrimSpace(apiKey),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type subscriptionStatusResponse struct {
	Subscribed bool `json:"subscribed"`
}

type subscribeRequest struct {
	RoomID   string `json:"room_id"`
	RoomName string `json:"room_name"`
}

func (c *Client) IsSubscribed(ctx context.Context, roomID string) (bool, error) {
	if c == nil {
		return false, fmt.Errorf("majorevent client is nil")
	}
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return false, fmt.Errorf("room id is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+majoreventcontracts.SubscriptionsPath+"/"+roomID, http.NoBody)
	if err != nil {
		return false, fmt.Errorf("new request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set(sharedserver.APIKeyHeader, c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		const maxBodyLen = 4096
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyLen))
		return false, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var parsed subscriptionStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return false, fmt.Errorf("decode response: %w", err)
	}
	return parsed.Subscribed, nil
}

func (c *Client) Subscribe(ctx context.Context, roomID, roomName string) error {
	if c == nil {
		return fmt.Errorf("majorevent client is nil")
	}
	roomID = strings.TrimSpace(roomID)
	roomName = strings.TrimSpace(roomName)
	if roomID == "" {
		return fmt.Errorf("room id is required")
	}

	body, err := json.Marshal(subscribeRequest{RoomID: roomID, RoomName: roomName})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+majoreventcontracts.SubscriptionsPath, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set(sharedserver.APIKeyHeader, c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		const maxBodyLen = 4096
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyLen))
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *Client) Unsubscribe(ctx context.Context, roomID string) error {
	if c == nil {
		return fmt.Errorf("majorevent client is nil")
	}
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return fmt.Errorf("room id is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+majoreventcontracts.SubscriptionsPath+"/"+roomID, http.NoBody)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set(sharedserver.APIKeyHeader, c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		const maxBodyLen = 4096
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyLen))
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
