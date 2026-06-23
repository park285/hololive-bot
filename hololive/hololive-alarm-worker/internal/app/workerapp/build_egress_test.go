package workerapp

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type youtubeOutboxKaringCapableSender interface {
	SendYouTubeOutboxKaring(ctx context.Context, roomID string, payload *domain.YouTubeOutboxDispatchPayload) error
}

type workerappEgressTestPostgres struct{}

func (workerappEgressTestPostgres) GetPool() *pgxpool.Pool {
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

func TestBuildAlarmDispatchRunnerDefaultsToPGWhenConsumerModeUnset(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_CONSUMER_ENABLED", "true")
	t.Setenv("ALARM_DISPATCH_CONSUMER_MODE", "")
	t.Setenv("ALARM_DISPATCH_PUBLISH_MODE", "")
	t.Setenv("ALARM_DISPATCH_MAX_BATCH", "7")
	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "true")
	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}}

	scheduler, err := buildAlarmDispatchRunner(infra, egress.NewIrisMessageSender(nil), nil)
	require.NoError(t, err)

	runner, ok := scheduler.(*alarmDispatchRunner)
	require.True(t, ok)
	assert.Equal(t, "pg", runner.consumerMode)
	assert.Equal(t, 7, runner.maxBatch)
	assert.True(t, runner.karingEnabled)
	assert.True(t, runner.postSendQuarantine)
	waiter, ok := runner.idleWaiter.(*alarmDispatchWakeupWaiter)
	require.True(t, ok)
	assert.NotNil(t, waiter)
}

func TestBuildAlarmDispatchRunnerRejectsRemovedLegacyConsumerMode(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_CONSUMER_ENABLED", "true")
	t.Setenv("ALARM_DISPATCH_CONSUMER_MODE", "valkey")
	t.Setenv("ALARM_DISPATCH_PUBLISH_MODE", "")
	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}}

	scheduler, err := buildAlarmDispatchRunner(infra, egress.NewIrisMessageSender(nil), nil)
	require.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "ALARM_DISPATCH_CONSUMER_MODE")
	assert.Contains(t, err.Error(), "no longer supported")
}

func TestBuildAlarmDispatchRunnerWiresPGMode(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_CONSUMER_ENABLED", "true")
	t.Setenv("ALARM_DISPATCH_CONSUMER_MODE", "PG")
	t.Setenv("ALARM_DISPATCH_MAX_BATCH", "9")
	t.Setenv("ALARM_DISPATCH_MAX_BATCHES_PER_WAKE", "3")
	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "false")
	t.Setenv("ALARM_DISPATCH_WAKEUP_ENABLED", "false")
	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}}

	scheduler, err := buildAlarmDispatchRunner(infra, egress.NewIrisMessageSender(nil), nil)
	require.NoError(t, err)

	runner, ok := scheduler.(*alarmDispatchRunner)
	require.True(t, ok)
	assert.Equal(t, "pg", runner.consumerMode)
	assert.Equal(t, 9, runner.maxBatch)
	assert.Equal(t, 3, runner.maxBatchesPerWake)
	assert.False(t, runner.karingEnabled)
	assert.True(t, runner.postSendQuarantine)
	waiter, ok := runner.idleWaiter.(*alarmDispatchWakeupWaiter)
	require.True(t, ok)
	assert.False(t, waiter.wakeupEnabled)
}

func TestBuildEgressDispatchersRespectDisabledFlags(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_CONSUMER_ENABLED", "false")
	t.Setenv("DELIVERY_DISPATCHER_ENABLED", "false")
	t.Setenv("YOUTUBE_OUTBOX_DISPATCHER_ENABLED", "false")

	scheduler, err := buildAlarmDispatchRunner(nil, egress.NewIrisMessageSender(nil), nil)
	require.NoError(t, err)
	assert.Nil(t, scheduler)

	scheduler, err = buildDeliveryOutboxDispatcher(nil, egress.NewIrisMessageSender(nil), nil)
	require.NoError(t, err)
	assert.Nil(t, scheduler)

	scheduler, err = buildYouTubeOutboxDispatcher(nil, egress.NewIrisMessageSender(nil), nil)
	require.NoError(t, err)
	assert.Nil(t, scheduler)
}

func TestBuildEgressDispatchersRejectMissingInfraWhenEnabled(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_CONSUMER_ENABLED", "true")
	t.Setenv("DELIVERY_DISPATCHER_ENABLED", "true")
	t.Setenv("YOUTUBE_OUTBOX_DISPATCHER_ENABLED", "true")

	scheduler, err := buildAlarmDispatchRunner(nil, egress.NewIrisMessageSender(nil), nil)
	require.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "infra is required")

	scheduler, err = buildDeliveryOutboxDispatcher(nil, egress.NewIrisMessageSender(nil), nil)
	require.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "postgres is required")

	scheduler, err = buildYouTubeOutboxDispatcher(nil, egress.NewIrisMessageSender(nil), nil)
	require.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "postgres is required")
}

type claimKeyReleaseRecordingCache struct {
	cache.Client
	delManyCalls int
	delManyKeys  []string
}

func (c *claimKeyReleaseRecordingCache) DelMany(_ context.Context, keys []string) (int64, error) {
	c.delManyCalls++
	c.delManyKeys = append(c.delManyKeys, keys...)
	return int64(len(keys)), nil
}

func TestBuildAlarmDispatchRunnerWiresPGModeClaimKeyReleaser(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_CONSUMER_ENABLED", "true")
	t.Setenv("ALARM_DISPATCH_CONSUMER_MODE", "pg")
	cacheFake := &claimKeyReleaseRecordingCache{}
	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}, Cache: cacheFake}

	scheduler, err := buildAlarmDispatchRunner(infra, egress.NewIrisMessageSender(nil), nil)
	require.NoError(t, err)
	runner, ok := scheduler.(*alarmDispatchRunner)
	require.True(t, ok)

	err = runner.consumer.ReleaseClaimKeys(context.Background(), []string{
		"notified:claim:room-1:stream-1:100:live",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, cacheFake.delManyCalls,
		"pg-mode consumer must wire claim-key releaser so DLQ claim keys are released (H14/P1-d)")
	assert.Equal(t, []string{"notified:claim:room-1:stream-1:100:live"}, cacheFake.delManyKeys)
}
