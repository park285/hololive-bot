package apiclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/park285/shared-go/v2/pkg/httputil"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/constants"
)

const holodexUserAgent = "hololive-bot (Linux; Holodex API client; +https://github.com/park285/hololive-bot)"

func (c *APIClient) DoRequest(ctx context.Context, method, path string, params url.Values) ([]byte, error) {
	if err := c.rejectIfCircuitOpen(); err != nil {
		return nil, fmt.Errorf("reject if circuit open: %w", err)
	}

	if c.apiKey == "" {
		return nil, errNoAPIKeys
	}

	state := holodexRequestRetryState{
		maxAttempts:       min(1+constants.RetryConfig.MaxAttempts, 10),
		maxTimeoutRetries: 3,
	}

	out, err := c.doRequestWithRetry(ctx, method, path, params, state)
	if err != nil {
		return out, fmt.Errorf("do request with retry: %w", err)
	}

	return out, nil
}

func (c *APIClient) doRequestWithRetry(ctx context.Context, method, path string, params url.Values, state holodexRequestRetryState) ([]byte, error) {
	for attempt := range state.maxAttempts {
		body, done, err := c.runHolodexRequestAttempt(ctx, method, path, params, attempt, state.maxAttempts)
		if done {
			out, finishErr := c.finishHolodexRequestResult(body, err)

			return out, errors.Join(finishErr)
		}

		stop, retryErr := c.prepareHolodexRequestRetry(ctx, path, attempt, err, &state)
		if retryErr != nil {
			return nil, errors.Join(retryErr)
		}

		if stop {
			break
		}
	}

	if state.lastErr != nil {
		return nil, state.lastErr
	}

	return nil, fmt.Errorf("holodex request failed after %d attempts", state.maxAttempts)
}

func (c *APIClient) finishHolodexRequestResult(body []byte, attemptErr error) ([]byte, error) {
	out, err := c.finishHolodexRequestAttempt(body, attemptErr)
	if err != nil {
		return nil, fmt.Errorf("finish holodex request attempt: %w", err)
	}

	return out, nil
}

func (c *APIClient) prepareHolodexRequestRetry(ctx context.Context, path string, attempt int, attemptErr error, state *holodexRequestRetryState) (bool, error) {
	if state.recordAttemptError(c.logger, path, attemptErr) {
		return true, nil
	}

	if err := c.waitHolodexRequestBackoff(ctx, attempt, state.maxAttempts); err != nil {
		return false, fmt.Errorf("wait holodex request backoff: %w", err)
	}

	return false, nil
}

func (c *APIClient) runHolodexRequestAttempt(ctx context.Context, method, path string, params url.Values, attempt, maxAttempts int) (result0 []byte, ok1 bool, err error) {
	if waitErr := c.waitForRateLimiter(ctx, path); waitErr != nil {
		return nil, true, fmt.Errorf("wait for rate limiter: %w", waitErr)
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, true, fmt.Errorf("context canceled before request: %w", ctxErr)
	}

	if acquireErr := c.acquireSemaphore(ctx); acquireErr != nil {
		return nil, true, fmt.Errorf("acquire semaphore: %w", acquireErr)
	}

	defer c.releaseSemaphore()

	out1, out2, err := c.tryHolodexRequest(ctx, method, path, params, attempt, maxAttempts)
	if err != nil {
		return out1, out2, fmt.Errorf("try holodex request: %w", err)
	}

	return out1, out2, nil
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
		return nil, true, fmt.Errorf("request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		done, handleErr := c.handleHolodexRequestError(ctx, err, resp == nil, attempt, maxAttempts)

		return nil, done, fmt.Errorf("handle holodex request error: %w", handleErr)
	}

	if resp == nil {
		return nil, true, errors.New("nil Holodex response")
	}

	if validateErr := validateHolodexResponse(resp); validateErr != nil {
		return nil, true, fmt.Errorf("validate holodex response: %w", validateErr)
	}

	defer c.closeHolodexResponse(resp)

	maxBody := c.maxResponseBodyBytes
	if maxBody <= 0 {
		maxBody = settings.DefaultMaxResponseBodyBytes
	}

	body, readErr := httputil.ReadAllLimited(resp.Body, maxBody)
	if readErr != nil {
		return nil, false, fmt.Errorf("failed to read response: %w", readErr)
	}

	out1, out2, err := c.processHolodexResponse(ctx, resp.StatusCode, body, reqURL, attempt, maxAttempts)
	if err != nil {
		return out1, out2, fmt.Errorf("process holodex response: %w", err)
	}

	return out1, out2, nil
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
		return errors.New("HTTP request failed: nil response")
	}

	if resp.Body == nil {
		return errors.New("HTTP request failed: nil response body")
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
