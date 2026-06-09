package polling

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polltarget"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/publishedat"
	"github.com/stretchr/testify/require"
)

type backfillTestPoller struct {
	name         string
	polled       int
	proxyEnabled bool
}

type registrationTestLiveStatusProvider struct{}

func (registrationTestLiveStatusProvider) GetChannelsLiveStatus(context.Context, []string) ([]*domain.Stream, error) {
	return nil, nil
}

func (p *backfillTestPoller) Poll(context.Context, string) error {
	p.polled++
	return nil
}

func (p *backfillTestPoller) Name() string {
	return p.name
}

func (p *backfillTestPoller) SetProxyEnabled(enabled bool) bool {
	changed := p.proxyEnabled != enabled
	p.proxyEnabled = enabled
	return changed
}

func (p *backfillTestPoller) ProxyEnabled() bool {
	return p.proxyEnabled
}

func TestNamedBackfillPollerDelegatesPollAndProxyToggle(t *testing.T) {
	base := &backfillTestPoller{name: "shorts"}
	wrapped := newNamedBackfillPoller("shorts_backfill", base)

	require.Equal(t, "shorts_backfill", wrapped.Name())
	require.NoError(t, wrapped.Poll(context.Background(), "UC_A"))
	require.Equal(t, 1, base.polled)

	proxyToggle, ok := wrapped.(interface {
		SetProxyEnabled(bool) bool
		ProxyEnabled() bool
	})
	require.True(t, ok)
	require.True(t, proxyToggle.SetProxyEnabled(true))
	require.True(t, base.proxyEnabled)
	require.True(t, proxyToggle.ProxyEnabled())
}

func TestBuildRegistrationsAddsEnabledBackfillPollers(t *testing.T) {
	registrations := buildBackfillTestRegistrations(config.ScraperBackfillConfig{
		Enabled:           true,
		ShortsEnabled:     true,
		ShortsInterval:    5 * time.Minute,
		CommunityEnabled:  true,
		CommunityInterval: 10 * time.Minute,
		LiveEnabled:       true,
		LiveInterval:      3 * time.Minute,
		TargetGroup:       "notification",
	}, []string{"UC_A", "UC_B"})

	assertBackfillRegistration(t, registrations, "shorts_backfill", 5*time.Minute)
	assertBackfillRegistration(t, registrations, "community_backfill", 10*time.Minute)
	assertBackfillRegistration(t, registrations, "live_backfill", 3*time.Minute)
}

func TestBuildRegistrationsLeavesOutputUnchangedWhenBackfillDisabled(t *testing.T) {
	base := buildBackfillTestRegistrations(config.ScraperBackfillConfig{}, []string{"UC_A"})
	withDisabled := buildBackfillTestRegistrations(config.ScraperBackfillConfig{
		Enabled:           false,
		ShortsEnabled:     true,
		ShortsInterval:    5 * time.Minute,
		CommunityEnabled:  true,
		CommunityInterval: 10 * time.Minute,
		LiveEnabled:       true,
		LiveInterval:      3 * time.Minute,
		TargetGroup:       "notification",
	}, []string{"UC_A"})

	require.Len(t, withDisabled, len(base))
	for _, registration := range withDisabled {
		require.NotContains(t, registration.Poller.Name(), "_backfill")
	}
}

func TestBudgetIncludesBackfillRegistrations(t *testing.T) {
	base := buildBackfillTestRegistrations(config.ScraperBackfillConfig{}, []string{"UC_A"})
	withBackfill := buildBackfillTestRegistrations(config.ScraperBackfillConfig{
		Enabled:           true,
		ShortsEnabled:     true,
		ShortsInterval:    5 * time.Minute,
		CommunityEnabled:  true,
		CommunityInterval: 10 * time.Minute,
		LiveEnabled:       true,
		LiveInterval:      3 * time.Minute,
		TargetGroup:       "notification",
	}, []string{"UC_A"})

	require.Greater(t, summarizeYouTubeProducerBudget(withBackfill).PollerRPM, summarizeYouTubeProducerBudget(base).PollerRPM)
}

