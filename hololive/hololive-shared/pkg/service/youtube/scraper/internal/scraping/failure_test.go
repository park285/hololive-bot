package scraping

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClassifyFailureRateLimitedWithRetryAfter(t *testing.T) {
	err := &httpStatusError{
		code:       http.StatusTooManyRequests,
		retryAfter: 15 * time.Second,
		cause:      ErrRateLimited,
	}

	detail := ClassifyFailure(fmt.Errorf("wrapped: %w", err), FailureSourceHTML)

	require.Equal(t, FailureReasonRateLimited, detail.Reason)
	require.Equal(t, FailureSourceHTML, detail.Source)
	require.Equal(t, http.StatusTooManyRequests, detail.StatusCode)
	require.Equal(t, 15*time.Second, detail.RetryAfter)
}

func TestClassifyFailureForbidden(t *testing.T) {
	err := &httpStatusError{
		code:  http.StatusForbidden,
		cause: ErrForbidden,
	}

	detail := ClassifyFailure(err, FailureSourceHTML)

	require.Equal(t, FailureReasonForbidden, detail.Reason)
	require.Equal(t, http.StatusForbidden, detail.StatusCode)
}

func TestClassifyFailureParserDrift(t *testing.T) {
	err := NewParserDriftError("upcoming_events", "extract_yt_initial_data", errors.New("marker missing"))

	detail := ClassifyFailure(err, FailureSourceHTML)

	require.Equal(t, FailureReasonParserDrift, detail.Reason)
	require.True(t, IsParserDriftError(err))
}

func TestClassifyFailureCooldown(t *testing.T) {
	err := &CooldownError{
		Kind:  "youtube transient",
		Delay: 3 * time.Minute,
		Err:   ErrTransientCooldown,
	}

	detail := ClassifyFailure(err, FailureSourceHTML)

	require.Equal(t, FailureReasonCooldown, detail.Reason)
	require.Equal(t, 3*time.Minute, detail.RetryAfter)
}
