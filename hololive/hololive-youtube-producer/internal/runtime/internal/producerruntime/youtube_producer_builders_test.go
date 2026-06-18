// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package producerruntime

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/config"
	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	settingsmocks "github.com/kapu/hololive-shared/pkg/service/settings/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/kapu/hololive-shared/pkg/testutil"
	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/configupdates"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polling"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/publishedat"
)

type fakeMemberDataProvider struct {
	members []*domain.Member
}

var seedProducerRuntimeExpiredBudgetReservationsLua = valkey.NewLuaScript(`
redis.call('SET', KEYS[1], '2')
redis.call('SET', KEYS[2], '2')
redis.call('ZADD', KEYS[3], ARGV[1], ARGV[2], ARGV[1], ARGV[3])
return 1
`)

type trackingProxyTogglePoller struct {
	mu      sync.Mutex
	enabled bool
}

func (p *trackingProxyTogglePoller) Poll(context.Context, string) error { return nil }
func (p *trackingProxyTogglePoller) Name() string                       { return "tracking_proxy_toggle" }
func (p *trackingProxyTogglePoller) SetProxyEnabled(enabled bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.enabled = enabled
	return true
}
func (p *trackingProxyTogglePoller) ProxyEnabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.enabled
}

func (f *fakeMemberDataProvider) FindMemberByChannelID(channelID string) *domain.Member {
	for _, member := range f.members {
		if member.ChannelID == channelID {
			return member
		}
	}
	return nil
}

func (f *fakeMemberDataProvider) FindMemberByName(name string) *domain.Member {
	for _, member := range f.members {
		if member.Name == name {
			return member
		}
	}
	return nil
}

func (f *fakeMemberDataProvider) FindMemberByAlias(alias string) *domain.Member {
	for _, member := range f.members {
		if member.HasAlias(alias) {
			return member
		}
	}
	return nil
}

func (f *fakeMemberDataProvider) GetChannelIDs() []string {
	ids := make([]string, 0, len(f.members))
	for _, member := range f.members {
		ids = append(ids, member.ChannelID)
	}
	return ids
}

func (f *fakeMemberDataProvider) GetAllMembers() []*domain.Member {
	return f.members
}

func (f *fakeMemberDataProvider) WithContext(context.Context) domain.MemberDataProvider {
	return f
}

func (f *fakeMemberDataProvider) FindMembersByName(name string) []*domain.Member {
	member := f.FindMemberByName(name)
	if member == nil {
		return nil
	}
	return []*domain.Member{member}
}

func (f *fakeMemberDataProvider) FindMembersByAlias(alias string) []*domain.Member {
	member := f.FindMemberByAlias(alias)
	if member == nil {
		return nil
	}
	return []*domain.Member{member}
}

func mustResolveCommunityShortsOperationalChannels(t *testing.T, membersData domain.MemberDataProvider) []communityShortsOperationalChannel {
	t.Helper()

	require.NotNil(t, membersData)
	return communityshorts.BuildOperationalChannelsFromMembers(membersData.GetAllMembers())
}

