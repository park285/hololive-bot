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

	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
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

type subscriptionStatusResponse struct {
	Subscribed bool `json:"subscribed"`
}

type subscribeRequest struct {
	RoomID   string `json:"room_id"`
	RoomName string `json:"room_name"`
}

type digestRequest struct {
	RoomID string `json:"room_id"`
	Period string `json:"period"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (c *Client) GenerateRoomDigest(ctx context.Context, roomID string, period membernewscontracts.Period) (*membernewscontracts.Digest, error) {
	if c == nil {
		return nil, fmt.Errorf("membernews client is nil")
	}
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
		req.Header.Set(sharedserver.APIKeyHeader, c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		var parsed errorResponse
		if err := httputil.DecodeJSON(resp, &parsed); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("decode not found response: %w", err)
		}
		if strings.EqualFold(strings.TrimSpace(parsed.Error), "no_subscribed_members") {
			return nil, membernewscontracts.ErrNoSubscribedMembers
		}
		if msg := strings.TrimSpace(parsed.Error); msg != "" {
			return nil, fmt.Errorf("status %d: %s", resp.StatusCode, msg)
		}
		return nil, fmt.Errorf("status %d", resp.StatusCode)
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
	return c.postSubscription(ctx, subscribeRequest{RoomID: roomID, RoomName: roomName})
}

func (c *Client) UnsubscribeRoom(ctx context.Context, roomID string) error {
	if c == nil {
		return fmt.Errorf("membernews client is nil")
	}
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return fmt.Errorf("room id is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+membernewscontracts.SubscriptionsPath+"/"+roomID, http.NoBody)
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

	if err := httputil.CheckStatus(resp); err != nil {
		return fmt.Errorf("check status: %w", err)
	}

	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return fmt.Errorf("drain response body: %w", err)
	}
	return nil
}

func (c *Client) IsRoomSubscribed(ctx context.Context, roomID string) (bool, error) {
	if c == nil {
		return false, fmt.Errorf("membernews client is nil")
	}
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return false, fmt.Errorf("room id is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+membernewscontracts.SubscriptionsPath+"/"+roomID, http.NoBody)
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

	if err := httputil.CheckStatus(resp); err != nil {
		return false, fmt.Errorf("check status: %w", err)
	}

	var parsed subscriptionStatusResponse
	if err := httputil.DecodeJSON(resp, &parsed); err != nil {
		return false, fmt.Errorf("decode response: %w", err)
	}
	return parsed.Subscribed, nil
}

func (c *Client) postSubscription(ctx context.Context, payload subscribeRequest) error {
	if c == nil {
		return fmt.Errorf("membernews client is nil")
	}
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
		req.Header.Set(sharedserver.APIKeyHeader, c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := httputil.CheckStatus(resp); err != nil {
		return fmt.Errorf("check status: %w", err)
	}

	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return fmt.Errorf("drain response body: %w", err)
	}
	return nil
}

// Ensure interface usage doesn't rely on direct error comparisons.
func IsNoSubscribedMembers(err error) bool {
	return errors.Is(err, membernewscontracts.ErrNoSubscribedMembers)
}
