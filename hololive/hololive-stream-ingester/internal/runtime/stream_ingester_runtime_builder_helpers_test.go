package runtime

import (
	"bytes"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

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

func TestLogYouTubeScraperBudgetSummary_ReportsPollerAndResolverRPM(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	logYouTubeScraperBudgetSummary(
		summarizeYouTubeScraperBudget(
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
		),
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

func TestBuildStreamIngesterYouTubeComponents_FailsWhenPollerBudgetExceedsRateLimit(t *testing.T) {
	t.Parallel()

	_, _, _, err := buildStreamIngesterYouTubeComponents(
		config.ScraperConfig{
			Poll: config.ScraperPoll{
				Videos:    15 * time.Minute,
				Shorts:    time.Minute,
				Community: time.Minute,
				Stats:     6 * time.Hour,
				Live:      10 * time.Minute,
			},
		},
		&databasemocks.Client{
			GetGormDBFunc: func() *gorm.DB { return nil },
		},
		repeatChannelIDs("UC_NOTIFY_", 12),
		repeatChannelIDs("UC_STATS_", 111),
		buildSharedYouTubeScraperClient(config.ScraperConfig{}, nil, nil),
		nil,
		nil,
		nil,
		nil,
		testLogger(),
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "poller RPM")
	assert.Contains(t, err.Error(), "increase poll intervals or reduce target channels")
}

func TestBuildStreamIngesterYouTubeComponents_AllowsBudgetSafeDefaultPollConfig(t *testing.T) {
	t.Parallel()

	scheduler, dispatcher, registrations, err := buildStreamIngesterYouTubeComponents(
		config.ScraperConfig{},
		&databasemocks.Client{
			GetGormDBFunc: func() *gorm.DB { return nil },
		},
		repeatChannelIDs("UC_NOTIFY_", 12),
		repeatChannelIDs("UC_STATS_", 111),
		buildSharedYouTubeScraperClient(config.ScraperConfig{}, nil, nil),
		nil,
		nil,
		nil,
		nil,
		testLogger(),
	)

	require.NoError(t, err)
	require.NotNil(t, scheduler)
	require.NotNil(t, dispatcher)
	require.Len(t, registrations, 5)
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

func repeatChannelIDs(prefix string, count int) []string {
	channelIDs := make([]string, 0, count)
	for idx := 0; idx < count; idx++ {
		channelIDs = append(channelIDs, fmt.Sprintf("%s%d", prefix, idx+1))
	}
	return channelIDs
}
