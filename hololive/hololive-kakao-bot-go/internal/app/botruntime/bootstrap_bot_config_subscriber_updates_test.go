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

package botruntime

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/testutil"
	json "github.com/park285/shared-go/pkg/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
	"github.com/kapu/hololive-shared/pkg/service/notification"
)

type trackingSettingsReadWriter struct {
	mu          sync.Mutex
	current     settings.Settings
	updateErr   error
	updateCalls int
}

func (s *trackingSettingsReadWriter) Get() settings.Settings {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.current
}

func (s *trackingSettingsReadWriter) Update(newSettings settings.Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.updateCalls++
	if s.updateErr != nil {
		return s.updateErr
	}

	s.current = newSettings

	return nil
}

func (s *trackingSettingsReadWriter) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.updateCalls
}

type trackingAlarmAdvanceCRUD struct {
	testAlarmCRUD

	mu          sync.Mutex
	lastMinutes int
	calls       int
	targets     []int
}

func (a *trackingAlarmAdvanceCRUD) UpdateAlarmAdvanceMinutes(_ context.Context, minutes int) []int {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.calls++

	a.lastMinutes = minutes
	if len(a.targets) > 0 {
		return append([]int(nil), a.targets...)
	}

	return []int{minutes}
}

func (a *trackingAlarmAdvanceCRUD) callSnapshot() (calls, lastMinutes int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.calls, a.lastMinutes
}

func newTestValkeyClient(t *testing.T) (valkey.Client, string) {
	t.Helper()

	client, mini := testutil.NewTestValkeyClient(t)
	return client, mini.Addr()
}

func publishConfigUpdate(t *testing.T, client valkey.Client, updateType string, payload any) {
	t.Helper()

	rawPayload, err := json.Marshal(payload)
	require.NoError(t, err)

	update := configsub.ConfigUpdate{
		Type:    updateType,
		Payload: rawPayload,
	}
	rawUpdate, err := json.Marshal(update)
	require.NoError(t, err)

	cmd := client.B().Publish().Channel(configsub.DefaultChannel).Message(string(rawUpdate)).Build()
	require.NoError(t, client.Do(t.Context(), cmd).Error())
}

func TestBuildBotConfigSubscriber_ScraperProxyUpdate(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	client, addr := newTestValkeyClient(t)
	publisher, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{addr},
		DisableCache:      true,
		ForceSingleClient: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { publisher.Close() })

	cache := &cachemocks.Client{
		GetClientFunc: func() valkey.Client { return client },
	}
	settingsService := &trackingSettingsReadWriter{
		current: settings.Settings{
			AlarmAdvanceMinutes: 5,
			ScraperProxyEnabled: false,
		},
	}
	youtubeService := &trackingYouTubeService{}
	scheduler := poller.NewScheduler(poller.SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: time.Millisecond,
	})
	trackingPoller := &trackingProxyTogglePoller{}
	scheduler.Register("channel-1", trackingPoller, poller.PriorityNormal, time.Minute)

	deps := botConfigSubscriberDependencies{
		Cache:    cache,
		Settings: settingsService,
	}
	runtimeDeps := botConfigSubscriberRuntimeDependencies{
		YouTubeService: youtubeService,
	}
	subscriber := appbootstrap.BuildBotConfigSubscriber(t.Context(), deps, runtimeDeps, scheduler, logger)
	require.NotNil(t, subscriber)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan struct{})

	go func() {
		subscriber.Run(ctx)
		close(done)
	}()

	// subscriber가 subscribe 핸드셰이크를 완료할 때까지 반복 publish (비결정적 sleep 제거)
	require.Eventually(t, func() bool {
		publishConfigUpdate(t, publisher, contractssettings.UpdateTypeScraperProxy, contractssettings.ScraperProxyPayloadV1{Enabled: true})

		got := settingsService.Get()

		return got.ScraperProxyEnabled && youtubeService.isProxyEnabled() && trackingPoller.isEnabled()
	}, 2*time.Second, 50*time.Millisecond)

	assert.GreaterOrEqual(t, settingsService.calls(), 1)

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not stop after cancel")
	}
}

func TestBuildBotConfigSubscriber_AlarmAdvanceMinutesUpdate(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	client, addr := newTestValkeyClient(t)
	publisher, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{addr},
		DisableCache:      true,
		ForceSingleClient: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { publisher.Close() })

	cache := &cachemocks.Client{
		GetClientFunc: func() valkey.Client { return client },
	}
	settingsService := &trackingSettingsReadWriter{
		current: settings.Settings{
			AlarmAdvanceMinutes: 5,
			ScraperProxyEnabled: false,
		},
		updateErr: errors.New("persist failed"),
	}
	alarmService := &trackingAlarmAdvanceCRUD{targets: []int{15, 30}}

	deps := botConfigSubscriberDependencies{
		Cache:    cache,
		Settings: settingsService,
	}
	runtimeDeps := botConfigSubscriberRuntimeDependencies{
		AlarmCRUD: alarmService,
	}
	subscriber := appbootstrap.BuildBotConfigSubscriber(t.Context(), deps, runtimeDeps, nil, logger)
	require.NotNil(t, subscriber)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan struct{})

	go func() {
		subscriber.Run(ctx)
		close(done)
	}()

	// subscriber가 subscribe 핸드셰이크를 완료할 때까지 반복 publish (비결정적 sleep 제거)
	require.Eventually(t, func() bool {
		publishConfigUpdate(t, publisher, contractssettings.UpdateTypeAlarmAdvanceMinutes, contractssettings.AlarmAdvanceMinutesPayloadV1{Minutes: 30})

		calls, last := alarmService.callSnapshot()

		return calls >= 1 && last == 30
	}, 2*time.Second, 50*time.Millisecond)

	require.Eventually(t, func() bool {
		return settingsService.calls() >= 1
	}, 2*time.Second, 50*time.Millisecond)
	assert.Equal(t, 5, settingsService.Get().AlarmAdvanceMinutes)

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not stop after cancel")
	}
}

