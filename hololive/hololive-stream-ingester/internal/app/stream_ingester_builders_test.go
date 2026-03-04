package app

import (
	"context"
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/config"
	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	settingsmocks "github.com/kapu/hololive-shared/pkg/service/settings/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type fakeMemberDataProvider struct {
	members []*domain.Member
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

func extractSubscriberApplyFn(t *testing.T, subscriber *configsub.Subscriber) func(configsub.ConfigUpdate) {
	t.Helper()

	require.NotNil(t, subscriber)
	field := reflect.ValueOf(subscriber).Elem().FieldByName("applyFn")
	require.True(t, field.IsValid(), "applyFn field must exist")

	field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	applyFn, ok := field.Interface().(func(configsub.ConfigUpdate))
	require.True(t, ok, "applyFn must be func(configsub.ConfigUpdate)")
	return applyFn
}

func schedulerJobCount(t *testing.T, scheduler *poller.Scheduler) int {
	t.Helper()

	require.NotNil(t, scheduler)
	field := reflect.ValueOf(scheduler).Elem().FieldByName("jobMap")
	require.True(t, field.IsValid(), "jobMap field must exist")

	field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	return field.Len()
}

func TestBuildStreamIngesterChannelPollerRegistrations(t *testing.T) {
	t.Parallel()

	postgres := &databasemocks.Client{}
	registrations := buildStreamIngesterChannelPollerRegistrations(
		postgres,
		scraper.ProxyConfig{Enabled: true, URL: "socks5://127.0.0.1:1080"},
		nil,
		nil,
	)

	require.Len(t, registrations, 5)
	intervals := providers.DefaultPollerIntervals()

	expected := []struct {
		name     string
		priority poller.Priority
		interval int64
	}{
		{name: "videos", priority: poller.PriorityNormal, interval: int64(intervals.Videos)},
		{name: "shorts", priority: poller.PriorityLow, interval: int64(intervals.Shorts)},
		{name: "community", priority: poller.PriorityLow, interval: int64(intervals.Community)},
		{name: "channel_stats", priority: poller.PriorityLow, interval: int64(intervals.Stats)},
		{name: "live", priority: poller.PriorityHigh, interval: int64(intervals.Live)},
	}

	for idx, registration := range registrations {
		assert.Equal(t, expected[idx].name, registration.Poller.Name())
		assert.Equal(t, expected[idx].priority, registration.Priority)
		assert.Equal(t, expected[idx].interval, int64(registration.Interval))
	}
}

func TestBuildStreamIngesterYouTubeComponents(t *testing.T) {
	t.Parallel()

	membersData := &fakeMemberDataProvider{
		members: []*domain.Member{
			{ChannelID: "active-channel", Name: "active", IsGraduated: false},
			{ChannelID: "graduated-channel", Name: "graduated", IsGraduated: true},
		},
	}

	scraperScheduler, outboxDispatcher := buildStreamIngesterYouTubeComponents(
		config.ScraperConfig{ProxyEnabled: true, ProxyURL: "socks5://127.0.0.1:1080"},
		&databasemocks.Client{},
		membersData,
		nil,
		nil,
		nil,
		nil,
		testLogger(),
	)

	require.NotNil(t, scraperScheduler)
	require.NotNil(t, outboxDispatcher)

	// active 멤버 1명 * 기본 poller 5종
	assert.Equal(t, 5, schedulerJobCount(t, scraperScheduler))
	assert.Equal(t, 5, scraperScheduler.SetProxyEnabled(false))
}

func TestBuildStreamIngesterConfigSubscriber_ApplyScraperProxyToggle(t *testing.T) {
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
	cacheService := &cachemocks.Client{
		GetClientFunc: func() valkey.Client { return nil },
	}

	subscriber := buildStreamIngesterConfigSubscriber(
		cacheService,
		settingsService,
		nil,
		nil,
		nil,
		testLogger(),
	)
	applyFn := extractSubscriberApplyFn(t, subscriber)

	applyFn(configsub.ConfigUpdate{
		Type:    contractssettings.UpdateTypeScraperProxy,
		Payload: []byte(`{"enabled":true}`),
	})

	assert.Equal(t, 1, updateCalls)
	assert.True(t, currentSettings.ScraperProxyEnabled)
}

func TestBuildStreamIngesterConfigSubscriber_InvalidAndIgnoredUpdates(t *testing.T) {
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
	cacheService := &cachemocks.Client{
		GetClientFunc: func() valkey.Client { return nil },
	}

	applyFn := extractSubscriberApplyFn(t, buildStreamIngesterConfigSubscriber(
		cacheService,
		settingsService,
		nil,
		nil,
		nil,
		testLogger(),
	))

	applyFn(configsub.ConfigUpdate{
		Type:    contractssettings.UpdateTypeScraperProxy,
		Payload: []byte(`{"enabled":`),
	})
	applyFn(configsub.ConfigUpdate{Type: contractssettings.UpdateTypeAlarmAdvanceMinutes})
	applyFn(configsub.ConfigUpdate{Type: "unknown_update_type"})

	assert.Equal(t, 0, updateCalls)
	assert.False(t, currentSettings.ScraperProxyEnabled)
}

var _ domain.MemberDataProvider = (*fakeMemberDataProvider)(nil)
var _ cache.Client = (*cachemocks.Client)(nil)
