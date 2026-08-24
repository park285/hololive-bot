package apiclient

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

func (c *APIClient) processHolodexResponse(ctx context.Context, status int, body []byte, reqURL string, attempt, maxAttempts int) (result0 []byte, ok1 bool, err error) {
	if status == http.StatusTooManyRequests {
		done, rateLimitErr := c.handleRateLimitedResponse(status, reqURL, attempt, maxAttempts)
		if rateLimitErr != nil {
			return nil, done, fmt.Errorf("handle rate limited response: %w", rateLimitErr)
		}

		return nil, done, nil
	}

	if status == http.StatusForbidden {
		return nil, true, fmt.Errorf("handle forbidden response: %w", c.handleForbiddenResponse(status, body, reqURL, attempt))
	}

	if status >= 500 {
		done, serverErr := c.handleServerError(ctx, status, attempt, maxAttempts)

		return nil, done, fmt.Errorf("handle server error: %w", serverErr)
	}

	if status >= 400 {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return nil, true, holodexClientError(status, reqURL)
	}

	return body, true, nil
}

func (c *APIClient) handleRateLimitedResponse(status int, reqURL string, attempt, maxAttempts int) (done bool, err error) {
	c.logger.Warn("Holodex rate limited, retrying",
		slog.Int("status", status),
		slog.Int("attempt", attempt+1),
		slog.String("url", reqURL),
	)

	if attempt < maxAttempts-1 {
		return false, nil
	}

	return true, NewKeyRotationError(reqURL, status)
}

func (c *APIClient) handleForbiddenResponse(status int, body []byte, reqURL string, attempt int) error {
	c.logger.Error("Holodex forbidden response",
		slog.Int("status", status),
		slog.Int("attempt", attempt+1),
		slog.String("url", reqURL),
		slog.String("body_preview", summarizeHolodexErrorBody(body)),
	)

	return NewAPIError(reqURL, status)
}

func holodexClientError(status int, reqURL string) error {
	return NewAPIError(reqURL, status)
}

func (c *APIClient) handleServerError(_ context.Context, status, attempt, maxAttempts int) (done bool, err error) {
	c.openCircuit()
	c.logger.Warn("Server error",
		slog.Int("status", status),
	)

	// circuit이 열렸으면 추가 재시도 없이 즉시 중단합니다.
	if c.IsCircuitOpen() {
		return true, NewAPIError(fmt.Sprintf("Server error: %d", status), status)
	}

	if attempt < maxAttempts-1 {
		return false, NewAPIError(fmt.Sprintf("Server error: %d", status), status)
	}

	return true, NewAPIError(fmt.Sprintf("Server error: %d", status), status)
}

func summarizeHolodexErrorBody(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}

	const maxPreviewLen = 256

	if len(trimmed) <= maxPreviewLen {
		return trimmed
	}

	return trimmed[:maxPreviewLen] + "..."
}
