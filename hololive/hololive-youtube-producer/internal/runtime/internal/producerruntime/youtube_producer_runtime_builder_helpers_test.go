package producerruntime

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
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polling"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/publishedat"
)

func TestEstimateResolvedPollerRPM_UsesExplicitChannelCounts(t *testing.T) {
	t.Parallel()

	rpm := polling.EstimateResolvedPollerRPM([]providers.ChannelPollerRegistration{
		providers.NewChannelPollerRegistration(fakeTestPoller{name: "videos"}, poller.PriorityNormal, time.Minute).
			WithChannelIDs([]string{"UC_A", "UC_A", "UC_B"}),
		providers.NewChannelPollerRegistration(fakeTestPoller{name: "stats"}, poller.PriorityLow, 2*time.Minute).
			WithChannelIDs([]string{"UC_STATS"}),
	})

	assert.Equal(t, 2.5, rpm)
}

func TestLogYouTubeProducerBudgetSummary_ReportsPollerAndResolverRPM(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	polling.LogBudgetSummary(
		polling.SummarizeBudget(
			[]providers.ChannelPollerRegistration{
				providers.NewChannelPollerRegistration(fakeTestPoller{name: "videos"}, poller.PriorityNormal, time.Second).
					WithChannelIDs([]string{"UC_A", "UC_B"}).
					WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
					WithWorstCaseRequestUnitsPerRun(6),
				providers.NewGlobalPollerRegistration(fakeTestPoller{name: poller.PendingPublishedAtResolverPollerName}, poller.PriorityLow, 15*time.Second).
					WithRequestsPerRun(2).
					WithWorstCaseAttempts(1).
					WithWorstCaseRequestUnitsPerRun(2),
			},
		),
		logger,
	)

	assert.Contains(t, logBuf.String(), `"msg":"youtube_producer_combined_budget_summary"`)
	assert.Contains(t, logBuf.String(), `"expected_poller_rpm":120`)
	assert.Contains(t, logBuf.String(), `"expected_poller_retry_amplified_rpm_max":720`)
	assert.Contains(t, logBuf.String(), `"expected_resolver_rpm":8`)
	assert.Contains(t, logBuf.String(), `"expected_resolver_retry_amplified_rpm_max":8`)
	assert.Contains(t, logBuf.String(), `"expected_combined_rpm":128`)
	assert.Contains(t, logBuf.String(), `"expected_combined_retry_amplified_rpm_max":728`)
	assert.Contains(t, logBuf.String(), `"msg":"youtube_producer_combined_budget_exceeds_rate_limit"`)
	assert.Contains(t, logBuf.String(), `"msg":"youtube_producer_fault_envelope_exceeds_rate_limit"`)
}

func TestSummarizeYouTubeProducerBudget_UsesRegistrationRequestsAndAttempts(t *testing.T) {
	t.Parallel()

	summary := polling.SummarizeBudget([]providers.ChannelPollerRegistration{
		providers.NewChannelPollerRegistration(fakeTestPoller{name: "shorts"}, poller.PriorityLow, 2*time.Minute).
			WithChannelIDs([]string{"UC_A", "UC_B"}).
			WithWorstCaseAttempts(1).
			WithWorstCaseRequestUnitsPerRun(4),
		providers.NewChannelPollerRegistration(fakeTestPoller{name: "videos"}, poller.PriorityNormal, 10*time.Minute).
			WithChannelIDs([]string{"UC_A"}).
			WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
			WithWorstCaseRequestUnitsPerRun(9),
		providers.NewGlobalPollerRegistration(fakeTestPoller{name: poller.PendingPublishedAtResolverPollerName}, poller.PriorityLow, 30*time.Second).
			WithRequestsPerRun(2).
			WithWorstCaseAttempts(1).
			WithWorstCaseRequestUnitsPerRun(2),
	})

	assert.InDelta(t, 1.1, summary.PollerRPM, 0.0001)
	assert.InDelta(t, 4.9, summary.PollerRetryAmplifiedRPM, 0.0001)
	assert.InDelta(t, 4.0, summary.ResolverRPM, 0.0001)
	assert.InDelta(t, 4.0, summary.ResolverRetryAmplifiedRPM, 0.0001)
	assert.InDelta(t, 5.1, summary.CombinedRPM, 0.0001)
	assert.InDelta(t, 8.9, summary.CombinedRetryAmplifiedRPM, 0.0001)
}

func TestLogYouTubeProducerBudgetSummary_FaultEnvelopeCanExceedSteadyBudgetViaRecoveryBranches(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	polling.LogBudgetSummary(
		polling.SummarizeBudget([]providers.ChannelPollerRegistration{
			providers.NewChannelPollerRegistration(fakeTestPoller{name: "shorts"}, poller.PriorityLow, 2*time.Minute).
				WithChannelIDs(repeatChannelIDs("UC_NOTIFY_", 39)).
				WithWorstCaseAttempts(scraper.HighFrequencyChannelFetchPolicy.MaxAttempts).
				WithWorstCaseRequestUnitsPerRun(4),
		}),
		logger,
	)

	assert.NotContains(t, logBuf.String(), `"msg":"youtube_producer_combined_budget_exceeds_rate_limit"`)
	assert.Contains(t, logBuf.String(), `"msg":"youtube_producer_fault_envelope_exceeds_rate_limit"`)
	assert.Contains(t, logBuf.String(), `"expected_combined_rpm":19.5`)
	assert.Contains(t, logBuf.String(), `"expected_combined_retry_amplified_rpm_max":78`)
}

