package apiclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/park285/shared-go/pkg/jsonutil"

	"github.com/kapu/hololive-shared/pkg/constants"
)

const holodexUserAgent = "hololive-bot (Linux; Holodex API client; +https://github.com/park285/hololive-bot)"

func (c *APIClient) DoRequest(ctx context.Context, method, path string, params url.Values) ([]byte, error) {
	if err := c.rejectIfCircuitOpen(); err != nil {
		return nil, err
	}

	if c.apiKey == "" {
		return nil, errNoAPIKeys
	}

	state := holodexRequestRetryState{
		maxAttempts:       min(1+constants.RetryConfig.MaxAttempts, 10),
		maxTimeoutRetries: 3,
	}

	return c.doRequestWithRetry(ctx, method, path, params, state)
}

func (c *APIClient) doRequestWithRetry(ctx context.Context, method, path string, params url.Values, state holodexRequestRetryState) ([]byte, error) {
	for attempt := range state.maxAttempts {
		body, done, err := c.runHolodexRequestAttempt(ctx, method, path, params, attempt, state.maxAttempts)
		if done {
			return c.finishHolodexRequestAttempt(body, err)
		}

		if state.recordAttemptError(c.logger, path, err) {
			break
		}
		if err := c.waitHolodexRequestBackoff(ctx, attempt, state.maxAttempts); err != nil {
			return nil, err
		}
	}

	if state.lastErr != nil {
		return nil, state.lastErr
	}

	return nil, fmt.Errorf("holodex request failed after %d attempts", state.maxAttempts)
}

func (c *APIClient) runHolodexRequestAttempt(ctx context.Context, method, path string, params url.Values, attempt, maxAttempts int) (result0 []byte, ok1 bool, err error) {
	if err := c.waitForRateLimiter(ctx, path); err != nil {
		return nil, true, err
	}
	if err := ctx.Err(); err != nil {
		return nil, true, fmt.Errorf("context canceled before request: %w", err)
	}
	if err := c.acquireSemaphore(ctx); err != nil {
		return nil, true, err
	}
	defer c.releaseSemaphore()
	return c.tryHolodexRequest(ctx, method, path, params, attempt, maxAttempts)
}

func (c *APIClient) finishHolodexRequestAttempt(body []byte, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	c.resetCircuit()
	return body, nil
}

func (c *APIClient) tryHolodexRequest(ctx context.Context, method, path string, params url.Values, attempt, maxAttempts int) (result0 []byte, ok1 bool, err error) {
	attemptCtx, cancel := context.WithTimeout(ctx, c.perAttemptTimeout)
	defer cancel()

	reqURL := c.buildRequestURL(path, params)
	req, err := c.newRequest(attemptCtx, method, reqURL, c.getNextAPIKey())
	if err != nil {
		return nil, true, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		done, err := c.handleHolodexRequestError(ctx, err, resp == nil, attempt, maxAttempts)
		return nil, done, err
	}
	if resp == nil {
		return nil, true, fmt.Errorf("nil Holodex response")
	}
	if err := validateHolodexResponse(resp); err != nil {
		return nil, true, err
	}
	defer c.closeHolodexResponse(resp)

	body, readErr := jsonutil.ReadAllLimit(resp.Body, c.maxResponseBodyBytes)
	if readErr != nil {
		return nil, false, fmt.Errorf("failed to read response: %w", readErr)
	}

	return c.processHolodexResponse(ctx, resp.StatusCode, body, reqURL, attempt, maxAttempts)
}

func (c *APIClient) closeHolodexResponse(resp *http.Response) {
	if closeErr := resp.Body.Close(); closeErr != nil && c.logger != nil {
		c.logger.Warn("Failed to close Holodex response body", "error", closeErr)
	}
}

func (c *APIClient) handleHolodexRequestError(
	ctx context.Context,
	err error,
	nilResponse bool,
	attempt int,
	maxAttempts int,
) (bool, error) {
	if nilResponse {
		err = fmt.Errorf("nil response: %w", err)
	}
	if c.retryAfterNetworkFailure(ctx, err, attempt, maxAttempts) {
		return false, fmt.Errorf("HTTP request failed (retrying): %w", err)
	}
	return true, fmt.Errorf("HTTP request failed: %w", err)
}

func validateHolodexResponse(resp *http.Response) error {
	if resp == nil {
		return fmt.Errorf("HTTP request failed: nil response")
	}
	if resp.Body == nil {
		return fmt.Errorf("HTTP request failed: nil response body")
	}
	return nil
}

func (c *APIClient) buildRequestURL(path string, params url.Values) string {
	reqURL := c.baseURL + path
	if params != nil {
		reqURL += "?" + params.Encode()
	}
	return reqURL
}

func (c *APIClient) newRequest(ctx context.Context, method, reqURL, apiKey string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-APIKEY", apiKey)
	// Holodex API Terms 준수를 위해 정직한 User-Agent 사용 (Section 6: Attribution)
	req.Header.Set("User-Agent", holodexUserAgent)
	return req, nil
}
