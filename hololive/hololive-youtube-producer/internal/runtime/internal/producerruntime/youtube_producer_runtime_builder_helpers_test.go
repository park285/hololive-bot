package producerruntime

import (
	"bytes"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polling"
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
	})

	assert.InDelta(t, 1.1, summary.PollerRPM, 0.0001)
	assert.InDelta(t, 4.9, summary.PollerRetryAmplifiedRPM, 0.0001)
	assert.InDelta(t, 1.1, summary.CombinedRPM, 0.0001)
	assert.InDelta(t, 4.9, summary.CombinedRetryAmplifiedRPM, 0.0001)
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

	_, _, err := polling.BuildComponents(
		&config.ScraperConfig{
			Poll: config.ScraperPoll{
				Videos:    2 * time.Minute,
				Shorts:    1 * time.Minute,
				Community: 1 * time.Minute,
				Stats:     6 * time.Hour,
				Live:      1 * time.Minute,
			},
		},
		newPollerRegistrationTestDB(t),
		repeatChannelIDs("UC_NOTIFY_", 12),
		repeatChannelIDs("UC_STATS_", 111),
		polling.BuildSharedClient(&config.ScraperConfig{}, nil, nil),
		nil,
		testLogger(),
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "combined active scraper RPM")
	assert.Contains(t, err.Error(), "increase poll intervals or reduce target channels")
}

func TestBuildYouTubeProducerYouTubeComponents_AllowsBudgetSafeDefaultPollConfig(t *testing.T) {
	t.Parallel()

	scheduler, registrations, err := polling.BuildComponents(
		&config.ScraperConfig{},
		newPollerRegistrationTestDB(t),
		repeatChannelIDs("UC_NOTIFY_", 12),
		repeatChannelIDs("UC_STATS_", 111),
		polling.BuildSharedClient(&config.ScraperConfig{}, nil, nil),
		nil,
		testLogger(),
	)

	require.NoError(t, err)
	require.NotNil(t, scheduler)
	require.Len(t, registrations, 5)
}

func TestBuildYouTubeProducerYouTubeComponents_ProductionShortsIntervalStaysWithinRaisedRPMBudget(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	_, _, err := polling.BuildComponentsWithJobClaimer(
		&config.ScraperConfig{
			Poll: config.ScraperPoll{
				Videos:    15 * time.Minute,
				Shorts:    2 * time.Minute,
				Community: 15 * time.Minute,
				Stats:     6 * time.Hour,
				Live:      10 * time.Minute,
			},
		},
		nil,
		&polling.GlobalBudgetWiring{BudgetRPM: 30},
		newPollerRegistrationTestDB(t),
		repeatChannelIDs("UC_NOTIFY_", 12),
		repeatChannelIDs("UC_STATS_", 111),
		polling.BuildSharedClient(&config.ScraperConfig{}, nil, nil),
		nil,
		logger,
	)

	require.NoError(t, err)
	assert.NotContains(t, logBuf.String(), `"msg":"youtube_producer_combined_budget_exceeds_rate_limit"`)
	assert.NotContains(t, logBuf.String(), `"msg":"youtube_producer_fault_envelope_exceeds_rate_limit"`)
	assert.Contains(t, logBuf.String(), `"budget_rpm":30`)
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
	assert.Equal(t, summary.PollerRPM, summary.CombinedRPM)
}

func repeatChannelIDs(prefix string, count int) []string {
	channelIDs := make([]string, 0, count)
	for idx := range count {
		channelIDs = append(channelIDs, fmt.Sprintf("%s%d", prefix, idx+1))
	}
	return channelIDs
}