func TestBuildYouTubeProducerYouTubeComponents_FailsWhenCombinedBudgetExceedsRateLimit(t *testing.T) {
	t.Parallel()

	resolver := publishedat.BuildPendingResolver(
		config.ScraperConfig{
			PublishedAtResolver: config.ScraperPublishedAtResolverConfig{
				Enabled:          true,
				Interval:         time.Second,
				MaxResolvePerRun: 1,
			},
		},
		&databasemocks.Client{GetGormDBFunc: func() *gorm.DB { return nil }},
		scraper.NewClient(),
		func(poller.NotificationRouteRequest) bool { return true },
		testLogger(),
	)
	require.NotNil(t, resolver)

	_, _, err := polling.BuildComponents(
		config.ScraperConfig{
			Poll: config.ScraperPoll{
				Videos:    15 * time.Minute,
				Shorts:    6 * time.Minute,
				Community: 6 * time.Minute,
				Stats:     6 * time.Hour,
				Live:      10 * time.Minute,
			},
			PublishedAtResolver: config.ScraperPublishedAtResolverConfig{
				Enabled:          true,
				Interval:         time.Second,
				MaxResolvePerRun: 1,
			},
		},
		&databasemocks.Client{
			GetGormDBFunc: func() *gorm.DB { return nil },
		},
		repeatChannelIDs("UC_NOTIFY_", 12),
		repeatChannelIDs("UC_STATS_", 111),
		polling.BuildSharedClient(config.ScraperConfig{}, nil, nil),
		nil,
		func(poller.NotificationRouteRequest) bool { return true },
		resolver,
		testLogger(),
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "combined active scraper RPM")
	assert.Contains(t, err.Error(), "increase poll intervals or reduce target channels")
}

func TestBuildYouTubeProducerYouTubeComponents_AllowsBudgetSafeDefaultPollConfig(t *testing.T) {
	t.Parallel()

	scheduler, registrations, err := polling.BuildComponents(
		config.ScraperConfig{},
		&databasemocks.Client{
			GetGormDBFunc: func() *gorm.DB { return nil },
		},
		repeatChannelIDs("UC_NOTIFY_", 12),
		repeatChannelIDs("UC_STATS_", 111),
		polling.BuildSharedClient(config.ScraperConfig{}, nil, nil),
		nil,
		nil,
		nil,
		testLogger(),
	)

	require.NoError(t, err)
	require.NotNil(t, scheduler)
	require.Len(t, registrations, 5)
}

func TestBuildYouTubeProducerYouTubeComponents_ProductionShortsIntervalKeepsRecoveryEnvelopeWithinBudget(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	resolver := publishedat.BuildPendingResolver(
		config.ScraperConfig{
			PublishedAtResolver: config.DefaultScraperPublishedAtResolverConfig(),
		},
		&databasemocks.Client{GetGormDBFunc: func() *gorm.DB { return nil }},
		scraper.NewClient(),
		func(poller.NotificationRouteRequest) bool { return true },
		logger,
	)
	require.NotNil(t, resolver)

	_, _, err := polling.BuildComponents(
		config.ScraperConfig{
			Poll: config.ScraperPoll{
				Videos:    15 * time.Minute,
				Shorts:    2 * time.Minute,
				Community: 15 * time.Minute,
				Stats:     6 * time.Hour,
				Live:      10 * time.Minute,
			},
			PublishedAtResolver: config.DefaultScraperPublishedAtResolverConfig(),
		},
		&databasemocks.Client{
			GetGormDBFunc: func() *gorm.DB { return nil },
		},
		repeatChannelIDs("UC_NOTIFY_", 12),
		repeatChannelIDs("UC_STATS_", 111),
		polling.BuildSharedClient(config.ScraperConfig{}, nil, nil),
		nil,
		func(poller.NotificationRouteRequest) bool { return true },
		resolver,
		logger,
	)

	require.NoError(t, err)
	assert.NotContains(t, logBuf.String(), `"msg":"youtube_producer_combined_budget_exceeds_rate_limit"`)
	assert.NotContains(t, logBuf.String(), `"msg":"youtube_producer_fault_envelope_exceeds_rate_limit"`)
	assert.Contains(t, logBuf.String(), `"expected_combined_retry_amplified_rpm_max":18.858333333333334`)
}

func TestBuildPendingPublishedAtResolver_LogsResolveTimeout(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	resolver := publishedat.BuildPendingResolver(
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
		func(poller.NotificationRouteRequest) bool { return true },
		logger,
	)

	require.NotNil(t, resolver)
	assert.Contains(t, logBuf.String(), `"msg":"published_at_resolver_configured"`)
	assert.Contains(t, logBuf.String(), `"resolve_timeout":10000000000`)
}

func TestSummarizeYouTubeProducerBudget_ExcludesInactiveResolver(t *testing.T) {
	t.Parallel()

	summary := polling.SummarizeBudget(
		[]providers.ChannelPollerRegistration{
			providers.NewChannelPollerRegistration(fakeTestPoller{name: "videos"}, poller.PriorityNormal, 10*time.Minute).
				WithChannelIDs([]string{"UC_A"}).
				WithWorstCaseAttempts(scraper.FetchPageMaxAttempts),
		},
	)

	assert.Equal(t, 0.1, summary.PollerRPM)
	assert.Zero(t, summary.ResolverRPM)
	assert.Equal(t, summary.PollerRPM, summary.CombinedRPM)
}

func repeatChannelIDs(prefix string, count int) []string {
	channelIDs := make([]string, 0, count)
	for idx := range count {
		channelIDs = append(channelIDs, fmt.Sprintf("%s%d", prefix, idx+1))
	}
	return channelIDs
}