func TestFlatAndBackfillRegistrationsHaveBudgetProfiles(t *testing.T) {
	registrations := buildBackfillTestRegistrations(config.ScraperBackfillConfig{
		Enabled:           true,
		ShortsEnabled:     true,
		ShortsInterval:    5 * time.Minute,
		CommunityEnabled:  true,
		CommunityInterval: 10 * time.Minute,
		LiveEnabled:       true,
		LiveInterval:      3 * time.Minute,
		TargetGroup:       "notification",
	}, []string{"UC_A", "UC_B"})

	assertAllBudgetProfiles(t, registrations)
}

func TestTieredAndBackfillRegistrationsHaveBudgetProfiles(t *testing.T) {
	pollers := newYouTubeProducerPollerSet(nil, nil, nil, defaultChannelPollerMaxResults, []string{}, nil, false)
	poll := config.ScraperPoll{
		Videos:    15 * time.Minute,
		Shorts:    6 * time.Minute,
		Community: 15 * time.Minute,
		Stats:     6 * time.Hour,
		Live:      2 * time.Minute,
	}
	registrations := buildTieredYouTubeProducerChannelPollerRegistrations(
		pollers,
		poll,
		polltarget.TieredTargets{
			NotificationChannelIDs:       []string{"UC_A", "UC_B", "UC_C"},
			ActiveNotificationChannelIDs: []string{"UC_A"},
			WarmNotificationChannelIDs:   []string{"UC_B"},
			ColdNotificationChannelIDs:   []string{"UC_C"},
			StatsChannelIDs:              []string{"UC_STATS"},
		},
		false,
		defaultChannelPollerMaxResults,
	)
	registrations = appendBackfillChannelPollerRegistrations(registrations, pollers, config.ScraperBackfillConfig{
		Enabled:           true,
		ShortsEnabled:     true,
		ShortsInterval:    5 * time.Minute,
		CommunityEnabled:  true,
		CommunityInterval: 10 * time.Minute,
		LiveEnabled:       true,
		LiveInterval:      3 * time.Minute,
		TargetGroup:       "notification",
	}, []string{"UC_A", "UC_B", "UC_C"}, false, defaultChannelPollerMaxResults)

	assertAllBudgetProfiles(t, registrations)
}

func TestTieredRegistrationBudgetPriorityMatrixSpotChecks(t *testing.T) {
	pollers := newYouTubeProducerPollerSet(nil, nil, nil, defaultChannelPollerMaxResults, []string{}, nil, false)
	videosName := pollers.videos.Name()
	shortsName := pollers.shorts.Name()
	poll := config.ScraperPoll{
		Videos:    15 * time.Minute,
		Shorts:    6 * time.Minute,
		Community: 15 * time.Minute,
		Stats:     6 * time.Hour,
		Live:      2 * time.Minute,
	}
	registrations := buildTieredYouTubeProducerChannelPollerRegistrations(
		pollers,
		poll,
		polltarget.TieredTargets{
			NotificationChannelIDs:       []string{"UC_A", "UC_B", "UC_C"},
			ActiveNotificationChannelIDs: []string{"UC_A"},
			WarmNotificationChannelIDs:   []string{"UC_B"},
			ColdNotificationChannelIDs:   []string{"UC_C"},
			StatsChannelIDs:              []string{"UC_STATS"},
		},
		false,
		defaultChannelPollerMaxResults,
	)

	videosCold := requireRegistrationForTargetGroup(t, registrations, videosName, providers.ChannelTargetGroupCold)
	require.Equal(t, poller.BudgetPriorityNormal, videosCold.BudgetProfile.Priority)

	shortsCold := requireRegistrationForTargetGroup(t, registrations, shortsName, providers.ChannelTargetGroupCold)
	require.Equal(t, poller.BudgetPriorityLow, shortsCold.BudgetProfile.Priority)
}

