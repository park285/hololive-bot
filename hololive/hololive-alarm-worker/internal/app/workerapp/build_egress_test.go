package workerapp

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type youtubeOutboxKaringCapableSender interface {
	SendYouTubeOutboxKaring(ctx context.Context, roomID string, payload domain.YouTubeOutboxDispatchPayload) error
}

type workerappEgressTestPostgres struct{}

func (workerappEgressTestPostgres) GetPool() *pgxpool.Pool {
	return nil
}

func (workerappEgressTestPostgres) GetGormDB() *gorm.DB {
	return nil
}

func (workerappEgressTestPostgres) Ping(context.Context) error {
	return nil
}

func (workerappEgressTestPostgres) Close() error {
	return nil
}

func TestBuildYouTubeOutboxSenderDisablesKaringByDefault(t *testing.T) {
	t.Setenv("YOUTUBE_OUTBOX_KARING_ENABLED", "")
	irisSender := egress.NewIrisMessageSender(nil)

	sender := buildYouTubeOutboxSender(irisSender)

	assert.Same(t, irisSender, sender)
	_, ok := sender.(youtubeOutboxKaringCapableSender)
	assert.False(t, ok)
}

func TestBuildYouTubeOutboxSenderEnablesKaringWhenConfigured(t *testing.T) {
	t.Setenv("YOUTUBE_OUTBOX_KARING_ENABLED", "true")
	irisSender := egress.NewIrisMessageSender(nil)

	sender := buildYouTubeOutboxSender(irisSender)

	_, ok := sender.(youtubeOutboxKaringCapableSender)
	assert.True(t, ok)
}

func TestBuildNotificationEgressRequiresPostgres(t *testing.T) {
	runner, err := buildNotificationEgress(&config.Config{}, &sharedmodules.InfraModule{}, nil)

	require.Error(t, err)
	assert.Nil(t, runner)
	assert.Contains(t, err.Error(), "postgres is required")
}

func TestBuildAlarmDispatchRunnerWiresValkeyMode(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_CONSUMER_ENABLED", "true")
	t.Setenv("ALARM_DISPATCH_CONSUMER_MODE", "")
	t.Setenv("ALARM_DISPATCH_MAX_BATCH", "7")
	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "true")
	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}}

	scheduler := buildAlarmDispatchRunner(infra, egress.NewIrisMessageSender(nil), nil)

	runner, ok := scheduler.(*alarmDispatchRunner)
	require.True(t, ok)
	assert.Equal(t, "valkey", runner.consumerMode)
	assert.Equal(t, 7, runner.maxBatch)
	assert.True(t, runner.karingEnabled)
	assert.True(t, runner.postSendQuarantine)
	assert.Nil(t, runner.idleWaiter)
}

func TestBuildAlarmDispatchRunnerWiresPGMode(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_CONSUMER_ENABLED", "true")
	t.Setenv("ALARM_DISPATCH_CONSUMER_MODE", "PG")
	t.Setenv("ALARM_DISPATCH_MAX_BATCH", "9")
	t.Setenv("ALARM_DISPATCH_MAX_BATCHES_PER_WAKE", "3")
	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "false")
	t.Setenv("ALARM_DISPATCH_WAKEUP_ENABLED", "false")
	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}}

	scheduler := buildAlarmDispatchRunner(infra, egress.NewIrisMessageSender(nil), nil)

	runner, ok := scheduler.(*alarmDispatchRunner)
	require.True(t, ok)
	assert.Equal(t, "pg", runner.consumerMode)
	assert.Equal(t, 9, runner.maxBatch)
	assert.Equal(t, 3, runner.maxBatchesPerWake)
	assert.False(t, runner.karingEnabled)
	assert.True(t, runner.postSendQuarantine)
	require.IsType(t, &alarmDispatchWakeupWaiter{}, runner.idleWaiter)
	assert.False(t, runner.idleWaiter.(*alarmDispatchWakeupWaiter).wakeupEnabled)
}

func TestBuildEgressDispatchersRespectDisabledFlags(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_CONSUMER_ENABLED", "false")
	t.Setenv("DELIVERY_DISPATCHER_ENABLED", "false")
	t.Setenv("YOUTUBE_OUTBOX_DISPATCHER_ENABLED", "false")

	assert.Nil(t, buildAlarmDispatchRunner(nil, egress.NewIrisMessageSender(nil), nil))
	assert.Nil(t, buildDeliveryOutboxDispatcher(nil, egress.NewIrisMessageSender(nil), nil))
	assert.Nil(t, buildYouTubeOutboxDispatcher(nil, egress.NewIrisMessageSender(nil), nil))
}
