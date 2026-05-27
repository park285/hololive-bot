package membernewsclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/service/subscriptionclient"
	"github.com/park285/shared-go/pkg/httputil"
	sharedjson "github.com/park285/shared-go/pkg/json"
)

type Client struct {
	subscriptionclient.Client
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		Client: subscriptionclient.Client{
			HTTPClient:        httputil.NewJSONClient(baseURL, apiKey, 60*time.Second),
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

func (c *Client) GenerateRoomDigest(ctx context.Context, roomID string, period membernewscontracts.Period) (*membernewscontracts.Digest, error) {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return nil, errors.New("room id is required")
	}

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

	defer func() { _ = resp.Body.Close() }()

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
