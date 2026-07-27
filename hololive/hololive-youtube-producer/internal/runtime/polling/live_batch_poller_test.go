package polling

import (
	"fmt"
	"testing"
	"time"

	providers "github.com/kapu/hololive-shared/pkg/providers"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/pollers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/stretchr/testify/require"
)

func TestAppendLivePollerRegistrationsBatchesWhenProviderEnabled(t *testing.T) {
	base := pollers.NewLivePollerWithStatusProvider(nil, nil, nil)
	ids := make([]string, 0, defaultLiveBatchChannelChunkSize+1)
	for i := range defaultLiveBatchChannelChunkSize + 1 {
		ids = append(ids, fmt.Sprintf("UC_TEST_%02d", i))
	}

	registrations := appendLivePollerRegistrations(nil, &livePollerRegistrationSpec{
		Name:           "live",
		Base:           base,
		BatchBase:      base,
		BatchEnabled:   true,
		Priority:       scheduler.PriorityHigh,
		Interval:       time.Minute,
		ChannelIDs:     ids,
		TargetGroup:    providers.ChannelTargetGroupNotification,
		BurstClass:     polling.BudgetBurstPrimary,
		BudgetPriority: polling.BudgetPriorityHigh,
	})

	require.Len(t, registrations, 2)
	require.Equal(t, "live_batch_01", registrations[0].Poller.Name())
	require.Equal(t, []string{providers.SyntheticGlobalPollerChannelID}, registrations[0].ChannelIDs)
	require.Equal(t, 1.0, registrations[0].BudgetProfile.SourceUnits[polling.BudgetSourceHolodexLive])
	require.Zero(t, registrations[0].BudgetProfile.SourceUnits[polling.BudgetSourceYouTubeScraper])
	require.Equal(t, float64(defaultLiveBatchChannelChunkSize*scraper.LiveStatusFallbackFetchPolicy.MaxAttempts), registrations[0].BudgetProfile.FallbackSourceUnits[polling.BudgetSourceYouTubeScraper])
	require.Equal(t, float64(defaultLiveBatchChannelChunkSize), registrations[0].BudgetProfile.SourceUnits[polling.BudgetSourcePostgresWrite])
	require.Equal(t, float64(defaultLiveBatchChannelChunkSize*scraper.LiveStatusFallbackFetchPolicy.MaxAttempts), registrations[0].WorstCaseRequestUnitsPerRun)
}

func TestSummarizeBudgetIncludesLiveBatchFallbackInYouTubeScraperFaultEnvelope(t *testing.T) {
	fallbackUnits := float64(30 * scraper.LiveStatusFallbackFetchPolicy.MaxAttempts)
	base := sourceCooldownTestPoller{name: "live"}
	registration := providers.NewChannelPollerRegistration(base, scheduler.PriorityHigh, time.Minute).
		WithChannelIDs([]string{providers.SyntheticGlobalPollerChannelID}).
		WithWorstCaseRequestUnitsPerRun(fallbackUnits).
		WithBudgetProfile(holodexLiveBatchBudgetProfile(30, polling.BudgetBurstPrimary, polling.BudgetPriorityHigh))

	summary := summarizeYouTubeProducerBudget([]providers.ChannelPollerRegistration{registration})

	require.Zero(t, summary.CombinedRPM)
	require.Equal(t, fallbackUnits, summary.CombinedRetryAmplifiedRPM)
}