func TestBuildBotConfigSubscriber_AlarmAdvanceMinutesUpdate_UpdatesAlarmServiceTargets(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	client, addr := newTestValkeyClient(t)
	publisher, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{addr},
		DisableCache:      true,
		ForceSingleClient: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { publisher.Close() })

	cache := &cachemocks.Client{
		GetClientFunc: func() valkey.Client { return client },
	}
	settingsService := &trackingSettingsReadWriter{
		current: settings.Settings{
			AlarmAdvanceMinutes: 5,
			ScraperProxyEnabled: false,
		},
	}
	alarmService, err := notification.NewAlarmService(nil, nil, nil, nil, nil, nil, logger, []int{5, 3, 1})
	require.NoError(t, err)

	deps := botConfigSubscriberDependencies{
		Cache:    cache,
		Settings: settingsService,
	}
	runtimeDeps := botConfigSubscriberRuntimeDependencies{
		AlarmCRUD: alarmService,
	}
	subscriber := appbootstrap.BuildBotConfigSubscriber(t.Context(), deps, runtimeDeps, nil, logger)
	require.NotNil(t, subscriber)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan struct{})

	go func() {
		subscriber.Run(ctx)
		close(done)
	}()

	require.Eventually(t, func() bool {
		publishConfigUpdate(t, publisher, contractssettings.UpdateTypeAlarmAdvanceMinutes, contractssettings.AlarmAdvanceMinutesPayloadV1{Minutes: 12})

		return assert.ObjectsAreEqual([]int{12, 3, 1}, alarmService.GetTargetMinutes()) &&
			settingsService.Get().AlarmAdvanceMinutes == 12 &&
			assert.ObjectsAreEqual([]int{12, 3, 1}, settingsService.Get().TargetMinutes)
	}, 2*time.Second, 50*time.Millisecond)

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not stop after cancel")
	}
}

func TestBuildBotConfigSubscriber_PublisherRoundTrip(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	client, addr := newTestValkeyClient(t)
	publisherClient, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{addr},
		DisableCache:      true,
		ForceSingleClient: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { publisherClient.Close() })

	cache := &cachemocks.Client{
		GetClientFunc: func() valkey.Client { return client },
	}
	settingsService := &trackingSettingsReadWriter{
		current: settings.Settings{
			AlarmAdvanceMinutes: 5,
			ScraperProxyEnabled: false,
		},
	}
	youtubeService := &trackingYouTubeService{}
	alarmService := &trackingAlarmAdvanceCRUD{targets: []int{15, 30}}
	scheduler := poller.NewScheduler(poller.SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: time.Millisecond,
	})
	trackingPoller := &trackingProxyTogglePoller{}
	scheduler.Register("channel-1", trackingPoller, poller.PriorityNormal, time.Minute)

	deps := botConfigSubscriberDependencies{
		Cache:    cache,
		Settings: settingsService,
	}
	runtimeDeps := botConfigSubscriberRuntimeDependencies{
		YouTubeService: youtubeService,
		AlarmCRUD:      alarmService,
	}
	subscriber := appbootstrap.BuildBotConfigSubscriber(t.Context(), deps, runtimeDeps, scheduler, logger)
	require.NotNil(t, subscriber)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan struct{})

	go func() {
		subscriber.Run(ctx)
		close(done)
	}()

	configPublisher := configsub.NewPublisher(publisherClient)

	require.Eventually(t, func() bool {
		if err := configPublisher.PublishScraperProxy(t.Context(), true); err != nil {
			return false
		}

		got := settingsService.Get()

		return got.ScraperProxyEnabled && youtubeService.isProxyEnabled() && trackingPoller.isEnabled()
	}, 2*time.Second, 50*time.Millisecond)

	require.Eventually(t, func() bool {
		if err := configPublisher.PublishAlarmAdvanceMinutes(t.Context(), 30); err != nil {
			return false
		}

		calls, last := alarmService.callSnapshot()

		return calls >= 1 && last == 30 && settingsService.Get().AlarmAdvanceMinutes == 30
	}, 2*time.Second, 50*time.Millisecond)

	assert.GreaterOrEqual(t, settingsService.calls(), 2)

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not stop after cancel")
	}
}

var _ settings.ReadWriter = (*trackingSettingsReadWriter)(nil)
