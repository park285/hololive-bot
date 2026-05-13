package app

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAlarmWorkerRuntime_FailFastOnNilInputs(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	runtime, err := BuildAlarmWorkerRuntime(t.Context(), nil, logger)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Equal(t, "config must not be nil", err.Error())

	runtime, err = BuildAlarmWorkerRuntime(t.Context(), &config.Config{}, nil)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Equal(t, "logger must not be nil", err.Error())
}

func TestRuntimeAllowsAlarmScheduler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		runtimeRole string
		configValue string
		want        bool
	}{
		{name: "default bot role", runtimeRole: "bot", configValue: "", want: true},
		{name: "default worker role", runtimeRole: "worker", configValue: "", want: true},
		{name: "bot explicitly enabled", runtimeRole: "bot", configValue: "bot", want: true},
		{name: "worker explicitly enabled", runtimeRole: "worker", configValue: "worker", want: true},
		{name: "bot disabled when worker owns scheduler", runtimeRole: "bot", configValue: "worker", want: false},
		{name: "worker disabled when bot owns scheduler", runtimeRole: "worker", configValue: "bot", want: false},
		{name: "off disables all", runtimeRole: "bot", configValue: "off", want: false},
		{name: "unknown disables", runtimeRole: "worker", configValue: "mystery", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, runtimeAllowsAlarmScheduler(tt.runtimeRole, tt.configValue))
		})
	}
}

func TestLoadAlarmDispatchPublishConfigRejectsUnknownMode(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_PUBLISH_MODE", "pg-frist")

	cfg, err := loadAlarmDispatchPublishConfig()
	require.Error(t, err)
	assert.Equal(t, queue.PublishConfig{}, cfg)
	assert.True(t, strings.Contains(err.Error(), "ALARM_DISPATCH_PUBLISH_MODE"))
}

func TestLoadAlarmDispatchPublishConfigRejectsForbiddenConsumerModePair(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_PUBLISH_MODE", "pg_first")
	t.Setenv("ALARM_DISPATCH_CONSUMER_MODE", "valkey")

	cfg, err := loadAlarmDispatchPublishConfig()
	require.Error(t, err)
	assert.Equal(t, queue.PublishConfig{}, cfg)
	assert.Contains(t, err.Error(), "forbidden alarm dispatch mode combination")
}

func TestLoadAlarmDispatchPublishConfigRejectsPGFirstWithoutPeerConsumerMode(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_PUBLISH_MODE", "pg_first")

	cfg, err := loadAlarmDispatchPublishConfig()
	require.Error(t, err)
	assert.Equal(t, queue.PublishConfig{}, cfg)
	assert.Contains(t, err.Error(), "ALARM_DISPATCH_CONSUMER_MODE is required")
}

func TestLoadAlarmDispatchPublishConfigAllowsMatchingPGPair(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_PUBLISH_MODE", "pg_first")
	t.Setenv("ALARM_DISPATCH_CONSUMER_MODE", "pg")

	cfg, err := loadAlarmDispatchPublishConfig()
	require.NoError(t, err)
	assert.Equal(t, queue.PublishModePGFirst, cfg.Mode)
}

func TestNotificationEgressRunnerRetriesHeldLeaseUntilAcquired(t *testing.T) {
	var setNXCalls atomic.Int32
	var schedulerStarts atomic.Int32
	cacheSvc := &cachemocks.Client{
		SetNXFunc: func(context.Context, string, string, time.Duration) (bool, error) {
			return setNXCalls.Add(1) > 1, nil
		},
		CompareAndDeleteFunc: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}
	runner := notificationEgressRunner{
		leaseCache:         cacheSvc,
		leaseEnabled:       true,
		leaseRetryInterval: time.Millisecond,
	}

	err := runner.startWithLease(t.Context(), []namedRuntimeScheduler{{
		name:      "test",
		scheduler: runtimeAlarmSchedulerFunc(func(context.Context) error { schedulerStarts.Add(1); return nil }),
	}})

	require.NoError(t, err)
	assert.GreaterOrEqual(t, setNXCalls.Load(), int32(2))
	assert.Equal(t, int32(1), schedulerStarts.Load())
}

type runtimeAlarmSchedulerFunc func(context.Context) error

func (f runtimeAlarmSchedulerFunc) Start(ctx context.Context) error {
	return f(ctx)
}
