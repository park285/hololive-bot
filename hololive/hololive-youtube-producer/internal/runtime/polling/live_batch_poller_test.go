package polling

import (
	"fmt"
	"testing"
	"time"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/stretchr/testify/require"
)

func TestAppendLivePollerRegistrationsBatchesWhenProviderEnabled(t *testing.T) {
	base := poller.NewLivePollerWithStatusProvider(nil, nil, nil)
	ids := make([]string, 0, defaultLiveBatchChannelChunkSize+1)
	for i := range defaultLiveBatchChannelChunkSize + 1 {
		ids = append(ids, fmt.Sprintf("UC_TEST_%02d", i))
	}

	registrations := appendLivePollerRegistrations(nil, &livePollerRegistrationSpec{
		Name:           "live",
		Base:           base,
		BatchBase:      base,
		BatchEnabled:   true,
		Priority:       poller.PriorityHigh,
		Interval:       time.Minute,
		ChannelIDs:     ids,
		TargetGroup:    providers.ChannelTargetGroupNotification,
		BurstClass:     poller.BudgetBurstPrimary,
		BudgetPriority: poller.BudgetPriorityHigh,
	})

	require.Len(t, registrations, 2)
	require.Equal(t, "live_batch_01", registrations[0].Poller.Name())
	require.Equal(t, []string{providers.SyntheticGlobalPollerChannelID}, registrations[0].ChannelIDs)
	require.Equal(t, 1.0, registrations[0].BudgetProfile.SourceUnits[poller.BudgetSourceHolodexLive])
	require.Zero(t, registrations[0].BudgetProfile.SourceUnits[poller.BudgetSourceYouTubeScraper])
	require.Equal(t, float64(defaultLiveBatchChannelChunkSize*scraper.LiveStatusFallbackFetchPolicy.MaxAttempts), registrations[0].BudgetProfile.FallbackSourceUnits[poller.BudgetSourceYouTubeScraper])
	require.Equal(t, float64(defaultLiveBatchChannelChunkSize), registrations[0].BudgetProfile.SourceUnits[poller.BudgetSourcePostgresWrite])
	require.Equal(t, float64(defaultLiveBatchChannelChunkSize*scraper.LiveStatusFallbackFetchPolicy.MaxAttempts), registrations[0].WorstCaseRequestUnitsPerRun)
}

func TestSummarizeBudgetIncludesLiveBatchFallbackInYouTubeScraperFaultEnvelope(t *testing.T) {
	fallbackUnits := float64(30 * scraper.LiveStatusFallbackFetchPolicy.MaxAttempts)
	base := sourceCooldownTestPoller{name: "live"}
	registration := providers.NewChannelPollerRegistration(base, poller.PriorityHigh, time.Minute).
		WithChannelIDs([]string{providers.SyntheticGlobalPollerChannelID}).
		WithWorstCaseRequestUnitsPerRun(fallbackUnits).
		WithBudgetProfile(holodexLiveBatchBudgetProfile(30, poller.BudgetBurstPrimary, poller.BudgetPriorityHigh))

	summary := summarizeYouTubeProducerBudget([]providers.ChannelPollerRegistration{registration})

	require.Zero(t, summary.CombinedRPM)
	require.Equal(t, fallbackUnits, summary.CombinedRetryAmplifiedRPM)
}
