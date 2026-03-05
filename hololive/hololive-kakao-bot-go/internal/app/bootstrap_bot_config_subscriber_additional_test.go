package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
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

func (a *trackingAlarmAdvanceCRUD) UpdateAlarmAdvanceMinutes(minutes int) []int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	a.lastMinutes = minutes
	if len(a.targets) > 0 {
		return append([]int(nil), a.targets...)
	}
	return []int{minutes}
}

func (a *trackingAlarmAdvanceCRUD) callSnapshot() (calls int, lastMinutes int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls, a.lastMinutes
}

func newTestValkeyClient(t *testing.T) (valkey.Client, *miniredis.Miniredis, string) {
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

	return client, mini, addr
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
	require.NoError(t, client.Do(context.Background(), cmd).Error())
}

func TestBuildBotConfigSubscriber_ScraperProxyUpdate(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, _, addr := newTestValkeyClient(t)
	publisher, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{addr},
		DisableCache:      true,
		ForceSingleClient: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { publisher.Close() })

	cacheSvc := &cachemocks.Client{
		GetClientFunc: func() valkey.Client { return client },
	}
	settingsSvc := &trackingSettingsReadWriter{
		current: settings.Settings{
			AlarmAdvanceMinutes: 5,
			ScraperProxyEnabled: false,
		},
	}
	youtubeSvc := &trackingYouTubeSvc{}
	scheduler := poller.NewScheduler(poller.SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: time.Millisecond,
	})
	trackingPoller := &trackingProxyTogglePoller{}
	scheduler.Register("channel-1", trackingPoller, poller.PriorityNormal, time.Minute)

	deps := botConfigSubscriberDependencies{
		cache:    cacheSvc,
		settings: settingsSvc,
	}
	runtimeDeps := botConfigSubscriberRuntimeDependencies{
		youtubeService: youtubeSvc,
	}
	subscriber := buildBotConfigSubscriber(deps, runtimeDeps, scheduler, logger)
	require.NotNil(t, subscriber)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		subscriber.Run(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	publishConfigUpdate(t, publisher, contractssettings.UpdateTypeScraperProxy, contractssettings.ScraperProxyPayloadV1{Enabled: true})

	require.Eventually(t, func() bool {
		got := settingsSvc.Get()
		return got.ScraperProxyEnabled && youtubeSvc.proxyEnabled && trackingPoller.enabled
	}, 2*time.Second, 50*time.Millisecond)

	assert.Equal(t, 1, settingsSvc.calls())

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not stop after cancel")
	}
}

func TestBuildBotConfigSubscriber_AlarmAdvanceMinutesUpdate(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, _, addr := newTestValkeyClient(t)
	publisher, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{addr},
		DisableCache:      true,
		ForceSingleClient: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { publisher.Close() })

	cacheSvc := &cachemocks.Client{
		GetClientFunc: func() valkey.Client { return client },
	}
	settingsSvc := &trackingSettingsReadWriter{
		current: settings.Settings{
			AlarmAdvanceMinutes: 5,
			ScraperProxyEnabled: false,
		},
		updateErr: errors.New("persist failed"),
	}
	alarmSvc := &trackingAlarmAdvanceCRUD{targets: []int{15, 30}}

	deps := botConfigSubscriberDependencies{
		cache:    cacheSvc,
		settings: settingsSvc,
	}
	runtimeDeps := botConfigSubscriberRuntimeDependencies{
		alarmCRUD: alarmSvc,
	}
	subscriber := buildBotConfigSubscriber(deps, runtimeDeps, nil, logger)
	require.NotNil(t, subscriber)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		subscriber.Run(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	publishConfigUpdate(t, publisher, contractssettings.UpdateTypeAlarmAdvanceMinutes, contractssettings.AlarmAdvanceMinutesPayloadV1{Minutes: 30})

	require.Eventually(t, func() bool {
		calls, last := alarmSvc.callSnapshot()
		return calls == 1 && last == 30
	}, 2*time.Second, 50*time.Millisecond)

	assert.Equal(t, 1, settingsSvc.calls())
	assert.Equal(t, 5, settingsSvc.Get().AlarmAdvanceMinutes)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not stop after cancel")
	}
}

var _ settings.ReadWriter = (*trackingSettingsReadWriter)(nil)
