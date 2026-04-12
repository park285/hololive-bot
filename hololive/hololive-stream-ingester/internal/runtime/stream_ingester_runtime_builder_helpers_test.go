package runtime

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func TestEstimateResolvedPollerRPM_UsesExplicitChannelCounts(t *testing.T) {
	t.Parallel()

	rpm := estimateResolvedPollerRPM([]providers.ChannelPollerRegistration{
		providers.NewChannelPollerRegistration(fakeTestPoller{name: "videos"}, poller.PriorityNormal, time.Minute).
			WithChannelIDs([]string{"UC_A", "UC_A", "UC_B"}),
		providers.NewChannelPollerRegistration(fakeTestPoller{name: "stats"}, poller.PriorityLow, 2*time.Minute).
			WithChannelIDs([]string{"UC_STATS"}),
	})

	assert.Equal(t, 2.5, rpm)
}

func TestLogCombinedYouTubeScraperBudget_ReportsPollerAndResolverRPM(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	logCombinedYouTubeScraperBudget(
		config.ScraperConfig{
			PublishedAtResolver: config.ScraperPublishedAtResolverConfig{
				Enabled:          true,
				Interval:         15 * time.Second,
				MaxResolvePerRun: 2,
			},
		},
		[]providers.ChannelPollerRegistration{
			providers.NewChannelPollerRegistration(fakeTestPoller{name: "videos"}, poller.PriorityNormal, time.Second).
				WithChannelIDs([]string{"UC_A", "UC_B"}),
		},
		logger,
	)

	assert.Contains(t, logBuf.String(), `"msg":"youtube_scraper_combined_budget_summary"`)
	assert.Contains(t, logBuf.String(), `"expected_poller_rpm":120`)
	assert.Contains(t, logBuf.String(), `"expected_poller_retry_amplified_rpm_max":360`)
	assert.Contains(t, logBuf.String(), `"expected_resolver_rpm":8`)
	assert.Contains(t, logBuf.String(), `"expected_resolver_retry_amplified_rpm_max":24`)
	assert.Contains(t, logBuf.String(), `"expected_combined_rpm":128`)
	assert.Contains(t, logBuf.String(), `"expected_combined_retry_amplified_rpm_max":384`)
	assert.Contains(t, logBuf.String(), `"msg":"youtube_scraper_combined_budget_exceeds_rate_limit"`)
	assert.Contains(t, logBuf.String(), `"msg":"youtube_scraper_retry_amplified_budget_exceeds_rate_limit"`)
}

func TestBuildPendingPublishedAtResolver_LogsResolveTimeout(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	resolver := buildPendingPublishedAtResolver(
		config.ScraperConfig{
			PublishedAtResolver: config.ScraperPublishedAtResolverConfig{
				Enabled:           true,
				Interval:          12 * time.Second,
				BatchSize:         9,
				MaxResolvePerRun:  3,
				MaxRunDuration:    12 * time.Second,
				ResolveTimeout:    10 * time.Second,
				MinDetectedAge:    45 * time.Second,
				FailureBackoffTTL: 7 * time.Minute,
			},
		},
		&databasemocks.Client{},
		scraper.NewClient(),
		nil,
		logger,
	)

	require.NotNil(t, resolver)
	assert.Contains(t, logBuf.String(), `"msg":"published_at_resolver_configured"`)
	assert.Contains(t, logBuf.String(), `"resolve_timeout":10000000000`)
}