func TestPrimaryCommunityRegistrationUsesShortsInterval(t *testing.T) {
	registrations := buildBackfillTestRegistrations(config.ScraperBackfillConfig{}, []string{"UC_A"})

	shorts := requireRegistration(t, registrations, "shorts")
	community := requireRegistration(t, registrations, "community")

	require.Equal(t, shorts.Interval, community.Interval)
	require.Equal(t, 6*time.Minute, community.Interval)
}

func TestTieredCommunityRegistrationsUseShortsInterval(t *testing.T) {
	pollers := newYouTubeProducerPollerSet(nil, nil, nil, defaultChannelPollerMaxResults, []string{}, nil, false)
	poll := config.ScraperPoll{
		Videos:    15 * time.Minute,
		Shorts:    6 * time.Minute,
		Community: 15 * time.Minute,
		Stats:     6 * time.Hour,
		Live:      2 * time.Minute,
	}
	registrations := buildTieredYouTubeProducerChannelPollerRegistrations(
		pollers,
		poll,
		polltarget.TieredTargets{
			NotificationChannelIDs:       []string{"UC_A", "UC_B", "UC_C"},
			ActiveNotificationChannelIDs: []string{"UC_A"},
			WarmNotificationChannelIDs:   []string{"UC_B"},
			ColdNotificationChannelIDs:   []string{"UC_C"},
			StatsChannelIDs:              []string{"UC_STATS"},
		},
		false,
		defaultChannelPollerMaxResults,
	)

	shortsName := pollers.shorts.Name()
	communityName := pollers.community.Name()
	for _, targetGroup := range []providers.ChannelTargetGroup{
		providers.ChannelTargetGroupActive,
		providers.ChannelTargetGroupWarm,
		providers.ChannelTargetGroupCold,
	} {
		shorts := requireRegistrationForTargetGroup(t, registrations, shortsName, targetGroup)
		community := requireRegistrationForTargetGroup(t, registrations, communityName, targetGroup)
		require.Equal(t, shorts.Interval, community.Interval)
	}
}

func TestRegistrationBudgetProfileMatrixSpotChecks(t *testing.T) {
	registrations := buildBackfillTestRegistrationsWithLiveBatch(config.ScraperBackfillConfig{
		Enabled:           true,
		ShortsEnabled:     true,
		ShortsInterval:    5 * time.Minute,
		CommunityEnabled:  true,
		CommunityInterval: 10 * time.Minute,
		LiveEnabled:       true,
		LiveInterval:      3 * time.Minute,
		TargetGroup:       "notification",
	}, []string{"UC_A", "UC_B"})

	live := requireRegistration(t, registrations, "live_batch")
	require.Equal(t, float64(1), live.BudgetProfile.SourceUnits[poller.BudgetSourceHolodexLive])
	require.Equal(t, float64(2), live.BudgetProfile.SourceUnits[poller.BudgetSourcePostgresWrite])
	require.Equal(t, poller.BudgetBurstPrimary, live.BudgetProfile.BurstClass)
	require.Equal(t, poller.BudgetPriorityHigh, live.BudgetProfile.Priority)

	videos := requireRegistration(t, registrations, "videos")
	require.Greater(t, videos.BudgetProfile.SourceUnits[poller.BudgetSourceYouTubeScraper], float64(0))
	require.Equal(t, float64(1), videos.BudgetProfile.SourceUnits[poller.BudgetSourcePostgresWrite])
	require.Equal(t, poller.BudgetBurstPrimary, videos.BudgetProfile.BurstClass)
	require.Equal(t, poller.BudgetPriorityNormal, videos.BudgetProfile.Priority)

	liveBackfill := requireRegistration(t, registrations, "live_backfill_batch")
	require.Equal(t, poller.BudgetBurstBackfill, liveBackfill.BudgetProfile.BurstClass)
	require.Equal(t, poller.BudgetPriorityLow, liveBackfill.BudgetProfile.Priority)

	stats := requireRegistration(t, registrations, "channel_stats")
	require.Equal(t, 2*float64(scraper.FetchPageMaxAttempts), stats.BudgetProfile.SourceUnits[poller.BudgetSourceYouTubeScraper])
	require.Equal(t, float64(1), stats.BudgetProfile.SourceUnits[poller.BudgetSourcePostgresWrite])
	require.Equal(t, poller.BudgetBurstPrimary, stats.BudgetProfile.BurstClass)
	require.Equal(t, poller.BudgetPriorityLow, stats.BudgetProfile.Priority)
}

