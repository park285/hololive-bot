package polling

import (
	"context"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type backfillTestPoller struct {
	name         string
	polled       int
	proxyEnabled bool
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
	postgres := &databasemocks.Client{GetGormDBFunc: func() *gorm.DB { return nil }}
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
		nil,
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

func manyChannelIDs(count int) []string {
	ids := make([]string, 0, count)
	for i := 0; i < count; i++ {
		ids = append(ids, "UC_BACKFILL_"+time.Unix(int64(i), 0).Format("150405"))
	}
	return ids
}
