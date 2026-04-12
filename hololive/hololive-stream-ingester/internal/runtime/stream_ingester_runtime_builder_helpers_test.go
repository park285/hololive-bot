package runtime

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
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
	assert.Contains(t, logBuf.String(), `"expected_resolver_rpm":8`)
	assert.Contains(t, logBuf.String(), `"expected_combined_rpm":128`)
	assert.Contains(t, logBuf.String(), `"msg":"youtube_scraper_combined_budget_exceeds_rate_limit"`)
}