func TestPublishedAtResolverRegistrationHasBudgetProfile(t *testing.T) {
	resolver := poller.NewPendingPublishedAtResolverWithControls(
		nil,
		nil,
		nil,
		20*time.Second,
		10,
		7,
		time.Second,
		time.Second,
		time.Second,
		time.Minute,
		nil,
	)
	registration := publishedat.BuildRegistration(resolver, config.ScraperConfig{
		PublishedAtResolver: config.ScraperPublishedAtResolverConfig{
			Enabled:          true,
			Interval:         20 * time.Second,
			MaxResolvePerRun: 7,
		},
	}, nil)

	require.NotNil(t, registration)
	require.True(t, registration.HasBudgetProfile)
	require.Equal(t, float64(7*scraper.MetadataResolveFetchPolicy.MaxAttempts), registration.BudgetProfile.SourceUnits[poller.BudgetSourceYouTubeScraper])
	require.Equal(t, float64(1), registration.BudgetProfile.SourceUnits[poller.BudgetSourcePostgresWrite])
	require.Equal(t, poller.BudgetBurstPrimary, registration.BudgetProfile.BurstClass)
	require.Equal(t, poller.BudgetPriorityLow, registration.BudgetProfile.Priority)
}

func TestValidateRegistrationBudgetProfilesRequiresExplicitProfiles(t *testing.T) {
	registrations := []providers.ChannelPollerRegistration{
		providers.NewChannelPollerRegistration(&backfillTestPoller{name: "missing_budget"}, poller.PriorityNormal, time.Minute).
			WithChannelIDs([]string{"UC_A"}),
		providers.NewChannelPollerRegistration(&backfillTestPoller{name: "empty_budget"}, poller.PriorityNormal, time.Minute).
			WithChannelIDs([]string{"UC_B"}).
			WithBudgetProfile(poller.BudgetProfile{
				SourceUnits: map[poller.BudgetSource]float64{},
				BurstClass:  poller.BudgetBurstPrimary,
				Priority:    poller.BudgetPriorityNormal,
			}),
		providers.NewChannelPollerRegistration(&backfillTestPoller{name: "satisfied_budget"}, poller.PriorityNormal, time.Minute).
			WithChannelIDs([]string{"UC_C"}).
			WithBudgetProfile(poller.BudgetProfile{
				SourceUnits: map[poller.BudgetSource]float64{poller.BudgetSourceYouTubeScraper: 1},
				BurstClass:  poller.BudgetBurstPrimary,
				Priority:    poller.BudgetPriorityNormal,
			}),
	}

	err := validateRegistrationBudgetProfiles(registrations)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing_budget")
	require.Contains(t, err.Error(), "empty_budget")

	require.NoError(t, validateRegistrationBudgetProfiles(registrations[2:]))
}

