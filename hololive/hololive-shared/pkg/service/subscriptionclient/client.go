package subscriptionclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/kapu/hololive-shared/pkg/contracts/subscription"
	"github.com/park285/shared-go/pkg/httputil"
)

type Client struct {
	HTTPClient        *httputil.JSONClient
	SubscriptionsPath string
}

func (c *Client) IsSubscribed(ctx context.Context, roomID string) (bool, error) {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return false, errors.New("room id is required")
	}

	req, err := c.HTTPClient.NewRequest(ctx, http.MethodGet, c.SubscriptionsPath+"/"+roomID)
	if err != nil {
		return false, fmt.Errorf("new request: %w", err)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("request: %w", err)
	}
	if resp == nil {
		return false, fmt.Errorf("request: nil response")
	}

	if err := c.HTTPClient.CheckStatus(resp); err != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			return false, errors.Join(fmt.Errorf("check status: %w", err), fmt.Errorf("close response body: %w", closeErr))
		}
		return false, fmt.Errorf("check status: %w", err)
	}

	var parsed subscription.SubscriptionStatusResponse
	if err := c.HTTPClient.DecodeJSON(resp, &parsed); err != nil {
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

	req, err := c.HTTPClient.NewJSONRequest(ctx, http.MethodPost, c.SubscriptionsPath, subscription.SubscribeRequest{
		RoomID:   roomID,
		RoomName: roomName,
	})
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	if resp == nil {
		return fmt.Errorf("request: nil response")
	}

	if err := c.HTTPClient.CheckStatus(resp); err != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			return errors.Join(fmt.Errorf("check status: %w", err), fmt.Errorf("close response body: %w", closeErr))
		}
		return fmt.Errorf("check status: %w", err)
	}

	if err := c.HTTPClient.DiscardBody(resp); err != nil {
		return fmt.Errorf("discard body: %w", err)
	}

	return nil
}

func (c *Client) Unsubscribe(ctx context.Context, roomID string) error {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return errors.New("room id is required")
	}

	req, err := c.HTTPClient.NewRequest(ctx, http.MethodDelete, c.SubscriptionsPath+"/"+roomID)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	if resp == nil {
		return fmt.Errorf("request: nil response")
	}

	if err := c.HTTPClient.CheckStatus(resp); err != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			return errors.Join(fmt.Errorf("check status: %w", err), fmt.Errorf("close response body: %w", closeErr))
		}
		return fmt.Errorf("check status: %w", err)
	}

	if err := c.HTTPClient.DiscardBody(resp); err != nil {
		return fmt.Errorf("discard body: %w", err)
	}

	return nil
}
