package apiclient

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func (c *APIClient) processHolodexResponse(ctx context.Context, status int, body []byte, reqURL string, attempt, maxAttempts int) ([]byte, bool, error) {
	if status == http.StatusTooManyRequests {
		return c.handleRateLimitedResponse(status, reqURL, attempt, maxAttempts)
	}
	if status == http.StatusForbidden {
		return c.handleForbiddenResponse(status, body, reqURL, attempt)
	}
	if status >= 500 {
		return c.handleServerError(ctx, status, attempt, maxAttempts)
	}
	if status >= 400 {
		return nil, true, holodexClientError(status, reqURL)
	}
	return body, true, nil
}

func (c *APIClient) handleRateLimitedResponse(status int, reqURL string, attempt int, maxAttempts int) ([]byte, bool, error) {
	c.logger.Warn("Holodex rate limited, retrying",
		slog.Int("status", status),
		slog.Int("attempt", attempt+1),
		slog.String("url", reqURL),
	)
	if attempt < maxAttempts-1 {
		return nil, false, nil
	}
	return nil, true, NewKeyRotationError("Holodex rate limit exhausted", status, map[string]any{
		"url": reqURL,
	})
}

func (c *APIClient) handleForbiddenResponse(status int, body []byte, reqURL string, attempt int) ([]byte, bool, error) {
	c.logger.Error("Holodex forbidden response",
		slog.Int("status", status),
		slog.Int("attempt", attempt+1),
		slog.String("url", reqURL),
		slog.String("body_preview", summarizeHolodexErrorBody(body)),
	)
	return nil, true, NewAPIError("Holodex forbidden", status, map[string]any{
		"operation": reqURL,
	})
}

func holodexClientError(status int, reqURL string) error {
	return NewAPIError(fmt.Sprintf("Client error: %d", status), status, map[string]any{
		"operation": reqURL,
	})
}

func (c *APIClient) handleServerError(_ context.Context, status, attempt, maxAttempts int) ([]byte, bool, error) {
	count := c.incrementFailureCount()
	c.logger.Warn("Server error",
		slog.Int("status", status),
		slog.Int("failure_count", count),
	)

	if count >= constants.CircuitBreakerConfig.FailureThreshold {
		c.openCircuit()
		return nil, true, NewAPIError(fmt.Sprintf("Server error: %d", status), status, nil)
	}

	if attempt < maxAttempts-1 {
		return nil, false, NewAPIError(fmt.Sprintf("Server error: %d", status), status, nil)
	}

	return nil, true, NewAPIError(fmt.Sprintf("Server error: %d", status), status, nil)
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
