package polling

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/pollers"
	"github.com/stretchr/testify/require"
)

type recordingLiveStatusProvider struct {
	calls [][]string
}

func (p *recordingLiveStatusProvider) GetChannelsLiveStatus(_ context.Context, channelIDs []string) ([]*domain.Stream, error) {
	p.calls = append(p.calls, append([]string(nil), channelIDs...))
	return nil, errors.New("provider unavailable")
}

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

	require.Len(t, registrations, 1)
	require.Equal(t, "live_batch", registrations[0].Poller.Name())
	require.Equal(t, []string{providers.SyntheticGlobalPollerChannelID}, registrations[0].ChannelIDs)
	require.Equal(t, 2.0, registrations[0].BudgetProfile.SourceUnits[polling.BudgetSourceHolodexLive])
	require.Zero(t, registrations[0].BudgetProfile.SourceUnits[polling.BudgetSourceYouTubeScraper])
	require.Equal(t, float64(len(ids)*scraper.LiveStatusFallbackFetchPolicy.MaxAttempts), registrations[0].BudgetProfile.FallbackSourceUnits[polling.BudgetSourceYouTubeScraper])
	require.Equal(t, float64(len(ids)), registrations[0].BudgetProfile.SourceUnits[polling.BudgetSourcePostgresWrite])
	require.Equal(t, float64(len(ids)*scraper.LiveStatusFallbackFetchPolicy.MaxAttempts), registrations[0].WorstCaseRequestUnitsPerRun)
}

func TestLiveBatchPollerChunksSnapshotAtExecution(t *testing.T) {
	provider := &recordingLiveStatusProvider{}
	base := pollers.NewLivePollerWithStatusProvider(provider, nil, nil)
	ids := make([]string, 0, defaultLiveBatchChannelChunkSize+1)
	for i := range defaultLiveBatchChannelChunkSize + 1 {
		ids = append(ids, fmt.Sprintf("UC_TEST_%02d", i))
	}
	poller := newLiveBatchPoller("live_batch", base, ids, polling.BudgetBurstPrimary, polling.BudgetPriorityHigh)

	err := poller.Poll(t.Context(), providers.SyntheticGlobalPollerChannelID)

	require.Error(t, err)
	require.Len(t, provider.calls, 2)
	require.Len(t, provider.calls[0], defaultLiveBatchChannelChunkSize)
	require.Len(t, provider.calls[1], 1)
}

func TestLiveBatchPollerWithChannelTargetsReturnsImmutableSnapshot(t *testing.T) {
	base := pollers.NewLivePollerWithStatusProvider(nil, nil, nil)
	poller := newLiveBatchPoller("live_batch", base, []string{"UC_OLD"}, polling.BudgetBurstPrimary, polling.BudgetPriorityHigh)

	updatedPoller, profile := poller.WithChannelTargets([]string{"UC_NEW", "UC_NEW", " "})
	updated, ok := updatedPoller.(*liveBatchPoller)
	if !ok {
		t.Fatalf("updated poller type = %T, want *liveBatchPoller", updatedPoller)
	}

	require.Equal(t, []string{"UC_OLD"}, poller.ChannelTargets())
	require.Equal(t, []string{"UC_NEW"}, updated.ChannelTargets())
	require.Equal(t, 1.0, profile.SourceUnits[polling.BudgetSourceHolodexLive])
	require.Equal(t, 1.0, profile.SourceUnits[polling.BudgetSourcePostgresWrite])
}

func TestLiveBatchPollerWithChannelTargetsHandlesNilReceiver(t *testing.T) {
	var poller *liveBatchPoller

	updated, profile := poller.WithChannelTargets([]string{"UC_NEW"})

	require.Nil(t, updated)
	require.Empty(t, profile.SourceUnits)
	require.Empty(t, profile.FallbackSourceUnits)
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
