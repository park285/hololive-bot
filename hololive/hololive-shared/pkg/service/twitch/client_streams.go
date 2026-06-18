package twitch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/park285/shared-go/pkg/json"
	"github.com/park285/shared-go/pkg/jsonutil"

	apperrors "github.com/kapu/hololive-shared/pkg/apperrors"
)

func (c *Client) GetStreams(ctx context.Context, userLogins []string) (*StreamsResponse, error) {
	targets := normalizeUserLogins(userLogins)
	if len(targets) == 0 {
		return &StreamsResponse{Data: []StreamData{}}, nil
	}

	if !c.IsConfigured() {
		return nil, errors.New("twitch client not configured")
	}

	if len(targets) <= maxUserLoginsPerRequest {
		streams, err := c.getStreams(ctx, targets, true)
		if err != nil {
			return nil, fmt.Errorf("get streams: %w", err)
		}
		return streams, nil
	}

	return c.getStreamChunks(ctx, targets)
}

func (c *Client) getStreamChunks(ctx context.Context, targets []string) (*StreamsResponse, error) {
	chunks := chunkUserLogins(targets, maxUserLoginsPerRequest)
	merged := &StreamsResponse{Data: make([]StreamData, 0, len(targets))}

	for i, chunk := range chunks {
		streams, err := c.getStreams(ctx, chunk, true)
		if err != nil {
			c.logger.Warn("Failed to fetch Twitch stream chunk",
				slog.Int("chunk_index", i+1),
				slog.Int("chunk_count", len(chunks)),
				slog.Int("chunk_size", len(chunk)),
				slog.Any("error", err),
			)

			return nil, fmt.Errorf("get stream chunk %d/%d: %w", i+1, len(chunks), err)
		}

		appendStreamData(merged, streams)
	}

	return merged, nil
}

func appendStreamData(merged, streams *StreamsResponse) {
	if streams != nil {
		merged.Data = append(merged.Data, streams.Data...)
	}
}

func normalizeUserLogins(userLogins []string) []string {
	seen := make(map[string]struct{}, len(userLogins))
	normalized := make([]string, 0, len(userLogins))

	for _, login := range userLogins {
		login = strings.ToLower(strings.TrimSpace(login))
		if login == "" {
			continue
		}

		if _, ok := seen[login]; ok {
			continue
		}

		seen[login] = struct{}{}
		normalized = append(normalized, login)
	}

	return normalized
}

func chunkUserLogins(userLogins []string, chunkSize int) [][]string {
	if len(userLogins) == 0 {
		return nil
	}

	chunks := make([][]string, 0, (len(userLogins)+chunkSize-1)/chunkSize)
	for start := 0; start < len(userLogins); start += chunkSize {
		end := min(start+chunkSize, len(userLogins))

		chunks = append(chunks, userLogins[start:end])
	}

	return chunks
}

func (c *Client) getStreams(
	ctx context.Context,
	userLogins []string,
	allowRefreshRetry bool,
) (*StreamsResponse, error) {
	if streams, done, err := c.prepareGetStreams(ctx, userLogins); done {
		return streams, err
	}

	req, err := c.newStreamsRequest(ctx, userLogins)
	if err != nil {
		return nil, fmt.Errorf("new streams request: %w", err)
	}

	resp, err := c.doStreamsRequest(req)
	if err != nil {
		return nil, fmt.Errorf("do streams request: %w", err)
	}

	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			c.logger.Warn("Failed to close Twitch streams response body", slog.Any("error", closeErr))
		}
	}()

	if resp.StatusCode == http.StatusUnauthorized {
		return c.retryUnauthorizedStreamsWithContext(ctx, userLogins, allowRefreshRetry)
	}

	return c.decodeStreamsHTTPResponse(resp)
}

func (c *Client) prepareGetStreams(ctx context.Context, userLogins []string) (*StreamsResponse, bool, error) {
	if !c.IsConfigured() {
		return nil, true, errors.New("twitch client not configured")
	}
	if len(userLogins) == 0 {
		return &StreamsResponse{Data: []StreamData{}}, true, nil
	}
	if c.isCircuitOpen() {
		return nil, true, &apperrors.APIError{
			Operation:  "twitch_get_streams",
			StatusCode: 503,
			Err:        errors.New("circuit breaker open"),
		}
	}
	if err := c.ensureValidToken(ctx); err != nil {
		return nil, true, fmt.Errorf("ensure token: %w", err)
	}
	return nil, false, nil
}

