package botrooms

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	irisroomscontracts "github.com/kapu/hololive-shared/pkg/contracts/irisrooms"
	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
	"github.com/park285/iris-client-go/iris"
	"github.com/park285/shared-go/pkg/httputil"
)

type Client struct {
	httpClient *httputil.JSONClient
}

func NewClient(baseURL, apiKey string, logger *slog.Logger) *Client {
	return &Client{httpClient: internalhttp.NewJSONClient(baseURL, apiKey, 30*time.Second, logger)}
}

func (c *Client) GetRooms(ctx context.Context) (*iris.RoomListResponse, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}

	req, err := c.httpClient.NewRequest(ctx, http.MethodGet, irisroomscontracts.ListPath)
	if err != nil {
		return nil, fmt.Errorf("list bot iris rooms: new request: %w", err)
	}

	resp, err := c.httpClient.Do(req) //nolint:bodyclose // CheckStatus/DecodeJSON이 성공·실패 경로에서 body를 닫는다.
	if err != nil {
		return nil, fmt.Errorf("list bot iris rooms: request: %w", err)
	}
	if resp == nil {
		return nil, errors.New("list bot iris rooms: response is nil")
	}
	if resp.Body == nil {
		return nil, errors.New("list bot iris rooms: response body is nil")
	}
	if err := c.httpClient.CheckStatus(resp); err != nil {
		return nil, fmt.Errorf("list bot iris rooms: request failed: %w", err)
	}

	var out iris.RoomListResponse
	if err := c.httpClient.DecodeJSON(resp, &out); err != nil {
		return nil, fmt.Errorf("list bot iris rooms: decode response: %w", err)
	}

	return &out, nil
}

func (c *Client) validate() error {
	if c == nil {
		return errors.New("list bot iris rooms: client is nil")
	}
	if c.httpClient == nil {
		return errors.New("list bot iris rooms: http client is not configured")
	}
	return nil
}
