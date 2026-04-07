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

package app

import (
	"context"
	"io"
	"log/slog"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/alicebob/miniredis/v2"
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

func newTestValkeyClient(t *testing.T) (valkey.Client, string) {
	t.Helper()

	mini := miniredis.RunT(t)
	host, portStr, err := net.SplitHostPort(mini.Addr())
	require.NoError(t, err)
	addr := net.JoinHostPort(host, portStr)

	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{addr},
		DisableCache:      true,
		ForceSingleClient: true,
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		client.Close()
		mini.Close()
	})

	return client, addr
}

func TestBuildStreamIngesterChannelPollerRegistrations(t *testing.T) {
	t.Parallel()

	postgres := &databasemocks.Client{}
	registrations := buildStreamIngesterChannelPollerRegistrations(
		postgres,
		scraper.ProxyConfig{Enabled: true, URL: "socks5://proxy.internal:1080"},
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
		config.ScraperConfig{ProxyEnabled: true, ProxyURL: "socks5://proxy.internal:1080"},
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

func TestBuildStreamIngesterConfigSubscriber_PublisherRoundTrip(t *testing.T) {
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
			if currentSettings != newSettings {
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
	scheduler := poller.NewScheduler(poller.SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: time.Millisecond,
	})
	trackingPoller := &trackingProxyTogglePoller{}
	scheduler.Register("channel-1", trackingPoller, poller.PriorityNormal, time.Minute)

	subscriber := buildStreamIngesterConfigSubscriber(
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