func TestEstimateYouTubeProducerSourceBudgetKeepsSustainedIndependentOfAPCount(t *testing.T) {
	registrations := buildBackfillTestRegistrationsWithLiveBatch(config.ScraperBackfillConfig{
		Enabled:           true,
		ShortsEnabled:     true,
		ShortsInterval:    5 * time.Minute,
		CommunityEnabled:  true,
		CommunityInterval: 10 * time.Minute,
		LiveEnabled:       true,
		LiveInterval:      3 * time.Minute,
		TargetGroup:       "notification",
	}, []string{"UC_A", "UC_B"})

	twoAPs := estimateYouTubeProducerSourceBudget(registrations, 2, 2)
	threeAPs := estimateYouTubeProducerSourceBudget(registrations, 3, 2)

	require.Equal(t, twoAPs.SustainedRPMBySource, threeAPs.SustainedRPMBySource)
	require.Greater(t, twoAPs.SustainedRPMBySource[poller.BudgetSourceYouTubeScraper], float64(0))
	require.Greater(t, twoAPs.SustainedRPMBySource[poller.BudgetSourceHolodexLive], float64(0))
	for source := range twoAPs.BurstInflightBySource {
		require.Equal(t, 4, twoAPs.BurstInflightBySource[source])
		require.Equal(t, 6, threeAPs.BurstInflightBySource[source])
	}
}

func TestResolveYouTubeProducerActiveAPCount(t *testing.T) {
	require.Equal(t, 4, resolveYouTubeProducerActiveAPCount(4, false))
	require.Equal(t, 3, resolveYouTubeProducerActiveAPCount(0, true))
	require.Equal(t, 1, resolveYouTubeProducerActiveAPCount(0, false))
}

func TestBudgetRejectsAggressiveBackfillInterval(t *testing.T) {
	registrations := buildBackfillTestRegistrations(config.ScraperBackfillConfig{
		Enabled:           true,
		ShortsEnabled:     true,
		ShortsInterval:    time.Second,
		CommunityEnabled:  false,
		CommunityInterval: 10 * time.Minute,
		LiveEnabled:       false,
		LiveInterval:      3 * time.Minute,
		TargetGroup:       "notification",
	}, manyChannelIDs(120))

	summary := summarizeYouTubeProducerBudget(registrations)
	require.Error(t, validateYouTubeProducerPollerBudget(summary))
}

func buildBackfillTestRegistrations(backfill config.ScraperBackfillConfig, notificationChannelIDs []string) []providers.ChannelPollerRegistration {
	return buildBackfillTestRegistrationsWithLiveStatusProvider(backfill, notificationChannelIDs, nil)
}

func buildBackfillTestRegistrationsWithLiveBatch(backfill config.ScraperBackfillConfig, notificationChannelIDs []string) []providers.ChannelPollerRegistration {
	return buildBackfillTestRegistrationsWithLiveStatusProvider(backfill, notificationChannelIDs, registrationTestLiveStatusProvider{})
}

func buildBackfillTestRegistrationsWithLiveStatusProvider(backfill config.ScraperBackfillConfig, notificationChannelIDs []string, liveStatusProvider poller.LiveStatusProvider) []providers.ChannelPollerRegistration {
	postgres := &databasemocks.Client{}
	return buildYouTubeProducerChannelPollerRegistrationsWithClient(
		postgres,
		config.ScraperConfig{
			Poll: config.ScraperPoll{
				Videos:    15 * time.Minute,
				Shorts:    6 * time.Minute,
				Community: 15 * time.Minute,
				Stats:     6 * time.Hour,
				Live:      2 * time.Minute,
			},
			Backfill: backfill,
		},
		nil,
		liveStatusProvider,
		nil,
		notificationChannelIDs,
		[]string{"UC_STATS"},
	)
}

func assertBackfillRegistration(t *testing.T, registrations []providers.ChannelPollerRegistration, name string, interval time.Duration) {
	t.Helper()
	for _, registration := range registrations {
		if registration.Poller == nil || registration.Poller.Name() != name {
			continue
		}
		require.Equal(t, poller.PriorityLow, registration.Priority)
		require.Equal(t, interval, registration.Interval)
		require.Equal(t, providers.ChannelTargetGroupNotification, registration.TargetGroup)
		require.Equal(t, []string{"UC_A", "UC_B"}, registration.ChannelIDs)
		require.True(t, registration.HasExplicitChannelIDs)
		return
	}
	t.Fatalf("missing backfill registration %s", name)
}

