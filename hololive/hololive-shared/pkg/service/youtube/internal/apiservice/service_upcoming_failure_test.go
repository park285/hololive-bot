package apiservice

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/stretchr/testify/require"
)

func TestClassifyYouTubeAPIFailureQuotaExceeded(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &QuotaExceededError{})

	detail := classifyYouTubeAPIFailure(err)

	require.Equal(t, scraper.FailureSourceAPI, detail.Source)
	require.Equal(t, scraper.FailureReasonForbidden, detail.Reason)
	require.Equal(t, http.StatusForbidden, detail.StatusCode)
}