func TestValidateCommunityShortsOperationalTargets(t *testing.T) {
	t.Parallel()

	t.Run("accepts distinct active channel targets", func(t *testing.T) {
		t.Parallel()

		err := communityshorts.ValidateOperationalTargets(mustResolveCommunityShortsOperationalChannels(t, &fakeMemberDataProvider{
			members: []*domain.Member{
				{Name: "Pekora", Org: "Hololive", ChannelID: "UCpekora"},
				{Name: "Miko", Org: "Hololive", ChannelID: "UCmiko"},
				{Name: "Graduated", Org: "Hololive", ChannelID: "", IsGraduated: true},
			},
		}))
		require.NoError(t, err)
	})

	t.Run("rejects active member without channel id", func(t *testing.T) {
		t.Parallel()

		err := communityshorts.ValidateOperationalTargets(mustResolveCommunityShortsOperationalChannels(t, &fakeMemberDataProvider{
			members: []*domain.Member{
				{Name: "Pekora", Org: "Hololive", ChannelID: ""},
			},
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing operating channel targets")
		assert.Contains(t, err.Error(), "Pekora (Hololive)")
	})

	t.Run("deduplicates shared channel deployment targets", func(t *testing.T) {
		t.Parallel()

		channels := mustResolveCommunityShortsOperationalChannels(t, &fakeMemberDataProvider{
			members: []*domain.Member{
				{Name: "Pekora", Org: "Hololive", ChannelID: "UCdup"},
				{Name: "Miko", Org: "Hololive", ChannelID: "UCdup"},
			},
		})
		require.Len(t, channels, 1)
		assert.Equal(t, "Pekora (Hololive)", channels[0].OwnerLabel)
		assert.Equal(t, "UCdup", channels[0].ChannelID)
		require.NoError(t, communityshorts.ValidateOperationalTargets(channels))
	})
}

func TestResolveCommunityShortsOperationalChannelsFromMembers(t *testing.T) {
	t.Parallel()

	channels := communityshorts.BuildOperationalChannelsFromMembers((&fakeMemberDataProvider{
		members: []*domain.Member{
			{Name: "Pekora", Org: "Hololive", ChannelID: "  UCpekora  "},
			{Name: "Miko", Org: "Hololive", ChannelID: "   "},
			{Name: "Graduated", Org: "Hololive", ChannelID: "UCgraduated", IsGraduated: true},
			nil,
		},
	}).GetAllMembers())
	require.Len(t, channels, 2)
	assert.Equal(t, "Pekora (Hololive)", channels[0].OwnerLabel)
	assert.Equal(t, "UCpekora", channels[0].ChannelID)
	assert.True(t, channels[0].Enabled)
	assert.Equal(t, "Miko (Hololive)", channels[1].OwnerLabel)
	assert.Equal(t, "", channels[1].ChannelID)
	assert.False(t, channels[1].Enabled)
	assert.Equal(t, []string{"UCpekora"}, communityshorts.EnabledChannelIDs(channels))
}

func TestCommunityShortsEnabledChannelIDs_UsesResolverEnablement(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"UCenabled"}, communityshorts.EnabledChannelIDs([]communityShortsOperationalChannel{
		{OwnerLabel: "Enabled", ChannelID: "UCenabled", Enabled: true},
		{OwnerLabel: "Disabled", ChannelID: "UCshadow", Enabled: false},
	}))
}

func newTestYouTubeProducerConfigApplyFn(
	t *testing.T,
	settingsService settings.ReadWriter,
	ytStack *providers.YouTubeStack,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) func(configsub.ConfigUpdate) {
	t.Helper()

	return configsub.NewApplyFn(logger, configsub.ApplyHandlers{
		ScraperProxy: func(payload contractssettings.ScraperProxyPayloadV1) {
			sharedsettings.ApplyScraperProxyToggle(payload.Enabled, ytStack.GetService(), nil, scraperScheduler, logger)
			current := settingsService.Get()
			current.ScraperProxyEnabled = payload.Enabled
			if err := settingsService.Update(current); err != nil && logger != nil {
				logger.Warn("Failed to persist scraper proxy setting", slog.Any("error", err))
			}
		},
	})
}

func schedulerJobCount(t *testing.T, scheduler *poller.Scheduler) int {
	t.Helper()

	require.NotNil(t, scheduler)
	field := reflect.ValueOf(scheduler).Elem().FieldByName("jobMap")
	require.True(t, field.IsValid(), "jobMap field must exist")

	return field.Len()
}

func scraperClientPointerFromPoller(t *testing.T, channelPoller poller.Poller) uintptr {
	t.Helper()

	value := reflect.ValueOf(channelPoller)
	require.True(t, value.IsValid())
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	field := value.FieldByName("client")
	require.True(t, field.IsValid(), "client field must exist")
	require.False(t, field.IsNil(), "client field must not be nil")
	return field.Pointer()
}

func inlinePublishedAtFallbackEnabled(t *testing.T, channelPoller poller.Poller) bool {
	t.Helper()

	value := reflect.ValueOf(channelPoller)
	require.True(t, value.IsValid())
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	field := value.FieldByName("inlinePublishedAtFallbackEnabled")
	require.True(t, field.IsValid(), "inlinePublishedAtFallbackEnabled field must exist")
	return field.Bool()
}

func scraperClientPointerFromResolver(t *testing.T, resolver *poller.PendingPublishedAtResolver) uintptr {
	t.Helper()

	value := reflect.ValueOf(resolver).Elem()
	field := value.FieldByName("client")
	require.True(t, field.IsValid(), "resolver client field must exist")
	require.False(t, field.IsNil(), "resolver client field must not be nil")
	return field.Pointer()
}

func extractResolverDurationField(t *testing.T, resolver *poller.PendingPublishedAtResolver, fieldName string) time.Duration {
	t.Helper()

	value := reflect.ValueOf(resolver).Elem()
	field := value.FieldByName(fieldName)
	require.True(t, field.IsValid(), "%s field must exist", fieldName)
	return time.Duration(field.Int())
}

func extractResolverIntField(t *testing.T, resolver *poller.PendingPublishedAtResolver, fieldName string) int {
	t.Helper()

	value := reflect.ValueOf(resolver).Elem()
	field := value.FieldByName(fieldName)
	require.True(t, field.IsValid(), "%s field must exist", fieldName)
	return int(field.Int())
}

func scraperRateLimiterPointer(t *testing.T, client *scraper.Client) uintptr {
	t.Helper()

	value := reflect.ValueOf(client).Elem()
	field := value.FieldByName("rateLimiter")
	require.True(t, field.IsValid(), "rateLimiter field must exist")
	require.False(t, field.IsNil(), "rateLimiter field must not be nil")
	return field.Pointer()
}

func extractScraperFetcherEngine(t *testing.T, client *scraper.Client) scraper.FetcherEngine {
	t.Helper()

	value := reflect.ValueOf(client).Elem()
	field := value.FieldByName("fetcherEngine")
	require.True(t, field.IsValid(), "fetcherEngine field must exist")
	return scraper.FetcherEngine(field.String())
}

func requireScraperStateStoreConfigured(t *testing.T, client *scraper.Client) {
	t.Helper()

	value := reflect.ValueOf(client).Elem()
	field := value.FieldByName("stateStore")
	require.True(t, field.IsValid(), "stateStore field must exist")
	require.False(t, field.IsNil(), "stateStore field must not be nil")
}

func newTestValkeyClient(t *testing.T) (client valkey.Client, addr string) {
	t.Helper()

	client, mini := testutil.NewTestValkeyClient(t)
	return client, mini.Addr()
}

func TestBuildSharedYouTubeProducerClient_UsesConfiguredFetcherEngine(t *testing.T) {
	t.Parallel()

	client := polling.BuildSharedClient(&config.ScraperConfig{
		FetcherEngine: config.ScraperFetcherEngineGoScrapy,
	}, nil, scraper.NewRateLimiter(time.Second))

	require.NotNil(t, client)
	assert.Equal(t, scraper.FetcherEngineGoScrapy, extractScraperFetcherEngine(t, client))
}

func TestBuildYouTubeProducerChannelPollerRegistrations(t *testing.T) {
	t.Parallel()

	postgres := newPollerRegistrationTestDB(t)
	registrations := polling.BuildRegistrations(
		postgres,
		&config.ScraperConfig{
			ProxyEnabled: true,
			ProxyURL:     "socks5://proxy.internal:1080",
			Poll: config.ScraperPoll{
				Videos:    7 * time.Minute,
				Shorts:    11 * time.Minute,
				Community: 13 * time.Minute,
				Stats:     4 * time.Hour,
				Live:      3 * time.Minute,
			},
		},
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	require.Len(t, registrations, 5)

	expected := []struct {
		name     string
		priority poller.Priority
		interval int64
	}{
		{name: "videos", priority: poller.PriorityNormal, interval: int64(7 * time.Minute)},
		{name: "shorts", priority: poller.PriorityLow, interval: int64(11 * time.Minute)},
		{name: "community", priority: poller.PriorityLow, interval: int64(11 * time.Minute)},
		{name: "channel_stats", priority: poller.PriorityLow, interval: int64(4 * time.Hour)},
		{name: "live", priority: poller.PriorityHigh, interval: int64(3 * time.Minute)},
	}

	for idx, registration := range registrations {
		assert.Equal(t, expected[idx].name, registration.Poller.Name())
		assert.Equal(t, expected[idx].priority, registration.Priority)
		assert.Equal(t, expected[idx].interval, int64(registration.Interval))
	}
}

func TestPendingPublishedAtResolver_UsesSharedScraperClientProxyState(t *testing.T) {
	t.Parallel()

	cacheClient := cachemocks.NewLenientClient()
	sharedRL := scraper.NewRateLimiter(time.Second)
	appConfig := config.ScraperConfig{
		ProxyEnabled:        true,
		ProxyURL:            "socks5://proxy.internal:1080",
		PublishedAtResolver: config.DefaultScraperPublishedAtResolverConfig(),
		Poll: config.ScraperPoll{
			Videos:    7 * time.Minute,
			Shorts:    11 * time.Minute,
			Community: 13 * time.Minute,
			Stats:     4 * time.Hour,
			Live:      3 * time.Minute,
		},
	}
	sharedClient := polling.BuildSharedClient(&appConfig, cacheClient, sharedRL)
	registrations := polling.BuildRegistrationsWithClient(
		newPollerRegistrationTestDB(t),
		&appConfig,
		sharedClient,
		nil,
		nil,
		[]string{"UC_NOTIFY_A"},
		[]string{"UC_STATS_A"},
	)
	resolver := publishedat.BuildPendingResolver(
		&appConfig,
		newPollerRegistrationTestDB(t),
		sharedClient,
		func(poller.NotificationRouteRequest) bool { return false },
		testLogger(),
	)

	require.NotNil(t, sharedClient)
	require.NotNil(t, resolver)
	require.NotEmpty(t, registrations)

	sharedClientPointer := reflect.ValueOf(sharedClient).Pointer()
	pollerClientPointer := scraperClientPointerFromPoller(t, registrations[0].Poller)
	resolverClientPointer := scraperClientPointerFromResolver(t, resolver)

	require.Equal(t, sharedClientPointer, pollerClientPointer)
	require.Equal(t, sharedClientPointer, resolverClientPointer)
	require.Equal(t, reflect.ValueOf(sharedRL).Pointer(), scraperRateLimiterPointer(t, sharedClient))
	requireScraperStateStoreConfigured(t, sharedClient)
	require.Equal(t, 3*time.Minute, extractResolverDurationField(t, resolver, "interval"))
	require.Equal(t, 10, extractResolverIntField(t, resolver, "batchSize"))
	require.Equal(t, 1, extractResolverIntField(t, resolver, "maxResolvePerRun"))
	require.Equal(t, 12*time.Second, extractResolverDurationField(t, resolver, "maxRunDuration"))
	require.Equal(t, 10*time.Second, extractResolverDurationField(t, resolver, "resolveTimeout"))
	require.Equal(t, 30*time.Second, extractResolverDurationField(t, resolver, "minDetectedAge"))
	require.Equal(t, 5*time.Minute, extractResolverDurationField(t, resolver, "failureBackoffTTL"))
	require.True(t, sharedClient.ProxyEnabled())

	require.True(t, sharedClient.SetProxyEnabled(false))
	assert.False(t, sharedClient.ProxyEnabled())

	value := reflect.ValueOf(resolver).Elem()
	softLimiterField := value.FieldByName("softLimiter")
	assert.False(t, softLimiterField.IsValid())
}

func TestBuildYouTubeProducerChannelPollerRegistrationsWithClient_NilRouteDeciderDisablesInlinePublishedAtFallback(t *testing.T) {
	t.Parallel()

	appConfig := config.ScraperConfig{
		PublishedAtResolver: config.DefaultScraperPublishedAtResolverConfig(),
		Poll: config.ScraperPoll{
			Videos:    7 * time.Minute,
			Shorts:    11 * time.Minute,
			Community: 13 * time.Minute,
			Stats:     4 * time.Hour,
			Live:      3 * time.Minute,
		},
	}

	registrations := polling.BuildRegistrationsWithClient(
		newPollerRegistrationTestDB(t),
		&appConfig,
		polling.BuildSharedClient(&appConfig, nil, scraper.NewRateLimiter(time.Second)),
		nil,
		nil,
		[]string{"UC_NOTIFY_A"},
		[]string{"UC_STATS_A"},
	)

	require.Len(t, registrations, 5)
	assert.False(t, inlinePublishedAtFallbackEnabled(t, registrations[1].Poller))
	assert.False(t, inlinePublishedAtFallbackEnabled(t, registrations[2].Poller))
}

func TestBuildYouTubeProducerChannelPollerRegistrationsWithClient_DisabledResolverEnablesInlinePublishedAtFallbackForRoutedPollers(t *testing.T) {
	t.Parallel()

	appConfig := config.ScraperConfig{
		PublishedAtResolver: config.ScraperPublishedAtResolverConfig{
			Enabled: false,
		},
		Poll: config.ScraperPoll{
			Videos:    7 * time.Minute,
			Shorts:    11 * time.Minute,
			Community: 13 * time.Minute,
			Stats:     4 * time.Hour,
			Live:      3 * time.Minute,
		},
	}

	registrations := polling.BuildRegistrationsWithClient(
		newPollerRegistrationTestDB(t),
		&appConfig,
		polling.BuildSharedClient(&appConfig, nil, scraper.NewRateLimiter(time.Second)),
		nil,
		func(poller.NotificationRouteRequest) bool { return true },
		[]string{"UC_NOTIFY_A"},
		[]string{"UC_STATS_A"},
	)

	require.Len(t, registrations, 5)
	assert.True(t, inlinePublishedAtFallbackEnabled(t, registrations[1].Poller))
	assert.True(t, inlinePublishedAtFallbackEnabled(t, registrations[2].Poller))
}

func TestBuildPendingPublishedAtResolver_UsesConfiguredControls(t *testing.T) {
	t.Parallel()

	appConfig := config.ScraperConfig{
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
	}

	resolver := publishedat.BuildPendingResolver(
		&appConfig,
		newPollerRegistrationTestDB(t),
		scraper.NewClient(),
		func(poller.NotificationRouteRequest) bool { return true },
		testLogger(),
	)

	require.NotNil(t, resolver)
	require.Equal(t, 12*time.Second, extractResolverDurationField(t, resolver, "interval"))
	require.Equal(t, 9, extractResolverIntField(t, resolver, "batchSize"))
	require.Equal(t, 3, extractResolverIntField(t, resolver, "maxResolvePerRun"))
	require.Equal(t, 12*time.Second, extractResolverDurationField(t, resolver, "maxRunDuration"))
	require.Equal(t, 10*time.Second, extractResolverDurationField(t, resolver, "resolveTimeout"))
	require.Equal(t, 45*time.Second, extractResolverDurationField(t, resolver, "minDetectedAge"))
	require.Equal(t, 7*time.Minute, extractResolverDurationField(t, resolver, "failureBackoffTTL"))
}

func TestBuildPendingPublishedAtResolver_DisabledReturnsNil(t *testing.T) {
	t.Parallel()

	resolver := publishedat.BuildPendingResolver(
		&config.ScraperConfig{
			PublishedAtResolver: config.ScraperPublishedAtResolverConfig{
				Enabled:  false,
				Interval: 15 * time.Second,
			},
		},
		newPollerRegistrationTestDB(t),
		scraper.NewClient(),
		nil,
		testLogger(),
	)

	require.Nil(t, resolver)
}

func TestBuildPendingPublishedAtResolver_ZeroValueConfigReturnsNil(t *testing.T) {
	t.Parallel()

	resolver := publishedat.BuildPendingResolver(
		&config.ScraperConfig{},
		newPollerRegistrationTestDB(t),
		scraper.NewClient(),
		nil,
		testLogger(),
	)

	require.Nil(t, resolver)
}

func TestBuildPendingPublishedAtResolver_NilRouteDeciderReturnsNil(t *testing.T) {
	t.Parallel()

	resolver := publishedat.BuildPendingResolver(
		&config.ScraperConfig{
			PublishedAtResolver: config.DefaultScraperPublishedAtResolverConfig(),
		},
		newPollerRegistrationTestDB(t),
		scraper.NewClient(),
		nil,
		testLogger(),
	)

	require.Nil(t, resolver)
}

func TestBuildYouTubeProducerYouTubeComponents(t *testing.T) {
	t.Parallel()

	operationalChannels := mustResolveCommunityShortsOperationalChannels(t, &fakeMemberDataProvider{
		members: []*domain.Member{
			{ChannelID: " active-channel ", Name: "active", IsGraduated: false},
			{ChannelID: "graduated-channel", Name: "graduated", IsGraduated: true},
			{ChannelID: "   ", Name: "missing", IsGraduated: false},
		},
	})

	scraperScheduler, registrations, err := polling.BuildComponents(
		&config.ScraperConfig{
			ProxyEnabled: true,
			ProxyURL:     "socks5://proxy.internal:1080",
			WorkerCount:  7,
			Poll: config.ScraperPoll{
				Videos:    5 * time.Minute,
				Shorts:    10 * time.Minute,
				Community: 10 * time.Minute,
				Stats:     6 * time.Hour,
				Live:      5 * time.Minute,
			},
		},
		newPollerRegistrationTestDB(t),
		communityshorts.EnabledChannelIDs(operationalChannels),
		communityshorts.EnabledChannelIDs(operationalChannels),
		polling.BuildSharedClient(
			&config.ScraperConfig{
				ProxyEnabled: true,
				ProxyURL:     "socks5://proxy.internal:1080",
				WorkerCount:  7,
				Poll: config.ScraperPoll{
					Videos:    5 * time.Minute,
					Shorts:    10 * time.Minute,
					Community: 10 * time.Minute,
					Stats:     6 * time.Hour,
					Live:      5 * time.Minute,
				},
			},
			nil,
			nil,
		),
		nil,
		nil,
		nil,
		testLogger(),
	)
	require.NoError(t, err)

	require.NotNil(t, scraperScheduler)
	require.Len(t, registrations, 5)

	// active 멤버 1명 * 기본 poller 5종
	assert.Equal(t, 5, schedulerJobCount(t, scraperScheduler))
	assert.Equal(t, 7, scraperScheduler.WorkerCount())
	assert.Equal(t, 5, scraperScheduler.SetProxyEnabled(false))
}

func TestBuildYouTubeProducerYouTubeComponents_RegistersPublishedAtResolverAsGlobalJob(t *testing.T) {
	t.Parallel()

	resolver := publishedat.BuildPendingResolver(
		&config.ScraperConfig{
			PublishedAtResolver: config.DefaultScraperPublishedAtResolverConfig(),
			Poll: config.ScraperPoll{
				Videos:    5 * time.Minute,
				Shorts:    10 * time.Minute,
				Community: 10 * time.Minute,
				Stats:     6 * time.Hour,
				Live:      5 * time.Minute,
			},
		},
		newPollerRegistrationTestDB(t),
		scraper.NewClient(),
		func(poller.NotificationRouteRequest) bool { return true },
		testLogger(),
	)
	require.NotNil(t, resolver)

	scraperScheduler, registrations, err := polling.BuildComponents(
		&config.ScraperConfig{
			Poll: config.ScraperPoll{
				Videos:    5 * time.Minute,
				Shorts:    10 * time.Minute,
				Community: 10 * time.Minute,
				Stats:     6 * time.Hour,
				Live:      5 * time.Minute,
			},
			PublishedAtResolver: config.DefaultScraperPublishedAtResolverConfig(),
		},
		newPollerRegistrationTestDB(t),
		[]string{"UC_NOTIFY"},
		[]string{"UC_STATS"},
		scraper.NewClient(),
		nil,
		func(poller.NotificationRouteRequest) bool { return true },
		resolver,
		testLogger(),
	)
	require.NoError(t, err)
	require.Len(t, registrations, 6)
	require.Contains(t, schedulerJobKeys(t, scraperScheduler), providers.SyntheticGlobalPollerChannelID+":"+poller.PendingPublishedAtResolverPollerName)
	assert.Equal(t, 6, schedulerJobCount(t, scraperScheduler))

	var resolverRegistration *providers.ChannelPollerRegistration
	for idx := range registrations {
		registration := &registrations[idx]
		if registration.Poller != nil && registration.Poller.Name() == poller.PendingPublishedAtResolverPollerName {
			resolverRegistration = registration
			break
		}
	}
	require.NotNil(t, resolverRegistration)
	assert.Equal(t, providers.ChannelTargetGroupGlobal, resolverRegistration.TargetGroup)
	assert.Equal(t, []string{providers.SyntheticGlobalPollerChannelID}, resolverRegistration.ChannelIDs)
	assert.Equal(t, 1, resolverRegistration.RequestsPerRun)
	assert.Equal(t, scraper.MetadataResolveFetchPolicy.MaxAttempts, resolverRegistration.WorstCaseAttempts)
}

func TestBuildIngestionRuntimeGlobalBudgetWiringPassesCleanupLimit(t *testing.T) {
	ctx := context.Background()
	cacheService := testutil.NewTestCacheService(t, ctx)
	namespace := "budget-cleanup-limit"
	source := poller.BudgetSourceYouTubeScraper
	class := poller.BudgetBurstPrimary
	appConfig := &config.Config{}
	appConfig.Scraper.ActiveActive.Namespace = namespace
	appConfig.Scraper.ActiveActive.InstanceID = "ap-a"
	budgetConfig := config.YouTubeProducerGlobalBudgetConfig{
		Enabled:                   true,
		AcquireTimeout:            time.Second,
		YouTubeScraperMaxInflight: 1,
		BackfillMaxInflight:       1,
		CleanupLimit:              1,
	}
	wiring, err := buildIngestionRuntimeGlobalBudgetWiring(appConfig, &youtubeProducerInfrastructure{cacheService: cacheService}, &budgetConfig, nil, testLogger())
	require.NoError(t, err)
	require.NotNil(t, wiring.Limiter)
	seedProducerRuntimeExpiredBudgetReservations(t, ctx, cacheService, namespace, source, class)

	reservation, decision, err := wiring.Limiter.TryReserve(ctx, &poller.BudgetJob{
		Namespace:  namespace,
		InstanceID: "ap-a",
		PollerName: "videos",
		ChannelID:  "channel-1",
		JobKey:     "cleanup-limit",
	}, poller.BudgetProfile{
		SourceUnits: map[poller.BudgetSource]float64{source: 1},
		BurstClass:  class,
		Priority:    poller.BudgetPriorityNormal,
	}, time.Minute)

	require.NoError(t, err)
	require.Nil(t, reservation)
	require.False(t, decision.Allowed)
	require.Equal(t, "budget_cleanup_incomplete", decision.Reason)
}

func seedProducerRuntimeExpiredBudgetReservations(
	t *testing.T,
	ctx context.Context,
	cacheService *cache.Service,
	namespace string,
	source poller.BudgetSource,
	class poller.BudgetBurstClass,
) {
	t.Helper()
	budgetPrefix := fmt.Sprintf("hololive:%s:youtube-producer:budget:{%s}:", namespace, source)
	expiredAtMS := fmt.Sprintf("%d", time.Now().Add(-time.Second).UnixMilli())
	firstMember := string(class) + "|expired-a"
	secondMember := string(class) + "|expired-b"
	result := seedProducerRuntimeExpiredBudgetReservationsLua.Exec(ctx, cacheService.GetClient(), []string{
		budgetPrefix + string(class) + ":inflight",
		budgetPrefix + "global:inflight",
		budgetPrefix + "reservations",
	}, []string{
		expiredAtMS,
		firstMember,
		secondMember,
	})
	require.NoError(t, result.Error())
}

func TestBuildYouTubeProducerConfigSubscriber_ApplyScraperProxyToggle(t *testing.T) {
	t.Parallel()

	currentSettings := settings.Settings{
		AlarmAdvanceMinutes: 5,
		ScraperProxyEnabled: false,
	}
	updateCalls := 0

	settingsService := &settingsmocks.ReadWriter{
		GetFunc: func() settings.Settings {
			return currentSettings
		},
		UpdateFunc: func(newSettings settings.Settings) error {
			updateCalls++
			currentSettings = newSettings
			return nil
		},
	}
	applyFn := newTestYouTubeProducerConfigApplyFn(
		t,
		settingsService,
		&providers.YouTubeStack{Service: &fakeYouTubeService{}},
		nil,
		testLogger(),
	)

	applyFn(configsub.ConfigUpdate{
		Type:    contractssettings.UpdateTypeScraperProxy,
		Payload: []byte(`{"enabled":true}`),
	})

	assert.Equal(t, 1, updateCalls)
	assert.True(t, currentSettings.ScraperProxyEnabled)
}

func TestBuildYouTubeProducerConfigSubscriber_InvalidAndIgnoredUpdates(t *testing.T) {
	t.Parallel()

	currentSettings := settings.Settings{
		AlarmAdvanceMinutes: 5,
		ScraperProxyEnabled: false,
	}
	updateCalls := 0

	settingsService := &settingsmocks.ReadWriter{
		GetFunc: func() settings.Settings {
			return currentSettings
		},
		UpdateFunc: func(newSettings settings.Settings) error {
			updateCalls++
			currentSettings = newSettings
			return nil
		},
	}
	applyFn := newTestYouTubeProducerConfigApplyFn(
		t,
		settingsService,
		&providers.YouTubeStack{Service: &fakeYouTubeService{}},
		nil,
		testLogger(),
	)

	applyFn(configsub.ConfigUpdate{
		Type:    contractssettings.UpdateTypeScraperProxy,
		Payload: []byte(`{"enabled":`),
	})
	applyFn(configsub.ConfigUpdate{Type: contractssettings.UpdateTypeAlarmAdvanceMinutes})
	applyFn(configsub.ConfigUpdate{Type: "unknown_update_type"})

	assert.Equal(t, 0, updateCalls)
	assert.False(t, currentSettings.ScraperProxyEnabled)
}

func TestBuildYouTubeProducerConfigSubscriber_PublisherRoundTrip(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, addr := newTestValkeyClient(t)
	publisherClient, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{addr},
		DisableCache:      true,
		ForceSingleClient: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { publisherClient.Close() })

	var mu sync.Mutex
	currentSettings := settings.Settings{
		AlarmAdvanceMinutes: 5,
		ScraperProxyEnabled: false,
	}
	updateCalls := 0
	settingsService := &settingsmocks.ReadWriter{
		GetFunc: func() settings.Settings {
			mu.Lock()
			defer mu.Unlock()
			return currentSettings
		},
		UpdateFunc: func(newSettings settings.Settings) error {
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(currentSettings, newSettings) {
				updateCalls++
			}
			currentSettings = newSettings
			return nil
		},
	}
	cacheService := &cachemocks.Client{
		GetClientFunc: func() valkey.Client { return client },
	}
	youtubeService := &fakeYouTubeService{}
	scheduler := poller.NewScheduler(&poller.SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: time.Millisecond,
	})
	trackingPoller := &trackingProxyTogglePoller{}
	scheduler.Register("channel-1", trackingPoller, poller.PriorityNormal, time.Minute)

	subscriber := configupdates.BuildSubscriber(
		cacheService,
		settingsService,
		nil,
		&providers.YouTubeStack{Service: youtubeService},
		scheduler,
		logger,
	)
	require.NotNil(t, subscriber)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		subscriber.Run(ctx)
		close(done)
	}()

	configPublisher := configsub.NewPublisher(publisherClient)
	require.Eventually(t, func() bool {
		if err := configPublisher.PublishScraperProxy(context.Background(), true); err != nil {
			return false
		}

		mu.Lock()
		defer mu.Unlock()
		return currentSettings.ScraperProxyEnabled && youtubeService.ScraperProxyEnabled() && trackingPoller.ProxyEnabled()
	}, 2*time.Second, 50*time.Millisecond)

	mu.Lock()
	assert.Equal(t, 1, updateCalls)
	mu.Unlock()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not stop after cancel")
	}
}

var _ domain.MemberDataProvider = (*fakeMemberDataProvider)(nil)
var _ cache.Client = (*cachemocks.Client)(nil)
