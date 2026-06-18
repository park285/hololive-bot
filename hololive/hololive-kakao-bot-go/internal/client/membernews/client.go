package membernews

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
	"github.com/kapu/hololive-shared/pkg/service/subscriptionclient"
	sharedjson "github.com/park285/shared-go/pkg/json"
)

type Client struct {
	subscriptionclient.Client
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		Client: subscriptionclient.Client{
			HTTPClient:        internalhttp.NewJSONClient(baseURL, apiKey, 60*time.Second, nil),
			SubscriptionsPath: membernewscontracts.SubscriptionsPath,
		},
	}
}

type digestRequest struct {
	RoomID string `json:"room_id"`
	Period string `json:"period"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (c *Client) GenerateRoomDigest(ctx context.Context, roomID string, period membernewscontracts.Period) (digest *membernewscontracts.Digest, err error) {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return nil, errors.New("room id is required")
	}

	resp, err := c.postRoomDigest(ctx, roomID, period)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := closeBody(resp.Body); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	return c.decodeRoomDigest(resp)
}

func (c *Client) postRoomDigest(ctx context.Context, roomID string, period membernewscontracts.Period) (*http.Response, error) {
	req, err := c.HTTPClient.NewJSONRequest(ctx, http.MethodPost, membernewscontracts.DigestPath, digestRequest{
		RoomID: roomID,
		Period: string(membernewscontracts.NormalizePeriod(period)),
	})
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	if resp == nil {
		return nil, errors.New("request: empty response")
	}
	if resp.Body == nil {
		return nil, errors.New("request: empty response body")
	}

	return resp, nil
}

func (c *Client) decodeRoomDigest(resp *http.Response) (*membernewscontracts.Digest, error) {
	if err := handleRoomDigestNotFound(resp); err != nil {
		return nil, err
	}

	if err := c.HTTPClient.CheckStatus(resp); err != nil {
		return nil, fmt.Errorf("check status: %w", err)
	}

	var digest membernewscontracts.Digest
	if err := c.HTTPClient.DecodeJSON(resp, &digest); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &digest, nil
}

func handleRoomDigestNotFound(resp *http.Response) error {
	if resp == nil {
		return errors.New("handle room digest not found: response is nil")
	}
	if resp.Body == nil {
		return errors.New("handle room digest not found: response body is nil")
	}
	if resp.StatusCode != http.StatusNotFound {
		return nil
	}

	var parsed errorResponse
	if decodeErr := sharedjson.NewDecoder(resp.Body).Decode(&parsed); decodeErr != nil {
		return fmt.Errorf("decode not found response: %w", decodeErr)
	}

	if strings.EqualFold(strings.TrimSpace(parsed.Error), "no_subscribed_members") {
		return membernewscontracts.ErrNoSubscribedMembers
	}

	return nil
}

func closeBody(body io.ReadCloser) error {
	if body == nil {
		return nil
	}
	if err := body.Close(); err != nil {
		return fmt.Errorf("close body: %w", err)
	}
	return nil
}

func (c *Client) SubscribeRoom(ctx context.Context, roomID, roomName string) error {
	return c.Subscribe(ctx, roomID, roomName)
}

func (c *Client) UnsubscribeRoom(ctx context.Context, roomID string) error {
	return c.Unsubscribe(ctx, roomID)
}

func (c *Client) IsRoomSubscribed(ctx context.Context, roomID string) (bool, error) {
	return c.IsSubscribed(ctx, roomID)
}

func IsNoSubscribedMembers(err error) bool {
	return errors.Is(err, membernewscontracts.ErrNoSubscribedMembers)
}