func (c *Client) retryUnauthorizedStreamsWithContext(
	ctx context.Context,
	userLogins []string,
	allowRefreshRetry bool,
) (*StreamsResponse, error) {
	retriedStreams, retryErr := c.retryUnauthorizedStreams(ctx, userLogins, allowRefreshRetry)
	if retryErr != nil {
		return nil, fmt.Errorf("retry unauthorized streams: %w", retryErr)
	}
	return retriedStreams, nil
}

func (c *Client) decodeStreamsHTTPResponse(resp *http.Response) (*StreamsResponse, error) {
	body, err := c.readStreamsResponseBody(resp)
	if err != nil {
		return nil, fmt.Errorf("read streams response body: %w", err)
	}

	result, err := c.decodeStreamsResponse(body)
	if err != nil {
		c.recordFailure()
		return nil, fmt.Errorf("decode streams response: %w", err)
	}

	c.recordSuccess()

	return result, nil
}

func (c *Client) doStreamsRequest(req *http.Request) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.recordFailure()
		if resp == nil {
			err = fmt.Errorf("nil response: %w", err)
		}
		return nil, fmt.Errorf("do request: %w", err)
	}
	if resp == nil {
		c.recordFailure()
		return nil, fmt.Errorf("do request: nil response")
	}
	if resp.Body == nil {
		c.recordFailure()
		return nil, fmt.Errorf("do request: nil response body")
	}

	return resp, nil
}

func (c *Client) retryUnauthorizedStreams(
	ctx context.Context,
	userLogins []string,
	allowRefreshRetry bool,
) (*StreamsResponse, error) {
	c.recordFailure()
	c.invalidateToken()

	if !allowRefreshRetry {
		return nil, &apperrors.APIError{
			Operation:  "twitch_get_streams",
			StatusCode: http.StatusUnauthorized,
			Err:        errors.New("unauthorized after token refresh"),
		}
	}

	if err := c.refreshToken(ctx); err != nil {
		return nil, fmt.Errorf("refresh token after 401: %w", err)
	}

	retriedStreams, err := c.getStreams(ctx, userLogins, false)
	if err != nil {
		return nil, fmt.Errorf("retry get streams after refresh: %w", err)
	}

	return retriedStreams, nil
}

func (c *Client) readStreamsResponseBody(resp *http.Response) ([]byte, error) {
	if err := c.validateStreamsResponse(resp); err != nil {
		return nil, fmt.Errorf("validate response: %w", err)
	}

	body, err := jsonutil.ReadAllLimit(resp.Body, c.maxResponseBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	return body, nil
}

func (c *Client) validateStreamsResponse(resp *http.Response) error {
	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests:
		c.recordFailure()

		return &apperrors.APIError{
			Operation:  "twitch_get_streams",
			StatusCode: http.StatusTooManyRequests,
			Err:        errors.New("rate limited"),
		}
	case resp.StatusCode >= http.StatusInternalServerError:
		c.recordFailure()

		return &apperrors.APIError{
			Operation:  "twitch_get_streams",
			StatusCode: resp.StatusCode,
			Err:        errors.New("server error"),
		}
	default:
		return &apperrors.APIError{
			Operation:  "twitch_get_streams",
			StatusCode: resp.StatusCode,
			Err:        errors.New("unexpected status"),
		}
	}
}

func (c *Client) decodeStreamsResponse(body []byte) (*StreamsResponse, error) {
	var result StreamsResponse

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &result, nil
}

func (c *Client) newStreamsRequest(ctx context.Context, userLogins []string) (*http.Request, error) {
	params := url.Values{}

	for _, login := range userLogins {
		params.Add("user_login", login)
	}

	reqURL := c.baseURL + "/streams?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	token := c.currentToken()
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Client-Id", c.clientID)

	return req, nil
}