func assertAllBudgetProfiles(t *testing.T, registrations []providers.ChannelPollerRegistration) {
	t.Helper()
	for _, registration := range registrations {
		require.NotNil(t, registration.Poller)
		require.True(t, registration.HasBudgetProfile, registration.Poller.Name())
		require.NotEmpty(t, registration.BudgetProfile.SourceUnits, registration.Poller.Name())
	}
}

func requireRegistration(t *testing.T, registrations []providers.ChannelPollerRegistration, name string) providers.ChannelPollerRegistration {
	t.Helper()
	for _, registration := range registrations {
		if registration.Poller != nil && registration.Poller.Name() == name {
			return registration
		}
	}
	t.Fatalf("missing registration %s", name)
	return providers.ChannelPollerRegistration{}
}

func requireRegistrationForTargetGroup(t *testing.T, registrations []providers.ChannelPollerRegistration, name string, targetGroup providers.ChannelTargetGroup) providers.ChannelPollerRegistration {
	t.Helper()
	for _, registration := range registrations {
		if registration.Poller != nil && registration.Poller.Name() == name && registration.TargetGroup == targetGroup {
			return registration
		}
	}
	t.Fatalf("missing registration %s for target group %s", name, targetGroup)
	return providers.ChannelPollerRegistration{}
}

func manyChannelIDs(count int) []string {
	ids := make([]string, 0, count)
	for i := range count {
		ids = append(ids, "UC_BACKFILL_"+time.Unix(int64(i), 0).Format("150405"))
	}
	return ids
}

func TestSummarizeBudgetFaultEnvelopeComparesAgainstFleetBudget(t *testing.T) {
	channelIDs := make([]string, 15)
	for i := range channelIDs {
		channelIDs[i] = fmt.Sprintf("UC_%02d", i)
	}
	registration := providers.NewChannelPollerRegistration(
		sourceCooldownTestPoller{name: "videos"},
		poller.PriorityNormal,
		time.Minute,
	).
		WithChannelIDs(channelIDs).
		WithBudgetProfile(youtubeScraperBudgetProfile(1, poller.BudgetBurstPrimary, poller.BudgetPriorityNormal))

	registrations := []providers.ChannelPollerRegistration{registration}

	singleAP := summarizeYouTubeProducerBudgetForFleet(registrations, 30, 1)
	require.Equal(t, 30.0, singleAP.FleetBudgetRPM)
	require.Equal(t, 1, singleAP.ActiveAPCount)

	tripleAP := summarizeYouTubeProducerBudgetForFleet(registrations, 30, 3)
	require.Equal(t, 90.0, tripleAP.FleetBudgetRPM)
	require.Equal(t, 3, tripleAP.ActiveAPCount)
	require.Equal(t, singleAP.CombinedRetryAmplifiedRPM, tripleAP.CombinedRetryAmplifiedRPM,
		"수요 추정은 fleet-aggregate라 AP 수와 무관해야 한다")

	if singleAP.CombinedRetryAmplifiedRPM <= 30 || singleAP.CombinedRetryAmplifiedRPM > 90 {
		t.Fatalf("fixture must amplify between per-AP and fleet budget, got %.2f", singleAP.CombinedRetryAmplifiedRPM)
	}
	require.True(t, singleAP.faultEnvelopeExceedsFleetBudget(),
		"단일 AP fleet에서는 envelope 초과 경고가 유지되어야 한다")
	require.False(t, tripleAP.faultEnvelopeExceedsFleetBudget(),
		"3-AP fleet 용량(90 RPM) 안의 envelope는 경고 대상이 아니다")
}
